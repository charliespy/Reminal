// Phone alerts: the relay's part is deliberately small. A machine seals the
// alert end-to-end to the phone's own push keys (RFC 8291) before it ever
// reaches us, so all this does is add the VAPID signature every push service
// requires and forward the opaque blob. Nothing is stored: no list of devices,
// no alert text, no log line — the Worker cannot read what it forwards and
// keeps no record that it did.
//
// Why the signature has to happen here: a browser holds ONE push subscription
// per site, bound to one VAPID key, so every machine must push under the same
// key. That key belongs to the site, so it lives in the Worker as a secret.

import { allow, inIsolate, type LimiterEnv } from "./limiter";
import { addressGroup } from "./session";

export interface PushEnv extends LimiterEnv {
  // Base64url of the uncompressed P-256 public key (65 bytes). Handed to the
  // page as applicationServerKey when it subscribes.
  VAPID_PUBLIC?: string;
  // The matching private key as a JWK (JSON string). A secret, never a var.
  VAPID_PRIVATE_JWK?: string;
  // The key an alert's token is made with (HMAC-SHA256; any long random
  // string). A secret: `wrangler secret put PUSH_TOKEN_KEY`.
  PUSH_TOKEN_KEY?: string;
}

// Alerts are bound to the machine that registered them. A phone's page asks
// for a token for (its subscription, the machine it is turning alerts on
// for); the relay sends that token to the phone itself, sealed to the
// subscription's keys, in a notification the phone shows — so only the
// phone can read it, and it hands it to the machine. Each alert then
// carries the token and a fresh signature by that machine's key over what
// it sends. The relay keeps nothing: the token is a MAC over what it binds.
//
// An alert without a token (a machine on an older reminal, or a phone that
// subscribed before tokens) is refused ("renew"), in words an older
// machine's log carries as they are: update reminal, and renew alerts from
// the phone, which the page then offers.
// A token asked for one subscription: few, so asking cannot be used to
// buzz someone's phone (each ask shows a notification on it).
const PER_TOKEN_ASK = { calls: 3, period: 600 };
// An alert's signature is good this many seconds either side of the
// relay's clock, and only once.
const SIG_FRESH = 120;
const tokenAsks = new Map<string, number[]>();
const sigsSeen = new Map<string, number[]>();

// Only the real push services. Without this the endpoint would make the
// Worker an open HTTP proxy for anyone willing to POST to it.
const PUSH_HOSTS = [
  /^fcm\.googleapis\.com$/,
  /^([a-z0-9-]+\.)*push\.apple\.com$/,
  /^([a-z0-9-]+\.)*push\.services\.mozilla\.com$/,
  /^([a-z0-9-]+\.)*notify\.windows\.com$/,
];

export function allowedEndpoint(raw: unknown): URL | null {
  if (typeof raw !== "string" || raw.length > 1024) return null;
  let u: URL;
  try {
    u = new URL(raw);
  } catch {
    return null;
  }
  if (u.protocol !== "https:" || u.port !== "") return null;
  // No credentials in it: a push service's endpoint never carries a user or
  // a password, and the relay posts to the host alone.
  if (u.username !== "" || u.password !== "") return null;
  // One spelling per subscription, as the browser gave it: the limits and
  // the token are keyed on it, so another spelling of the same endpoint
  // (a case, a default port, a query or a fragment) is not let through.
  if (u.hash !== "" || raw !== u.href) return null;
  // A query only where a push service puts one: Edge on Windows subscribes
  // through WNS, whose endpoints are https://<host>.notify.windows.com/w/
  // with a single token parameter. Any other query is refused.
  if (u.search !== "" && !wnsEndpoint(u)) return null;
  return PUSH_HOSTS.some((re) => re.test(u.hostname)) ? u : null;
}

// wnsEndpoint says whether u is a WNS push endpoint with its token: one host
// label under notify.windows.com, the path /w/, and the query ?token= and
// nothing else.
function wnsEndpoint(u: URL): boolean {
  return /^[a-z0-9-]+\.notify\.windows\.com$/.test(u.hostname) &&
    u.pathname === "/w/" &&
    /^\?token=[A-Za-z0-9%._~+\/=-]+$/.test(u.search);
}

