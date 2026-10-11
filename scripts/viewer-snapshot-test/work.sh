#!/bin/bash
# The session's shell: a coding agent's long answer first — 4000
# syntax-coloured lines of about 600 bytes each (8 colours, a foreground and
# a background a word), none wider than the session — then a shell.
gawk 'BEGIN {
  srand(1)
  split("the agent read the file and found that the function returns early when the buffer is empty so the caller never sees the error", w, " ")
  split("215;119;87 86;156;214 78;201;176 220;220;170 197;134;192 156;220;254 206;145;120 181;206;168", c, " ")
  for (i = 1; i <= 4000; i++) {
    s = "line " i " "; vis = 0
    # Under 80 columns (the default width of a session: no line wraps), about 600
    # bytes: bold, a foreground and a background colour a word.
    while (vis < 60) { x = w[int(rand() * 25) + 1]; s = s "\033[1;38;2;" c[int(rand() * 8) + 1] "m\033[48;2;" c[int(rand() * 8) + 1] "m" x "\033[0m "; vis += length(x) + 1 }
    print s
  }
}'
touch /tmp/answer-done
exec bash
