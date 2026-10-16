// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

// Windows has no process groups to list (and no /proc to list them from).
func syscallGetpgrp() int { return -1 }
