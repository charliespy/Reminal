#!/bin/bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# The session's shell for run-second.sh: claude; once it is suspended (Ctrl-Z),
# the shell at its prompt for a while; then a second claude, left there.
set -m
claude
# What a shell says as it takes the terminal back, and its prompt.
printf '\nzsh: suspended  claude\n$ '
read -r -t 6 _ || true
printf 'claude\n'
claude
