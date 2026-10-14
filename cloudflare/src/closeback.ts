// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

// closeBack answers a peer's close: a WebSocket's closing handshake is the
// peer's close frame and ours back. A Durable Object's webSocketClose that
// sends none leaves the peer waiting for it — a browser fires its onclose
// only then, about ten seconds on — so a page that hung up a socket itself
// (a dead socket it found, a reconnect) learned it was closed, and dialled
// again, that much later. The peer's own code goes back where it may be sent,
// else 1000; a socket already closed is left as it is.
export function closeBack(ws: WebSocket, code?: number, reason?: string): void {
  const sendable = code !== undefined && code >= 1000 && code < 5000 && ![1004, 1005, 1006, 1015].includes(code);
  // A close reason is at most 123 bytes.
  let why = reason ?? "";
  while (new TextEncoder().encode(why).length > 123) why = why.slice(0, -1);
  try { ws.close(sendable ? code : 1000, why); } catch { /* closed already */ }
}
