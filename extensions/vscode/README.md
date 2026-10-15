# reminal for VS Code

Open a terminal you can reach from any browser or phone, right from the
terminal panel. Each one is a [reminal](https://reminal.app) session: start
something in VS Code, then check on it from your phone.

Works in VS Code, Cursor, Windsurf, VSCodium and code-server.

## What it adds

- A **reminal** terminal profile. Pick it from the arrow next to the `+` in
  the terminal panel (or make it your default profile). Every terminal opened
  from it is a reminal session.
- A **cloud button** in the terminal panel's title bar. It lists the reminal
  terminals in this window, each with its state (Working, Needs you, Done),
  so you can jump to one. The last row, **New reminal Terminal Here**, opens
  a new one in the folder your active terminal is in.
- **Open reminal in This Folder** in a terminal tab's right-click menu. It
  opens a new reminal terminal in that terminal's folder. Only the folder
  carries over, so it asks whether to close the old terminal too.
- **Status beside each tab's name.** When a reminal terminal runs an agent
  such as Claude Code, its tab shows whether the agent is working (`…`),
  needs you (`!`) or is done (`✓`). A notification also tells you when one
  starts waiting for you. Turn on `reminal.statusBar` for a summary in the
  status bar too, for example "reminal: 1 needs you, 2 working".
- **Share for a Person** and **Share for an AI Agent** in a reminal
  terminal tab's right-click menu. They copy the same messages as the Share
  buttons in reminal's web page: a join link and `reminal connect` line for a
  person, or step-by-step instructions for an agent such as Claude Code.

When a reminal terminal starts, a notification shows its PIN with
**Copy link** and **Open in browser**. The terminal itself also prints the
link and a QR code to scan from your phone. To see the link again later, run
**reminal: Show Link for This Terminal** from the command palette.

## Leave it running, even with the lid closed

reminal keeps your computer awake while a session runs, so an agent can keep
working after you walk away. On a Mac you can go further and close the lid:
turn on **closed-lid mode** in reminal's settings and your sessions stay
reachable with the lid shut and nothing plugged in.

    reminal settings closed-lid on

It asks for your admin password, since it changes when the Mac sleeps.
Unplugged, it runs on battery, and a running laptop shouldn't go in a bag.
`reminal settings closed-lid off` puts everything back. You can also flip it
on the settings page that `reminal settings` opens.

## Before you start

The extension runs the `reminal` program, so install that first:

    curl -fsSL https://reminal.app/install.sh | sh

On Windows (PowerShell):

    irm https://reminal.app/install.ps1 | iex

If reminal isn't installed, the extension tells you so. **Install reminal**
runs the install command in a terminal, where you answer the installer's
questions, and offers a reminal terminal once it's done. You can also copy
the command instead. It looks on your PATH and in the folder the installer
uses, so it finds reminal even when the editor was started from the Dock or
Start menu.

## Settings

| Setting | Default | What it does |
| --- | --- | --- |
| `reminal.path` | empty | Full path to `reminal`, if it lives somewhere unusual. User settings only |
| `reminal.showLinkOnStart` | `true` | Show the link notification when a reminal terminal starts |
| `reminal.closeOriginal` | `ask` | After Open reminal in This Folder: `ask`, `always` close the old terminal, or `never` |
| `reminal.notifyNeedsYou` | `true` | Notify you when a reminal terminal starts waiting for you |
| `reminal.statusBar` | `false` | Also sum up every reminal terminal's status in the status bar |

## Good to know

- A shell that is already running in a VS Code terminal can't be moved into
  reminal, because VS Code owns that terminal. Open reminal in This Folder
  starts a new shell in the same folder instead. Your history and anything
  running in the old terminal stay there.
- VS Code doesn't let extensions put a button on each row of the terminal tab
  list, so the per-terminal actions live in the tab's right-click menu. The
  share items show there while any reminal terminal is open; on a terminal
  that isn't a reminal they offer to open one instead.
- The status needs a recent reminal (`reminal upgrade`). With an older one
  the tabs show no status, and everything else works.
- The status sits beside the tab's name in the terminal tab list and on a
  terminal in the editor area. VS Code shows no badge in the single-tab
  header it uses when only one terminal is open, and hides it while its own
  icon (such as the warning after a reload) is on the tab.
- In a remote window (Remote SSH, Dev Containers, code-server), reminal runs
  on the remote machine, so install it there.

## License

AGPL-3.0-or-later, like the rest of reminal.
