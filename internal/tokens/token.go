// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package tokens

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	// TokenPrefix is the standard prefix for all Crucible Personal Access
	// Tokens, modeled after modern tokens for easy secret scanning.
	TokenPrefix = "cb_pat_"

	// tokenRandomBytes defines cryptographic entropy for PAT (256 bits).
	tokenRandomBytes = 32
)

// GenerateRawToken generates a cryptographically secure Personal Access Token.
// It returns the full plaintext token (to show once to the user), the short
// display prefix, and the SHA-256 storage hash.
func GenerateRawToken() (string, string, string, error) {
	buf := make([]byte, tokenRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", "", fmt.Errorf(
			"generating random token bytes: %w",
			err,
		)
	}

	hexSecret := hex.EncodeToString(buf)
	rawToken := TokenPrefix + hexSecret
	displayPrefix := TokenPrefix + hexSecret[:8] + "..."
	hash := HashToken(rawToken)

	return rawToken, displayPrefix, hash, nil
}

// HashToken calculates the SHA-256 hex digest of a plaintext token string.
func HashToken(raw string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(h[:])
}

// ValidateTokenFormat checks if the raw token string begins with the expected
// prefix and contains valid hex encoding.
func ValidateTokenFormat(raw string) bool {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, TokenPrefix) {
		return false
	}
	secret := raw[len(TokenPrefix):]
	if len(secret) != tokenRandomBytes*2 {
		return false
	}
	_, err := hex.DecodeString(secret)
	return err == nil
}
