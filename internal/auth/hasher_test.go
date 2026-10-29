// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	t.Parallel()

	password := "CorrectHorseBatteryStaple#2026!"

	encoded, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Errorf("unexpected hash format: %s", encoded)
	}

	// Verify valid password
	ok, err := VerifyPassword(password, encoded)
	if err != nil {
		t.Fatalf("VerifyPassword failed: %v", err)
	}
	if !ok {
		t.Errorf("expected valid password verification to succeed")
	}

	// Verify invalid password
	ok, err = VerifyPassword("WrongPassword", encoded)
	if err != nil {
		t.Fatalf("VerifyPassword with wrong password failed: %v", err)
	}
	if ok {
		t.Errorf("expected wrong password verification to fail")
	}
}

func TestHashPassword_Empty(t *testing.T) {
	t.Parallel()

	_, err := HashPassword("")
	if err == nil {
		t.Errorf("expected error hashing empty password, got nil")
	}
}

func TestVerifyPassword_Malformed(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		hash string
	}{
		{name: "empty", hash: ""},
		{name: "wrong_parts", hash: "plain_text"},
		{name: "wrong_algo", hash: "$bcrypt$v=19$m=65536,t=3,p=4$salt$hash"},
		{name: "wrong_version", hash: "$argon2id$v=18$m=65536,t=3,p=4$salt$hash"},
		{name: "bad_params", hash: "$argon2id$v=19$badparams$salt$hash"},
		{name: "bad_salt_b64", hash: "$argon2id$v=19$m=65536,t=3,p=4$!@#$hash"},
		{name: "bad_hash_b64", hash: "$argon2id$v=19$m=65536,t=3,p=4$salt$!@#"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := VerifyPassword("pass", tc.hash)
			if err == nil {
				t.Errorf("expected error for malformed hash, got nil")
			}
			if ok {
				t.Errorf("expected false for malformed hash, got true")
			}
		})
	}
}
