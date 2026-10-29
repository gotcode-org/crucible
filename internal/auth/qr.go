// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package auth

import (
	"fmt"
	"strings"

	"github.com/skip2/go-qrcode"
)

// GenerateQRCodeSVG renders a vector SVG QR code representation of the given
// content (such as an otpauth:// URI). The returned SVG is self-contained and
// crisp across all display densities.
func GenerateQRCodeSVG(content string, displaySize int) (string, error) {
	qr, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", fmt.Errorf("encoding qr code: %w", err)
	}

	bm := qr.Bitmap()
	height := len(bm)
	if height == 0 {
		return "", fmt.Errorf("empty qr code bitmap")
	}
	width := len(bm[0])

	if displaySize <= 0 {
		displaySize = 256
	}

	var sb strings.Builder
	// SVG Header
	fmt.Fprintf(
		&sb,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" `+
			`width="%d" height="%d" shape-rendering="crispEdges">`,
		width,
		height,
		displaySize,
		displaySize,
	)
	sb.WriteString("\n")
	// White background rect
	fmt.Fprintf(
		&sb,
		`  <rect width="%d" height="%d" fill="#ffffff"/>`+"\n",
		width,
		height,
	)
	// Dark modules as SVG path
	sb.WriteString(`  <path fill="#0a0a0c" d="`)
	for y, row := range bm {
		for x, val := range row {
			if val {
				fmt.Fprintf(&sb, "M%d,%dh1v1h-1z ", x, y)
			}
		}
	}
	sb.WriteString(`"/>` + "\n</svg>\n")

	return sb.String(), nil
}

// GenerateQRCodeTerminal renders a compact ASCII/Unicode representation of
// the QR code suitable for ANSI operator terminals.
func GenerateQRCodeTerminal(content string) (string, error) {
	qr, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", fmt.Errorf("encoding terminal qr code: %w", err)
	}
	return qr.ToSmallString(false), nil
}