// What one alert may cost, counted where the count is the count (the
// RateLimiter, src/limiter.ts), each isolate's own only where that cannot be
// asked: per subscription, and per caller (its address, IPv6 by its /64). A
// machine's watcher sends a handful of alerts a day; these are brakes on a
// runaway loop or on someone sending through this relay, not quotas. An
// address can be many machines (one NAT, a hosting provider's shared
// egress), so a caller has room for many subscriptions' worth. Nothing is counted per push
// service: one count for everyone is one anyone could use up.
// In an isolate, the oldest counts go first, never all at once.
const PER_CALLER = { calls: 600, period: 60 };
const PER_ENDPOINT = { calls: 20, period: 60 };
const callers = new Map<string, number[]>();
const endpoints = new Map<string, number[]>();

// Where the RateLimiter can't be asked (an outage), each isolate counts its
// own: deliberately not failing closed, so a blip drops no real alerts. For
// the once-check on an alert's signature that means a replay to another
// isolate during an outage gets through, at worst one alert shown twice.
async function within(env: LimiterEnv, scope: string, counts: Map<string, number[]>, key: string, lim: { calls: number; period: number }): Promise<boolean> {
  const c = await allow(env, scope, key, lim.calls, lim.period);
  return c === true || (c === null && inIsolate(counts, key, lim.calls, lim.period));
}

