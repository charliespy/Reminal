// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"testing"

	"reminal/internal/protocol"
)

// What a reply is to a request: its own (the id it carries), somebody else's
// (another id, or none from a machine that tags its replies), or — from a
// machine too old to tag them — its own only if it answers what was asked;
// one that does not is kept, to say what that machine is, if nothing better
// comes before the deadline.
func TestDirReplyFor(t *testing.T) {
	keys := dirQueryReq{KeysID: "S1", Keys: "eA==", ReqID: "r1"}
	for _, c := range []struct {
		name string
		req  dirQueryReq
		resp protocol.DirResponse
		want dirReplyVerdict
	}{
		{"plain asks anything", dirQueryReq{}, protocol.DirResponse{}, dirReplyMine},
		{"tagged, ours", keys, protocol.DirResponse{ReqID: "r1", Tagged: true}, dirReplyMine},
		{"tagged, another's", keys, protocol.DirResponse{ReqID: "r2", Tagged: true, KeysOK: true}, dirReplyOthers},
		{"tagged, a plain listing", keys, protocol.DirResponse{Tagged: true}, dirReplyOthers},
		{"untagged, answers keys", keys, protocol.DirResponse{KeysOK: true}, dirReplyMine},
		{"untagged, keys refused", keys, protocol.DirResponse{KeysError: "no such session"}, dirReplyMine},
		{"untagged, a plain listing", keys, protocol.DirResponse{}, dirReplyMaybe},
		{"untagged, transcript answered", dirQueryReq{Transcript: "S1", ReqID: "r1"},
			protocol.DirResponse{Sessions: []protocol.DirSession{{ID: "S1", TranscriptOK: true}}}, dirReplyMine},
		{"untagged, transcript not", dirQueryReq{Transcript: "S1", ReqID: "r1"},
			protocol.DirResponse{Sessions: []protocol.DirSession{{ID: "S1"}}}, dirReplyMaybe},
		{"untagged, transcript of a session not there", dirQueryReq{Transcript: "S9", ReqID: "r1"},
			protocol.DirResponse{Sessions: []protocol.DirSession{{ID: "S1"}}}, dirReplyMine},
		{"untagged, restart answered", dirQueryReq{Restart: true, ReqID: "r1"}, protocol.DirResponse{RestartOK: true}, dirReplyMine},
		{"untagged, restart not", dirQueryReq{Restart: true, ReqID: "r1"}, protocol.DirResponse{}, dirReplyMaybe},
		{"untagged, open answered", dirQueryReq{OpenID: "P", ReqID: "r1"}, protocol.DirResponse{OpenError: "no"}, dirReplyMine},
		{"untagged, open not", dirQueryReq{OpenID: "P", ReqID: "r1"}, protocol.DirResponse{}, dirReplyMaybe},
		{"untagged, search (cannot tell)", dirQueryReq{Pattern: "x", ReqID: "r1"}, protocol.DirResponse{}, dirReplyMine},
	} {
		if got := dirReplyFor(c.req, c.resp); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// A machine that took the request and did nothing with it is named, with its
// version when it says it.
func TestHostTooOldNamesTheVersion(t *testing.T) {
	err := hostTooOld("accept keystrokes", protocol.DirResponse{Stats: &protocol.MachineStats{Version: "3.1.4"}})
	if got := err.Error(); got != "its reminal (3.1.4) is too old to accept keystrokes — upgrade it" {
		t.Fatalf("%q", got)
	}
	if got := hostTooOld("dump scrollback", protocol.DirResponse{}).Error(); got != "its reminal is too old to dump scrollback — upgrade it" {
		t.Fatalf("%q", got)
	}
}
