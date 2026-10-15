// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

// The two share texts, word for word the web viewer's shareHumanText and
// shareAgentText (internal/client/web/app.js), so a session shared from VS Code
// reads the same as one shared from a phone. test/share.test.ts runs the
// viewer's own functions next to these and fails if they drift apart.
//
// The viewer's one extra case, a PIN not yet known, can't happen here: a
// session is only shared once `reminal info` has given its PIN.

export function shareHumanText(id: string, pin: string, url: string): string {
  id = id.toUpperCase();
  let t = "Join this reminal terminal:\n\n" + url;
  if (id && pin) t += "\n\nOr from a terminal:\nreminal connect " + id + " " + pin;
  t += "\n\nAnyone with these details can type in the same shell.";
  return t;
}

export function shareAgentText(id: string, pin: string, url: string): string {
  id = id.toUpperCase();
  return [
    "You are an AI agent. Connect to this reminal session (a live shared terminal) and drive it.",
    "",
    "If reminal is not installed:",
    "  macOS / Linux:  curl -fsSL https://reminal.app/install.sh | sh",
    "  Windows:        irm https://reminal.app/install.ps1 | iex",
    "",
    "Then run this in a terminal that is NOT already inside reminal:",
    "  reminal connect " + id + (pin ? " " + pin : ""),
    "",
    "That attaches your terminal to the remote shell. Type commands as you would locally. The host and other viewers see the same output.",
    "",
    "To leave without stopping the host, press Ctrl-].",
    "",
    "Browser join (same session):",
    "  " + url,
    "",
    "Session: " + id,
    "PIN: " + pin,
    "",
    "Do not share these credentials unless the user asked you to.",
  ].join("\n");
}
