// The PIN gate in front of a forwarded port (`reminal expose`): how long a
// source that keeps getting the PIN wrong waits, what the visitor cookie a
// right PIN earns looks like, and how a typed PIN is read. Kept free of the
// Durable Object so it runs, and is tested, on its own.

// Wrong PINs one source may make before it waits.
export const GATE_SOURCE_FREE = 5;
// Wrong PINs across every source before sources the tunnel has not seen get
// the PIN right wait too. Much more than one source's allowance, so it takes
// many addresses to reach, and a source that has opened the tunnel before is
// never held by it: strangers guessing from everywhere cannot lock out the
// people the tunnel was shared with.
export const GATE_TUNNEL_FREE = 50;
// The first wait, doubled on each one after it, up to GATE_MAX_LOCK_MS.
export const GATE_FIRST_LOCK_MS = 5 * 60 * 1000;
export const GATE_MAX_LOCK_MS = 24 * 3600 * 1000;
// How many sources' counts a tunnel keeps, and how many sources it remembers
// as having got the PIN right.
export const GATE_SOURCES_MAX = 512;
export const GATE_KNOWN_MAX = 64;

// How long a visitor cookie lets its holder in, counted from when it was
// issued and checked here, not just by the browser.
export const VISITOR_MAX_AGE_S = 30 * 24 * 3600;

// Strikes is one counter: wrong PINs since the last wait, how many waits
// there have been, and when the current one ends.
export type Strikes = { fails: number; locks: number; until: number };

// lockFor is how long the locks-th wait lasts.
export function lockFor(locks: number): number {
  if (locks <= 0) return 0;
  return Math.min(GATE_MAX_LOCK_MS, GATE_FIRST_LOCK_MS * 2 ** Math.min(locks - 1, 30));
}

// current is s as it stands at now: forgotten once it has been quiet, after
// its last wait ended, for as long as that wait lasted.
export function current(s: Strikes | undefined, now: number): Strikes {
  if (!s) return { fails: 0, locks: 0, until: 0 };
  if (s.locks > 0 && now >= s.until + lockFor(s.locks)) return { fails: 0, locks: 0, until: 0 };
  return s;
}

// waitLeft is how long s still holds its source, in ms; 0 is not at all.
export function waitLeft(s: Strikes | undefined, now: number): number {
  const c = current(s, now);
  return c.until > now ? c.until - now : 0;
}

// strike records one wrong PIN; past free of them the counter waits, each wait
// twice the last.
export function strike(s: Strikes | undefined, now: number, free: number): Strikes {
  const c = current(s, now);
  const fails = c.fails + 1;
  if (fails < free) return { fails, locks: c.locks, until: c.until };
  const locks = c.locks + 1;
  return { fails: 0, locks, until: now + lockFor(locks) };
}

// pruneSources keeps at most max counters, dropping first the ones that have
// been forgotten, then the ones whose wait ends soonest.
export function pruneSources(m: Record<string, Strikes>, now: number, max: number): Record<string, Strikes> {
  const out: Record<string, Strikes> = {};
  const live = Object.entries(m).filter(([, s]) => {
    const c = current(s, now);
    return c.fails > 0 || c.locks > 0;
  });
  live.sort((a, b) => b[1].until - a[1].until);
  for (const [k, s] of live.slice(0, max)) out[k] = s;
  return out;
}

// rememberKnown adds tag to the sources that got the PIN right, keeping the
// max most recent.
export function rememberKnown(m: Record<string, number>, tag: string, now: number, max: number): Record<string, number> {
  const all = Object.entries({ ...m, [tag]: now });
  all.sort((a, b) => b[1] - a[1]);
  return Object.fromEntries(all.slice(0, max));
}

// GateState is what a tunnel's gate keeps: a counter per source, one for the
// whole tunnel, and the sources that have got the PIN right (when, last).
export type GateState = { sources: Record<string, Strikes>; tunnel?: Strikes; known: Record<string, number> };

// gateWait is how long src must wait before its next PIN is checked, in ms.
// A source that has got the PIN right before waits only for its own wrong
// PINs; any other waits for the tunnel's too.
export function gateWait(g: GateState, src: string, now: number): number {
  return Math.max(waitLeft(g.sources[src], now), src in g.known ? 0 : waitLeft(g.tunnel, now));
}

