// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package session

import "os"

// openRecord: Windows has no FIFO at a file's name; the file as it is.
func openRecord(path string) (*os.File, error) { return os.Open(path) }
