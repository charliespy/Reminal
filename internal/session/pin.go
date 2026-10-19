// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package session

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

const pinDigits = "0123456789"

// PINLength is how many digits a new session's PIN has. ValidatePIN takes 6
// to 8, so a session that already has a six-digit PIN keeps it.
const PINLength = 8

func NewPIN(length int) (string, error) {
	pin := make([]byte, length)
	max := big.NewInt(10)
	for i := range pin {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		pin[i] = pinDigits[n.Int64()]
	}
	return string(pin), nil
}

// exposeAlphabet is Crockford's base32: it leaves out I, L, O and U, so no
// character in a PIN can be read as another. The gate in front of a forwarded
// port reads a typed O as 0 and an I or L as 1, and ignores case.
const exposeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ExposePINLength is how long the PIN of a forwarded port (`reminal expose`)
// is: 60 bits. Its link carries it, so it is rarely typed, and anyone who
// finds the port's address faces a guess no rate limit has to make hopeless.
const ExposePINLength = 12

// NewExposePIN is a fresh PIN for a forwarded port.
func NewExposePIN() (string, error) {
	b := make([]byte, ExposePINLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = exposeAlphabet[b[i]&31] // 32 divides 256: no bias
	}
	return string(b), nil
}

func ValidatePIN(pin string) error {
	if len(pin) < 6 || len(pin) > 8 {
		return fmt.Errorf("PIN must be 6–8 digits")
	}
	for _, c := range pin {
		if c < '0' || c > '9' {
			return fmt.Errorf("PIN must be numeric")
		}
	}
	return nil
}
