// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

// A request body held no further than a cap. A body sent without its length
// (chunked) is not known to be too big until it is read, and one read whole
// first — request.arrayBuffer(), formData() — is held whole, whatever is
// refused after: up to the platform's own request ceiling, near a room's
// memory. Past the cap the rest is still read, and let go as it comes: an
// answer sent before the body is read leaves the Worker that passes it on
// reading a request already answered, which throws there.

// BodyBudget is what bodies read at once may hold together: each under its
// own cap, several at once still are not to add up past this.
export class BodyBudget {
  held = 0;
  readonly max: number;
  constructor(max: number) { this.max = max; }
  take(n: number): boolean {
    if (this.held + n > this.max) return false;
    this.held += n;
    return true;
  }
  give(n: number): void {
    this.held = Math.max(0, this.held - n);
  }
}

// readBody is a body's bytes, or "large" past max, or "busy" past what the
// budget has left — held no further either way (the rest read and let go),
// and what was taken of the budget given back. Read whole, the bytes stay taken from the budget until
// the caller gives them back (give(bytes.byteLength)).
export async function readBody(body: ReadableStream<Uint8Array> | null, max: number, budget?: BodyBudget): Promise<Uint8Array<ArrayBuffer> | "large" | "busy"> {
  if (!body) return new Uint8Array(0);
  const reader = body.getReader();
  const parts: Uint8Array[] = [];
  let n = 0;
  let refused: "large" | "busy" | null = null;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      if (n + value.byteLength > max) { refused = "large"; break; }
      if (budget && !budget.take(value.byteLength)) { refused = "busy"; break; }
      n += value.byteLength;
      parts.push(value);
    }
  } catch {
    refused = "large";
  }
  if (refused) {
    budget?.give(n);
    parts.length = 0;
    try {
      for (;;) if ((await reader.read()).done) break;
    } catch {
      // Gone with the sender: nothing more to read.
    }
    return refused;
  }
  const all = new Uint8Array(n);
  let o = 0;
  for (const p of parts) { all.set(p, o); o += p.byteLength; }
  return all;
}