// gateWrong is g once src has got the PIN wrong.
export function gateWrong(g: GateState, src: string, now: number): GateState {
  const sources = pruneSources({ ...g.sources, [src]: strike(g.sources[src], now, GATE_SOURCE_FREE) }, now, GATE_SOURCES_MAX);
  return { sources, tunnel: strike(g.tunnel, now, GATE_TUNNEL_FREE), known: g.known };
}

// NO_SOURCE is the source of a request that came without an address. It is
// counted like any other, but never remembered as known: it is everyone.
export const NO_SOURCE = "-";

// gateRight is g once src has got the PIN right. The tunnel's count stays:
// another source's wrong PINs are not forgiven by this one getting it right.
export function gateRight(g: GateState, src: string, now: number): GateState {
  const sources = { ...g.sources };
  delete sources[src];
  const known = src === NO_SOURCE ? g.known : rememberKnown(g.known, src, now, GATE_KNOWN_MAX);
  return { sources, tunnel: g.tunnel, known };
}

// normalizePIN is a typed PIN as it was issued: spaces and dashes dropped,
// letters upper-cased, and the letters base32 leaves out because they look
// like digits read as those digits. A six-digit PIN is unchanged.
export function normalizePIN(s: string): string {
  return s.replace(/[\s-]+/g, "").toUpperCase().replace(/O/g, "0").replace(/[IL]/g, "1");
}

// visitorCookie is the cookie a right PIN earns: a random visitor id and when
// it was issued (unix seconds), MACed with the tunnel's key.
export async function visitorCookie(keyHex: string, nowMs: number): Promise<string> {
  const id = toHex(crypto.getRandomValues(new Uint8Array(16)));
  const at = String(Math.floor(nowMs / 1000));
  return `v1.${id}.${at}.${await hmacHex(keyHex, visitorMessage(id, at))}`;
}

// visitorCookieOK reports whether value is a visitor cookie this key issued
// no more than VISITOR_MAX_AGE_S ago.
export async function visitorCookieOK(keyHex: string, value: string, nowMs: number): Promise<boolean> {
  const m = /^v1\.([0-9a-f]{32})\.(\d{1,12})\.([0-9a-f]{64})$/.exec(value);
  if (!m) return false;
  const age = nowMs / 1000 - Number(m[2]);
  if (age > VISITOR_MAX_AGE_S || age < -60) return false;
  return timingSafeEqual(m[3], await hmacHex(keyHex, visitorMessage(m[1], m[2])));
}

function visitorMessage(id: string, at: string): string {
  return `visitor|${id}|${at}`;
}

// legacyCookie is the one cookie every visitor of a tunnel was given before
// visitor cookies. A tunnel that predates them takes it until its grace ends.
export async function legacyCookie(keyHex: string): Promise<string> {
  return hmacHex(keyHex, "ok");
}

export async function hmacHex(keyHex: string, message: string): Promise<string> {
  const keyBytes = fromHex(keyHex);
  const key = await crypto.subtle.importKey(
    "raw", keyBytes.buffer as ArrayBuffer, { name: "HMAC", hash: "SHA-256" }, false, ["sign"],
  );
  const data = new TextEncoder().encode(message);
  const sig = await crypto.subtle.sign("HMAC", key, data.buffer as ArrayBuffer);
  return toHex(new Uint8Array(sig));
}

// timingSafeEqual compares two strings without short-circuiting on the first
// differing character, for secret tokens (the auth-cookie HMAC). A plain !==
// returns as soon as it finds a mismatch, so the time it takes leaks how long a
// prefix matched, in principle letting a network attacker recover the expected
// value one character at a time. The length is not secret (the HMAC hex length is
// fixed and public), so an early length check is fine; the content compare is not
// short-circuited. Matches === semantics exactly for equal-length inputs.
export function timingSafeEqual(a: string, b: string): boolean {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i++) diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
  return diff === 0;
}

export function toHex(b: Uint8Array): string {
  let s = "";
  for (let i = 0; i < b.length; i++) s += b[i].toString(16).padStart(2, "0");
  return s;
}

export function fromHex(s: string): Uint8Array {
  const out = new Uint8Array(s.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16);
  return out;
}