function b64url(bytes: ArrayBuffer | Uint8Array): string {
  const u8 = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
  let s = "";
  for (const b of u8) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function b64ToBytes(s: string): Uint8Array<ArrayBuffer> {
  const bin = atob(s.replace(/-/g, "+").replace(/_/g, "/"));
  const out = new Uint8Array(new ArrayBuffer(bin.length));
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

let signingKey: CryptoKey | null = null;
const jwtCache = new Map<string, { jwt: string; exp: number }>();

export async function vapidJWT(env: PushEnv, audience: string, nowSec: number): Promise<string> {
  const hit = jwtCache.get(audience);
  if (hit && hit.exp - nowSec > 3600) return hit.jwt;
  if (!signingKey) {
    signingKey = await crypto.subtle.importKey(
      "jwk",
      JSON.parse(env.VAPID_PRIVATE_JWK as string),
      { name: "ECDSA", namedCurve: "P-256" },
      false,
      ["sign"],
    );
  }
  const exp = nowSec + 12 * 3600;
  const enc = new TextEncoder();
  const head = b64url(enc.encode(JSON.stringify({ typ: "JWT", alg: "ES256" })));
  const body = b64url(enc.encode(JSON.stringify({ aud: audience, exp, sub: "https://reminal.app" })));
  // WebCrypto's ECDSA signature is already the raw r||s form JWS wants.
  const sig = await crypto.subtle.sign({ name: "ECDSA", hash: "SHA-256" }, signingKey, enc.encode(head + "." + body));
  const jwt = head + "." + body + "." + b64url(sig);
  jwtCache.set(audience, { jwt, exp });
  return jwt;
}

const enc = new TextEncoder();

// A machine's ed25519 public key, as the page and the machine send it
// (base64 or base64url), in one spelling for the MAC: standard base64.
function machineKey(raw: unknown): { b64: string; bytes: Uint8Array<ArrayBuffer> } | null {
  if (typeof raw !== "string" || raw.length > 64) return null;
  let bytes: Uint8Array<ArrayBuffer>;
  try {
    bytes = b64ToBytes(raw);
  } catch {
    return null;
  }
  if (bytes.length !== 32) return null;
  let s = "";
  for (const b of bytes) s += String.fromCharCode(b);
  return { b64: btoa(s), bytes };
}

async function hmac(env: PushEnv, msg: string): Promise<string> {
  const key = await crypto.subtle.importKey("raw", enc.encode(env.PUSH_TOKEN_KEY as string), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  return b64url(await crypto.subtle.sign("HMAC", key, enc.encode(msg)));
}

// tokenFor binds an alert subscription to the machine that may send to it.
// Its issued-at is in it, so a later rule can retire old ones.
export async function tokenFor(env: PushEnv, endpoint: string, machine: string, iat: number): Promise<string> {
  return "v1." + iat + "." + (await hmac(env, "reminal-push-token\n" + endpoint + "\n" + machine + "\n" + iat));
}

async function tokenGood(env: PushEnv, token: unknown, endpoint: string, machine: string, nowSec: number): Promise<boolean> {
  if (typeof token !== "string") return false;
  const m = /^v1\.(\d{1,12})\.([A-Za-z0-9_-]{43})$/.exec(token);
  if (!m || Number(m[1]) > nowSec + SIG_FRESH) return false;
  const want = await tokenFor(env, endpoint, machine, Number(m[1]));
  // Compared whole, as bytes of equal length, without stopping early.
  const a = enc.encode(want), b = enc.encode(token);
  if (a.length !== b.length) return false;
  let d = 0;
  for (let i = 0; i < a.length; i++) d |= a[i] ^ b[i];
  return d === 0;
}

// What a machine signs for one alert: where it goes, what it is, and when.
export function alertSigned(endpoint: string, payloadSha256Hex: string, ts: number): string {
  return "reminal-push\n" + endpoint + "\n" + payloadSha256Hex + "\n" + ts;
}

function hex(buf: ArrayBuffer): string {
  return [...new Uint8Array(buf)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

// RFC 8291 (aes128gcm): a message sealed to one subscription's keys, as a
// machine seals its alerts (webpush.go's sealPush).
export async function sealTo(p256dh: Uint8Array<ArrayBuffer>, auth: Uint8Array<ArrayBuffer>, plaintext: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  const ua = await crypto.subtle.importKey("raw", p256dh, { name: "ECDH", namedCurve: "P-256" }, false, []);
  const as = (await crypto.subtle.generateKey({ name: "ECDH", namedCurve: "P-256" }, true, ["deriveBits"])) as CryptoKeyPair;
  const asPub = new Uint8Array(await crypto.subtle.exportKey("raw", as.publicKey) as ArrayBuffer);
  const ecdh = await crypto.subtle.deriveBits({ name: "ECDH", public: ua }, as.privateKey, 256);
  const hkdf = async (salt: Uint8Array<ArrayBuffer>, ikm: ArrayBuffer, info: Uint8Array<ArrayBuffer>, n: number) =>
    crypto.subtle.deriveBits({ name: "HKDF", hash: "SHA-256", salt, info }, await crypto.subtle.importKey("raw", ikm, "HKDF", false, ["deriveBits"]), n * 8);
  const cat = (...parts: Uint8Array[]) => {
    const out = new Uint8Array(new ArrayBuffer(parts.reduce((n, p) => n + p.length, 0)));
    let o = 0;
    for (const p of parts) { out.set(p, o); o += p.length; }
    return out;
  };
  const ikm = await hkdf(auth, ecdh, cat(enc.encode("WebPush: info\0"), p256dh, asPub), 32);
  const salt = crypto.getRandomValues(new Uint8Array(new ArrayBuffer(16)));
  const cek = await hkdf(salt, ikm, enc.encode("Content-Encoding: aes128gcm\0"), 16);
  const nonce = await hkdf(salt, ikm, enc.encode("Content-Encoding: nonce\0"), 12);
  const key = await crypto.subtle.importKey("raw", cek, "AES-GCM", false, ["encrypt"]);
  const ct = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv: nonce }, key, cat(plaintext, new Uint8Array([2]))));
  const rs = new Uint8Array([0, 0, 16, 0]); // 4096
  return cat(salt, rs, new Uint8Array([asPub.length]), asPub, ct);
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json", "cache-control": "no-store" },
  });
}

// GET /push/key — the public half, for the page's subscribe call.
export function handlePushKey(env: PushEnv): Response {
  if (!env.VAPID_PUBLIC || !env.VAPID_PRIVATE_JWK) return json({ error: "alerts are not set up on this relay" }, 501);
  return json({ key: env.VAPID_PUBLIC });
}

