// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"regexp"
	"strings"
	"testing"
)

// Every owner on a machine's channel hears every reply, and a directory
// request takes only the one carrying its own id: an id that can come out
// empty or a few characters long (Math.random's base-36 tail) could take
// another owner's reply for its own. The viewer's ids are random bytes.
func TestTheViewersDirectoryRequestIdsAreRandomBytes(t *testing.T) {
	for _, dir := range []string{"web", hostedPublic} {
		s := viewerText(t, dir)
		if regexp.MustCompile(`reqId\s*=\s*Math\.random`).MatchString(s) {
			t.Errorf("%s: a directory request id from Math.random", dir)
		}
		if !strings.Contains(s, "att.reqId = dirReqId()") || !regexp.MustCompile(`function dirReqId\(\) \{\s*return Array\.from\(crypto\.getRandomValues\(new Uint8Array\(8\)\)`).MatchString(s) {
			t.Errorf("%s: the directory request ids are not dirReqId's random bytes", dir)
		}
	}
}
