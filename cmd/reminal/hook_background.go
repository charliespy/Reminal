// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"reminal/internal/session"
)

// hookBackground reads, from a hook's payload, the work its harness still
// has running in the background. Claude Code's Stop and SubagentStop carry it
// as background_tasks: each an id, a type ("shell" or "subagent") and a
// status. Only one whose status says running is counted, and only a shell or
// a subagent; anything else about an entry, or an entry that cannot be read,
// is passed over. except is the subagent the event is about (SubagentStop's
// agent_id): it is finishing, whatever the list says of it.
//
// nil when the payload has no such list at all — a harness, or an older
// build, that does not say — which leaves what the screen says to stand.
func hookBackground(payload []byte, except string) *session.Background {
	var p struct {
		Tasks *[]json.RawMessage `json:"background_tasks"`
	}
	if json.Unmarshal(payload, &p) != nil || p.Tasks == nil {
		return nil
	}
	bg := &session.Background{}
	for _, raw := range *p.Tasks {
		var t struct {
			ID     json.RawMessage `json:"id"`
			Type   string          `json:"type"`
			Status string          `json:"status"`
		}
		if json.Unmarshal(raw, &t) != nil || !strings.EqualFold(t.Status, "running") {
			continue
		}
		if except != "" && strings.Trim(string(t.ID), `"`) == except {
			continue
		}
		switch strings.ToLower(t.Type) {
		case "shell":
			bg.Shells++
		case "subagent":
			bg.Agents++
		}
	}
	return bg
}

// hookAgentID is the subagent a SubagentStop is about.
func hookAgentID(payload []byte) string {
	var p struct {
		AgentID string `json:"agent_id"`
	}
	_ = json.Unmarshal(payload, &p)
	return p.AgentID
}

// hookDebugBackground says, with REMINAL_HOOK_DEBUG set, what a hook made of
// its event's background work: the counts, never the payload.
func hookDebugBackground(id, event string, bg *session.Background) {
	if os.Getenv("REMINAL_HOOK_DEBUG") == "" {
		return
	}
	said := "no background_tasks"
	if bg != nil {
		said = fmt.Sprintf("shells=%d agents=%d", bg.Shells, bg.Agents)
	}
	_ = session.AppendHookDebug(fmt.Sprintf("session=%s event=%s %s", id, event, said))
}
