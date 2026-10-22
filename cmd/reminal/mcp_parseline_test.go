// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

// A request line the server cannot read is answered, through the loop's one
// way out (send), not dropped: -32700 for a line that is not JSON (one brace
// too many, as a hand-built call made it), -32600 for JSON that is not a
// request object, each with a null id. A good line is read and answers
// nothing here.
func TestMCPParseLineAnswersWhatItCannotRead(t *testing.T) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	send := func(m map[string]any) { _ = enc.Encode(m) }
	type answer struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	for _, c := range []struct {
		line string
		code int
	}{
		{`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"x","arguments":{"name":"y"}}}}`, -32700},
		{`{"jsonrpc":"2.0","id":2,`, -32700},
		{`[1,2]`, -32600},
		{`"tools/list"`, -32600},
	} {
		buf.Reset()
		if _, ok := mcpParseLine(c.line, send); ok {
			t.Errorf("%s: read as a request", c.line)
			continue
		}
		var a answer
		if err := json.Unmarshal(buf.Bytes(), &a); err != nil {
			t.Errorf("%s: no answer written (%q)", c.line, buf.String())
			continue
		}
		if a.JSONRPC != "2.0" || string(a.ID) != "null" || a.Error.Code != c.code || a.Error.Message == "" {
			t.Errorf("%s: answered %s, want code %d with a null id", c.line, buf.Bytes(), c.code)
		}
	}
	buf.Reset()
	msg, ok := mcpParseLine(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`, send)
	if !ok || msg.Method != "tools/list" || buf.Len() != 0 {
		t.Fatalf("a good line: ok=%v method=%q wrote %q", ok, msg.Method, buf.String())
	}
}
