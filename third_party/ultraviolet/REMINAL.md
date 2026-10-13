# This copy of charmbracelet/ultraviolet

This is `github.com/charmbracelet/ultraviolet` at v0.0.0-20260622092850-f39628c8a989 (MIT, see LICENSE). reminal uses it in place of the module (`replace` in the root go.mod) for one change.

**The change:** `Buffer.InsertLineArea` and `Buffer.DeleteLineArea` move whole lines (their slice headers, `rotateLines`) when the area is as wide as the buffer, instead of copying every cell.

Every newline at the bottom of a terminal scrolls through `DeleteLineArea`. The cell-by-cell copy made each one O(width × height) cell copies, 112 bytes a cell. In reminal that was 81% of an agent's CPU while a session printed, and it capped the emulator at 0.2–1.3 MB/s.

**Measured** (`BenchmarkReminalScroll` here, `BenchmarkEmulatorScroll` in internal/client, Linux arm64):

| window | one scroll, before → after | emulator, before → after |
|---|---|---|
| 80×24 | 41.9 → 1.9 µs | 1.43 → 5.57 MB/s |
| 160×45 | 156 → 3.8 µs | 0.46 → 4.19 MB/s |
| 240×70 | 355 → 5.7 µs | 0.20 → 3.55 MB/s |

A headless session printing ~1,000 lines a second at 80×24 went from 7.8% to 3.4% of a core with no viewer, and from 17% to 5.5% with one.

**Partial-width areas** (left/right margins) keep the per-cell path. The lines pushed out are reused for the new blank lines, cleared by the same code as before. So no row shares a line with another. `buffer_reminal_test.go` checks both paths against the old cell copy on random areas.

**Upstream:** the change is meant for charmbracelet/ultraviolet. Once a release has it, drop this copy and the `replace`.
