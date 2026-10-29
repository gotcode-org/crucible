// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- RFC 6238 TOTP mandates HMAC-SHA1
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

// TOTP parameters per RFC 6238 and RFC 4226.
const (
	DefaultTOTPPeriodSeconds = 30
	DefaultTOTPDigits        = 6
	DefaultTOTPSecretBytes   = 20 // 160 bits recommended for HMAC-SHA1
	DefaultIssuer            = "Crucible"
)

var (
	// ErrInvalidSecretBase32 reports an invalid base32-encoded TOTP secret.
	ErrInvalidSecretBase32 = errors.New("invalid base32 totp secret")
)

// GenerateSecret generates a cryptographically secure 160-bit (20-byte)
// random secret encoded in unpadded Base32.
func GenerateSecret() (string, error) {
	buf := make([]byte, DefaultTOTPSecretBytes)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("reading entropy for totp secret: %w", err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf),
		nil
}

// GenerateCode computes the 6-digit TOTP passcode for the given secret at
// the specified time.
func GenerateCode(secretBase32 string, t time.Time) (string, error) {
	secret, err := decodeBase32Secret(secretBase32)
	if err != nil {
		return "", err
	}

	unixSec := t.Unix()
	if unixSec < 0 {
		return "", errors.New("timestamp precedes Unix epoch")
	}
	counter := uint64(unixSec / DefaultTOTPPeriodSeconds) // #nosec G115
	return computeHOTP(secret, counter)
}

// ValidateCode checks whether the provided 6-digit passcode matches the
// secret at time t within a drift tolerance of +/- skewSteps intervals.
// Skew of 1 step corresponds to +/- 30 seconds (total 90s window).
func ValidateCode(
	secretBase32, code string,
	t time.Time,
	skewSteps int,
) bool {
	cleanCode := strings.TrimSpace(code)
	if len(cleanCode) != DefaultTOTPDigits {
		return false
	}

	secret, err := decodeBase32Secret(secretBase32)
	if err != nil {
		return false
	}

	unixSec := t.Unix()
	if unixSec < 0 {
		return false
	}

	step := int64(DefaultTOTPPeriodSeconds)
	baseCounter := unixSec / step

	for offset := -skewSteps; offset <= skewSteps; offset++ {
		stepVal := baseCounter + int64(offset)
		if stepVal < 0 {
			continue
		}
		counter := uint64(stepVal) // #nosec G115 -- stepVal verified >= 0
		expected, err := computeHOTP(secret, counter)
		if err != nil {
			continue
		}
		if subtle.ConstantTimeCompare(
			[]byte(cleanCode),
			[]byte(expected),
		) == 1 {
			return true
		}
	}

	return false
}

// BuildOTPAuthURI constructs the canonical otpauth:// URI for authenticator
// apps like Google Authenticator, Bitwarden, Authy, and 1Password.
func BuildOTPAuthURI(username, issuer, secretBase32 string) string {
	if issuer == "" {
		issuer = DefaultIssuer
	}

	label := fmt.Sprintf("%s:%s", issuer, username)
	params := url.Values{}
	params.Set("secret", strings.ToUpper(strings.TrimSpace(secretBase32)))
	params.Set("issuer", issuer)
	params.Set("algorithm", "SHA1")
	params.Set("digits", fmt.Sprintf("%d", DefaultTOTPDigits))
	params.Set("period", fmt.Sprintf("%d", DefaultTOTPPeriodSeconds))

	return fmt.Sprintf(
		"otpauth://totp/%s?%s",
		url.PathEscape(label),
		params.Encode(),
	)
}

func decodeBase32Secret(s string) ([]byte, error) {
	clean := strings.ToUpper(strings.TrimSpace(s))
	clean = strings.ReplaceAll(clean, " ", "")
	clean = strings.ReplaceAll(clean, "-", "")

	// Support unpadded or standard padded base32
	dec := base32.StdEncoding.WithPadding(base32.NoPadding)
	if data, err := dec.DecodeString(clean); err == nil {
		return data, nil
	}

	if data, err := base32.StdEncoding.DecodeString(clean); err == nil {
		return data, nil
	}

	return nil, ErrInvalidSecretBase32
}

func computeHOTP(secret []byte, counter uint64) (string, error) {
	var counterBuf [8]byte
	binary.BigEndian.PutUint64(counterBuf[:], counter)

	mac := hmac.New(sha1.New, secret)
	if _, err := mac.Write(counterBuf[:]); err != nil {
		return "", fmt.Errorf("hashing hotp counter: %w", err)
	}
	hash := mac.Sum(nil)

	// RFC 4226 dynamic truncation
	offset := hash[len(hash)-1] & 0x0f
	binaryCode := (uint32(hash[offset]&0x7f) << 24) |
		(uint32(hash[offset+1]&0xff) << 16) |
		(uint32(hash[offset+2]&0xff) << 8) |
		uint32(hash[offset+3]&0xff)

	code := binaryCode % 1000000
	return fmt.Sprintf("%06d", code), nil
}
