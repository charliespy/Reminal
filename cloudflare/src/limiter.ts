// A rate limiter that actually counts. The platform's rate-limiting binding
// answered "success" to every call in a burst of sixteen against a limit of
// six (its counts are per location and settle late); a Durable Object is
// one object per key, single-threaded, everywhere — so its count is the
// count. One object per caller, kept in memory: a caller that goes quiet is
// evicted and starts over, which is the right thing for a burst limit.
// Not for a limit over an hour that a caller must not reset by pausing
// (guesses at a secret, say): asked with persist, its count is kept in the
// object's storage and outlives the object.

export interface LimiterEnv {
  LIMITER?: { idFromName(name: string): unknown; get(id: unknown): { fetch(input: Request | string, init?: RequestInit): Promise<Response> } };
}

interface LimiterStorage { get<T>(key: string): Promise<T | undefined>; put(key: string, value: unknown): Promise<void> }

export class RateLimiter {
  private hits: number[] = [];
  private loaded = false;
  private storage?: LimiterStorage;
  constructor(state?: { storage?: LimiterStorage }) {
    this.storage = state?.storage;
  }
  async fetch(request: Request): Promise<Response> {
    const u = new URL(request.url);
    const limit = Math.max(1, Number(u.searchParams.get("limit")) || 6);
    const period = Math.max(1, Number(u.searchParams.get("period")) || 60) * 1000;
    const persist = u.searchParams.has("persist") && !!this.storage;
    if (persist && !this.loaded) {
      const kept = await this.storage!.get<number[]>("hits");
      if (Array.isArray(kept)) this.hits = kept.filter((t) => typeof t === "number");
      this.loaded = true;
    }
    const now = Date.now();
    const before = this.hits.length;
    this.hits = this.hits.filter((t) => now - t < period);
    let ok = true;
    // refund: a call counted that turned out not to count (refund below).
    if (u.searchParams.has("refund")) this.hits.pop();
    else {
      ok = this.hits.length < limit;
      if (ok) this.hits.push(now);
    }
    if (persist && (ok || this.hits.length !== before)) await this.storage!.put("hits", this.hits);
    return new Response(JSON.stringify({ success: ok, count: this.hits.length }), { headers: { "content-type": "application/json" } });
  }
}

// allow asks the caller's own limiter object whether one more call is
// within limit per period seconds. A limiter that cannot be reached does
// not block the call: the local allowance behind it still applies. A call
// is counted as it is asked; one that turns out not to count is given back
// (refund).
// A Worker deployed without the LIMITER binding (one self-hosted from an
// older wrangler.toml) runs on, its limits counted in each isolate alone
// (inIsolate) — said once, so whoever deploys it knows to add the binding.
let unboundSaid = false;

export async function allow(env: LimiterEnv, scope: string, key: string, limit: number, period: number, persist = false): Promise<boolean | null> {
  if (!env.LIMITER) {
    if (!unboundSaid) {
      unboundSaid = true;
      console.log("reminal: no LIMITER binding (the RateLimiter class in wrangler.toml): rate limits are counted per isolate only");
    }
    return null;
  }
  try {
    const stub = env.LIMITER.get(env.LIMITER.idFromName(scope + ":" + key));
    const r = await stub.fetch(`https://limiter/?limit=${limit}&period=${period}${persist ? "&persist=1" : ""}`);
    const body = (await r.json()) as { success?: boolean };
    return !!body.success;
  } catch {
    return null;
  }
}

// refund takes back one call counted by allow: what is limited is known
// only after the call (a redemption that failed), so it is counted first —
// calls at once each take a place within the limit, none merely looks — and
// given back when it did not count.
export async function refund(env: LimiterEnv, scope: string, key: string, limit: number, period: number, persist = false): Promise<void> {
  if (!env.LIMITER) return;
  try {
    const stub = env.LIMITER.get(env.LIMITER.idFromName(scope + ":" + key));
    await (await stub.fetch(`https://limiter/?limit=${limit}&period=${period}&refund=1${persist ? "&persist=1" : ""}`)).body?.cancel();
  } catch {
    // Not given back: one call counted too many, at worst.
  }
}

// inIsolate counts calls per key in this isolate's memory: no request to
// anything, so it can stand in front of the calls that do cost one. Nothing
// is cleared wholesale — a flood of keys must not wipe everyone's counts;
// the oldest entries go.
export function inIsolate(counts: Map<string, number[]>, key: string, limit: number, period: number): boolean {
  const now = Date.now();
  const hits = (counts.get(key) || []).filter((t) => now - t < period * 1000);
  if (hits.length >= limit) return false;
  hits.push(now);
  counts.set(key, hits);
  if (counts.size > 5000) for (const k of [...counts.keys()].slice(0, 2500)) counts.delete(k);
  return true;
}
