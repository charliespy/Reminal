// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

// Command viewerbuild writes the viewer's script, internal/client/web/app.js,
// to its content-named asset beside both copies of the page and stamps the
// page's tag for it (see internal/viewerbuild). Run from the repository root
// after changing app.js:
//
//	go run ./cmd/viewerbuild
//
// With -check it changes nothing and fails if anything is out of date.
package main

import (
	"flag"
	"fmt"
	"os"

	"reminal/internal/viewerbuild"
)

func main() {
	check := flag.Bool("check", false, "change nothing; fail if the build is out of date")
	flag.Parse()
	changed, err := viewerbuild.Build(".", !*check)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, f := range changed {
		fmt.Println(f)
	}
	if *check && len(changed) > 0 {
		fmt.Fprintln(os.Stderr, "viewerbuild: out of date — run `go run ./cmd/viewerbuild`")
		os.Exit(1)
	}
}