// POST /push/token {endpoint, p256dh, auth, machine}: a token binding this
// subscription to that machine, sent to the subscription itself — sealed, in
// a notification its phone shows (the service worker hands the token to the
// page, and the page to the machine). Answers {status}, the push service's.
export async function handlePushToken(request: Request, env: PushEnv): Promise<Response> {
  if (request.method !== "POST") return json({ error: "POST only" }, 405);
  if (!env.VAPID_PUBLIC || !env.VAPID_PRIVATE_JWK || !env.PUSH_TOKEN_KEY) return json({ error: "alerts are not set up on this relay" }, 501);
  let req: { endpoint?: unknown; p256dh?: unknown; auth?: unknown; machine?: unknown };
  try {
    req = await request.json();
  } catch {
    return json({ error: "bad request" }, 400);
  }
  const ep = allowedEndpoint(req.endpoint);
  if (!ep) return json({ error: "not a known push service" }, 400);
  const machine = machineKey(req.machine);
  if (!machine) return json({ error: "not a machine's key" }, 400);
  let p256dh: Uint8Array<ArrayBuffer>, auth: Uint8Array<ArrayBuffer>;
  try {
    p256dh = b64ToBytes(String(req.p256dh));
    auth = b64ToBytes(String(req.auth));
  } catch {
    return json({ error: "bad subscription keys" }, 400);
  }
  if (p256dh.length !== 65 || p256dh[0] !== 4 || auth.length !== 16) return json({ error: "bad subscription keys" }, 400);
  const endpoint = ep.href;
  const caller = addressGroup(request.headers.get("cf-connecting-ip") ?? "");
  if (!(await within(env, "push-caller", callers, caller, PER_CALLER)) ||
    !(await within(env, "push-token", tokenAsks, endpoint, PER_TOKEN_ASK))) {
    return json({ error: "too many requests; try again in a few minutes" }, 429);
  }
  const now = Date.now();
  const token = await tokenFor(env, endpoint, machine.b64, Math.floor(now / 1000));
  let sealed: Uint8Array<ArrayBuffer>;
  try {
    sealed = await sealTo(p256dh, auth, enc.encode(JSON.stringify({ kind: "alerts-on", machine: machine.b64, token, at: now })));
  } catch {
    return json({ error: "bad subscription keys" }, 400);
  }
  const jwt = await vapidJWT(env, ep.origin, Math.floor(now / 1000));
  const res = await fetch(ep.href, {
    method: "POST",
    headers: {
      TTL: "600",
      Urgency: "normal",
      "Content-Encoding": "aes128gcm",
      "Content-Type": "application/octet-stream",
      Authorization: `vapid t=${jwt}, k=${env.VAPID_PUBLIC}`,
    },
    body: sealed,
    redirect: "manual", // never sent on elsewhere (pushAnswer)
  });
  await res.body?.cancel();
  return pushAnswer(res);
}

// pushAnswer is the push service's answer as the caller is told it: its
// status. A redirect is not followed (the post is sent with redirect:
// "manual"): the sealed alert and the relay's VAPID signature go to the
// endpoint that was allowed, never on to wherever it points. Its status is
// passed on as the failure it is (a machine counts anything but 2xx, 404
// and 410 as a failed send).
function pushAnswer(res: Response): Response {
  if (res.status >= 300 && res.status < 400) {
    return json({ status: res.status, error: "the push service redirected; not followed" });
  }
  return json({ status: res.status });
}

