// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// AES-GCM envelope encryption constants.
const (
	AES256KeySize = 32
	GCMNonceSize  = 12
)

// AESGCMCrypter implements SecretCrypter using AES-256-GCM envelope
// encryption to secure TOTP secrets at rest.
type AESGCMCrypter struct {
	key []byte
}

// NewAESGCMCrypter constructs a new AESGCMCrypter using a 32-byte master key.
func NewAESGCMCrypter(key []byte) (*AESGCMCrypter, error) {
	if len(key) != AES256KeySize {
		return nil, fmt.Errorf(
			"invalid key size: expected %d bytes, got %d",
			AES256KeySize,
			len(key),
		)
	}
	keyCopy := make([]byte, AES256KeySize)
	copy(keyCopy, key)
	return &AESGCMCrypter{key: keyCopy}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM with a random 12-byte nonce,
// returning a base64-encoded envelope containing nonce + ciphertext + tag.
func (c *AESGCMCrypter) Encrypt(plaintext []byte) (string, error) {
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", fmt.Errorf("creating aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("creating gcm cipher: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generating gcm nonce: %w", err)
	}

	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt decodes and authenticates the base64-encoded envelope, returning
// the original plaintext.
func (c *AESGCMCrypter) Decrypt(ciphertext string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decoding base64 ciphertext: %w", err)
	}

	block, err := aes.NewCipher(c.key)
	if err != nil {
		return nil, fmt.Errorf("creating aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("creating gcm cipher: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return nil, errors.New("ciphertext payload shorter than nonce size")
	}

	nonce := raw[:nonceSize]
	enc := raw[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, enc, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypting gcm envelope: %w", err)
	}

	return plaintext, nil
}

// NewRandomKey generates a cryptographically secure 32-byte key for
// AES-256-GCM.
func NewRandomKey() ([]byte, error) {
	key := make([]byte, AES256KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("reading entropy for master key: %w", err)
	}
	return key, nil
}

// LoadOrCreateMasterKey reads the 32-byte AES key from keyPath. If the file
// does not exist, a new 32-byte random key is generated and saved with 0600
// permissions. If keyPath is empty, an ephemeral random key is generated.
func LoadOrCreateMasterKey(keyPath string) ([]byte, error) {
	if keyPath == "" {
		return NewRandomKey()
	}

	cleanPath := filepath.Clean(keyPath)
	// #nosec G304 -- key file path is explicitly configured by operator
	keyData, err := os.ReadFile(cleanPath)
	if err == nil {
		if len(keyData) == AES256KeySize {
			return keyData, nil
		}
		// If key was stored in base64
		if dec, bErr := base64.StdEncoding.DecodeString(
			string(keyData),
		); bErr == nil && len(dec) == AES256KeySize {
			return dec, nil
		}
		return nil, fmt.Errorf(
			"invalid master key file size %d in %s (expected 32)",
			len(keyData),
			keyPath,
		)
	}

	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading master key %s: %w", keyPath, err)
	}

	dir := filepath.Dir(keyPath)
	if mErr := os.MkdirAll(dir, 0700); mErr != nil {
		return nil, fmt.Errorf("creating master key directory %s: %w", dir, mErr)
	}

	newKey, err := NewRandomKey()
	if err != nil {
		return nil, err
	}

	if wErr := os.WriteFile(keyPath, newKey, 0600); wErr != nil {
		return nil, fmt.Errorf("writing master key %s: %w", keyPath, wErr)
	}

	return newKey, nil
}

// PlaintextCrypter is a test double that stores data in base64 without
// encryption.
type PlaintextCrypter struct{}

// NewPlaintextCrypter returns a crypter test double.
func NewPlaintextCrypter() *PlaintextCrypter {
	return &PlaintextCrypter{}
}

// Encrypt encodes plaintext to base64.
func (p *PlaintextCrypter) Encrypt(plaintext []byte) (string, error) {
	return base64.StdEncoding.EncodeToString(plaintext), nil
}

// Decrypt decodes base64 back to plaintext.
func (p *PlaintextCrypter) Decrypt(ciphertext string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(ciphertext)
}
