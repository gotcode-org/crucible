// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Default Argon2id parameter constants recommended by OWASP.
const (
	Argon2Time      uint32 = 3
	Argon2Memory    uint32 = 64 * 1024 // 64 MB (65536 KiB)
	Argon2Threads   uint8  = 4
	Argon2KeyLen    uint32 = 32
	Argon2SaltLen          = 16
	Argon2Version19        = 19
)

// ErrInvalidHash indicates a password hash string is malformed or
// unsupported.
var ErrInvalidHash = errors.New("malformed or unsupported password hash")

// HashPassword hashes a plaintext password using Argon2id with
// cryptographically secure random salt, returning a standard PHC formatted
// string: $argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>
func HashPassword(password string) (string, error) {
	if len(password) == 0 {
		return "", errors.New("password cannot be empty")
	}

	salt := make([]byte, Argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generating argon2 salt: %w", err)
	}

	hash := argon2.IDKey(
		[]byte(password),
		salt,
		Argon2Time,
		Argon2Memory,
		Argon2Threads,
		Argon2KeyLen,
	)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		Argon2Version19,
		Argon2Memory,
		Argon2Time,
		Argon2Threads,
		b64Salt,
		b64Hash,
	), nil
}

// VerifyPassword verifies a plaintext password against an Argon2id PHC hash.
// Returns true if the password matches, false if it does not, or an error if
// the hash string is malformed.
func VerifyPassword(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return false, ErrInvalidHash
	}

	if parts[1] != "argon2id" {
		return false, fmt.Errorf("%w: unrecognized algorithm", ErrInvalidHash)
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, fmt.Errorf("%w: invalid version", ErrInvalidHash)
	}
	if version != Argon2Version19 {
		return false, fmt.Errorf("%w: incompatible version", ErrInvalidHash)
	}

	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(
		parts[3],
		"m=%d,t=%d,p=%d",
		&memory,
		&time,
		&threads,
	); err != nil {
		return false, fmt.Errorf("%w: invalid parameters", ErrInvalidHash)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("%w: invalid salt encoding", ErrInvalidHash)
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("%w: invalid hash encoding", ErrInvalidHash)
	}

	if len(expectedHash) == 0 || len(expectedHash) > 1024 {
		return false, fmt.Errorf(
			"%w: invalid hash length",
			ErrInvalidHash,
		)
	}

	keyLen := uint32(len(expectedHash)) // #nosec G115 -- bounded
	computed := argon2.IDKey(
		[]byte(password),
		salt,
		time,
		memory,
		threads,
		keyLen,
	)

	if subtle.ConstantTimeCompare(computed, expectedHash) == 1 {
		return true, nil
	}

	return false, nil
}