// POST /push {endpoint, payload (base64 aes128gcm), ttl?, urgency?,
// token, machine, ts, sig}
// Answers {status} — the push service's own status — so the machine can tell
// "delivered" from "this phone is gone, forget it" (404/410). Without a
// token: refused ("renew"). An alert whose time is off is refused with the
// relay's own time (now, ms), so the machine can set its offset and send
// it again.
export async function handlePush(request: Request, env: PushEnv, nowMs = Date.now()): Promise<Response> {
  if (request.method !== "POST") return json({ error: "POST only" }, 405);
  if (!env.VAPID_PUBLIC || !env.VAPID_PRIVATE_JWK) return json({ error: "alerts are not set up on this relay" }, 501);
  let req: { endpoint?: unknown; payload?: unknown; ttl?: unknown; urgency?: unknown; token?: unknown; machine?: unknown; ts?: unknown; sig?: unknown };
  try {
    req = await request.json();
  } catch {
    return json({ error: "bad request" }, 400);
  }
  const ep = allowedEndpoint(req.endpoint);
  if (!ep) return json({ error: "not a known push service" }, 400);
  if (typeof req.payload !== "string") return json({ error: "missing payload" }, 400);
  let payload: Uint8Array<ArrayBuffer>;
  try {
    payload = b64ToBytes(req.payload);
  } catch {
    return json({ error: "payload is not base64" }, 400);
  }
  // 4096 is the limit every push service shares. 103 is the aes128gcm header
  // (86) plus the GCM tag (16) plus the record delimiter (1): anything shorter
  // cannot be a sealed message.
  if (payload.length > 4096 || payload.length < 103) return json({ error: "payload size" }, 400);
  const now = nowMs;
  // Checked before anything is counted: an alert that is not the machine's
  // spends none of its subscription's allowance.
  const nowSec = Math.floor(now / 1000);
  // A relay without PUSH_TOKEN_KEY can check no token, so it forwards
  // nothing — and says it is not set up, never that a token is unbound
  // (machines drop an unbound one).
  if (!env.PUSH_TOKEN_KEY) return json({ error: "alerts are not set up on this relay" }, 501);
  if (req.token === undefined) {
    return json({ error: "phone alerts now need an up-to-date reminal: update reminal on this machine to keep phone alerts, then renew them from the phone", code: "renew" }, 403);
  }
  const machine = machineKey(req.machine);
  const ts = typeof req.ts === "number" && Number.isInteger(req.ts) ? req.ts : NaN;
  if (!machine || !(await tokenGood(env, req.token, ep.href, machine.b64, nowSec))) {
    return json({ error: "this alert is not bound to this machine; turn alerts on again from the phone", code: "unbound" }, 403);
  }
  if (!(Math.abs(nowSec - ts) <= SIG_FRESH)) return json({ error: "this alert's time is off this relay's", code: "clock", now }, 403);
  let sig: Uint8Array<ArrayBuffer>;
  try {
    sig = b64ToBytes(String(req.sig));
  } catch {
    return json({ error: "bad signature", code: "unbound" }, 403);
  }
  const key = await crypto.subtle.importKey("raw", machine.bytes, { name: "Ed25519" }, false, ["verify"]);
  const signed = alertSigned(ep.href, hex(await crypto.subtle.digest("SHA-256", payload)), ts);
  if (sig.length !== 64 || !(await crypto.subtle.verify({ name: "Ed25519" }, key, sig, enc.encode(signed)))) {
    return json({ error: "bad signature", code: "unbound" }, 403);
  }
  // Once: the same signed alert sent again is refused for as long as its
  // time would pass. Keyed on what was signed, not on the signature's bytes.
  if (!(await within(env, "push-sig", sigsSeen, b64url(await crypto.subtle.digest("SHA-256", enc.encode(signed))), { calls: 1, period: 2 * SIG_FRESH }))) {
    return json({ error: "this alert was already sent" }, 409);
  }
  const caller = addressGroup(request.headers.get("cf-connecting-ip") ?? "");
  if (!(await within(env, "push-caller", callers, caller, PER_CALLER)) ||
    !(await within(env, "push-endpoint", endpoints, ep.href, PER_ENDPOINT))) {
    return json({ error: "too many alerts; slow down" }, 429);
  }

  const ttl = typeof req.ttl === "number" && req.ttl >= 0 && req.ttl <= 86400 ? Math.floor(req.ttl) : 3600;
  const urgency = ["very-low", "low", "normal", "high"].includes(req.urgency as string) ? (req.urgency as string) : "normal";
  const jwt = await vapidJWT(env, ep.origin, nowSec);
  const res = await fetch(ep.href, {
    method: "POST",
    headers: {
      TTL: String(ttl),
      Urgency: urgency,
      "Content-Encoding": "aes128gcm",
      "Content-Type": "application/octet-stream",
      Authorization: `vapid t=${jwt}, k=${env.VAPID_PUBLIC}`,
    },
    body: payload,
    redirect: "manual", // never sent on elsewhere (pushAnswer)
  });
  // Drain without reading into memory we keep; the body is the service's
  // diagnostic text, which we do not relay.
  await res.body?.cancel();
  return pushAnswer(res);
}
