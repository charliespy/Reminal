// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package session

import (
	"errors"
	"fmt"
	"io"
)

// What a record may be, at most: a session's active record is a few KB, its
// restore record (sealed) tens of KB.
const (
	maxActiveRecord  = 1 << 20
	maxRestoreRecord = 16 << 20
)

// readRecord reads one of a session's records: a regular file, opened
// through no link at its name and never waiting (openRecord), and at most
// max bytes. Whoever asks after a session reads them, and the folder they
// are in may be written by someone else than the reader (a process of
// root's reading a user's): a FIFO put at the name held the read, and
// whoever asked, for ever; a link there led to another file; a huge file was
// read whole.
func readRecord(path string, max int64) ([]byte, error) {
	f, err := openRecord(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", path)
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New(path + ": larger than a record can be")
	}
	return b, nil
}
