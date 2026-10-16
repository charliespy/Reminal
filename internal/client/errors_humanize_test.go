// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"errors"
	"strings"
	"testing"
)

// A machine that can't verify the relay's certificate is told about CA
// certificates; other TLS failures keep the general message.
func TestHumanizeUntrustedCertificate(t *testing.T) {
	got := humanize(errors.New("dial relay: tls: failed to verify certificate: x509: certificate signed by unknown authority"))
	if !strings.Contains(got, "ca-certificates") {
		t.Fatalf("unknown authority: got %q", got)
	}
	got = humanize(errors.New("dial relay: remote error: tls: handshake failure"))
	if strings.Contains(got, "ca-certificates") || !strings.Contains(got, "TLS handshake failed") {
		t.Fatalf("other TLS failure: got %q", got)
	}
}
