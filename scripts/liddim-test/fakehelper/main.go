// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

// A stand-in capture helper for check.sh: records how it was started (argv
// and the DYLD_/LD_/REMINAL_ variables it was given), then behaves like a lid
// dimmer that holds until stdin closes.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func main() {
	f, _ := os.OpenFile("/tmp/liddim-test/calls.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	var env []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "DYLD_") || strings.HasPrefix(kv, "LD_") || strings.HasPrefix(kv, "REMINAL_") {
			env = append(env, kv)
		}
	}
	fmt.Fprintf(f, "%s %s | %s\n", os.Args[0], strings.Join(os.Args[1:], " "), strings.Join(env, " "))
	f.Close()
	if len(os.Args) > 1 && os.Args[1] == "liddim" {
		io.Copy(io.Discard, os.Stdin)
		return
	}
	os.Exit(1)
}
