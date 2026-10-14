// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package pty

import (
	"errors"
	"io"
)

var errWriteStopped = errors.New("write stopped")

type written struct {
	n   int
	err error
}

func writeCancel(w io.Writer, p []byte, stop <-chan struct{}) (int, error) {
	if stop == nil {
		return w.Write(p)
	}
	select {
	case <-stop:
		return 0, errWriteStopped
	default:
	}
	done := make(chan written, 1)
	go func() {
		n, err := w.Write(p)
		done <- written{n, err}
	}()
	select {
	case r := <-done:
		return r.n, r.err
	case <-stop:
		return 0, errWriteStopped
	}
}
