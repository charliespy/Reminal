// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/charmbracelet/x/vt"
)

// How fast the emulator takes a session's output: 20,000 lines, each one
// scrolling the screen, with 10,000 lines of scrollback, at three window
// sizes. Scrolling is what third_party/ultraviolet's line moves are for.
//
//	go test -run xxx -bench BenchmarkEmulatorScroll ./internal/client/
func BenchmarkEmulatorScroll(b *testing.B) {
	var data bytes.Buffer
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&data, "line %d reminal paced output 0123456789 abcdefghijklmnopqrstuvwxyz\r\n", i)
	}
	for _, s := range []struct{ cols, rows int }{{80, 24}, {160, 45}, {240, 70}} {
		b.Run(fmt.Sprintf("%dx%d", s.cols, s.rows), func(b *testing.B) {
			e := vt.NewEmulator(s.cols, s.rows)
			e.Scrollback().SetMaxLines(10000)
			go func() { // replies the emulator writes back, drained
				buf := make([]byte, 4096)
				for {
					if _, err := e.Read(buf); err != nil {
						return
					}
				}
			}()
			defer e.Close()
			b.SetBytes(int64(data.Len()))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = e.Write(data.Bytes())
			}
		})
	}
}
