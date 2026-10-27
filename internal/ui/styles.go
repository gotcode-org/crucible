// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

// Package ui provides terminal styling, layouts, adaptive colors, and formatting helpers
// for Crucible command-line binaries using Lip Gloss.
package ui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
)

// GetTerminalWidth detects the terminal width, falling back to 80-100 columns.
func GetTerminalWidth() int {
	w, _, err := term.GetSize(os.Stdout.Fd())
	if err != nil || w <= 0 {
		return 100
	}
	if w < 80 {
		return 80
	}
	return w
}

var (
	// Subtle defines a muted gray adaptive color for borders and dividers.
	Subtle = lipgloss.AdaptiveColor{Light: "#D9D9D9", Dark: "#383838"}

	// Magenta defines an accent purple/magenta color.
	Magenta = lipgloss.AdaptiveColor{Light: "#EE6FF8", Dark: "#EE6FF8"}

	// Cyan defines a crisp cyan color for labels and identifiers.
	Cyan = lipgloss.AdaptiveColor{Light: "#02BAEF", Dark: "#02BAEF"}

	// Green defines a success indicator color.
	Green = lipgloss.AdaptiveColor{Light: "#02BA59", Dark: "#02BA59"}

	// Yellow defines a warning indicator color.
	Yellow = lipgloss.AdaptiveColor{Light: "#EAD027", Dark: "#EAD027"}

	// Red defines an error indicator color.
	Red = lipgloss.AdaptiveColor{Light: "#FF4672", Dark: "#FF4672"}

	// Gray defines secondary descriptive text color.
	Gray = lipgloss.AdaptiveColor{Light: "#828282", Dark: "#828282"}

	// BoldStyle renders bold text.
	BoldStyle = lipgloss.NewStyle().Bold(true)

	// TitleStyle renders high-contrast section headers.
	TitleStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#FFFFFF")).
			Bold(true)

	// HeaderStyle renders underlined magenta section titles.
	HeaderStyle = lipgloss.NewStyle().
			Foreground(Magenta).
			Bold(true).
			Underline(true)

	// LabelStyle renders bold cyan key labels.
	LabelStyle = lipgloss.NewStyle().
			Foreground(Cyan).
			Bold(true)

	// BorderStyle renders standard divider borders.
	BorderStyle = lipgloss.NewStyle().
			Foreground(Subtle).
			Border(lipgloss.NormalBorder(), true, false, true, false)

	// UsageStyle adds padding around command usage text.
	UsageStyle = lipgloss.NewStyle().Padding(1, 2)

	// CommandStyle highlights command names in purple.
	CommandStyle = lipgloss.NewStyle().Foreground(Magenta).Bold(true).Width(14)

	// DescriptionStyle renders dimmed command descriptions.
	DescriptionStyle = lipgloss.NewStyle().Foreground(Gray)

	// LogoStyle renders the Crucible ASCII banner in bold purple.
	LogoStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)

	// HelpTitleStyle renders the CLI help header.
	HelpTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#FFFFFF")).
			Padding(0, 1).
			Bold(true).
			MarginBottom(1)

	// HelpDescStyle renders command descriptions in italic gray.
	HelpDescStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AFB1B6")).
			Italic(true).
			MarginBottom(1)

	// HelpSectionStyle renders help section headers.
	HelpSectionStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7D56F4")).
				Bold(true).
				MarginTop(1)

	// HelpFlagStyle renders flag documentation text.
	HelpFlagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#AFB1B6"))
)

// SuccessMsg returns a formatted green success checkmark message.
func SuccessMsg(format string, args ...any) string {
	msg := fmt.Sprintf(format, args...)
	return BoldStyle.Foreground(Green).Render("✔ " + msg)
}

// ErrorMsg returns a formatted red error cross message.
func ErrorMsg(format string, args ...any) string {
	msg := fmt.Sprintf(format, args...)
	return BoldStyle.Foreground(Red).Render("✘ Error: " + msg)
}

// WarningMsg returns a formatted yellow warning message.
func WarningMsg(format string, args ...any) string {
	msg := fmt.Sprintf(format, args...)
	return BoldStyle.Foreground(Yellow).Render("! " + msg)
}

// BadgeActive renders a styled green ACTIVE status badge.
func BadgeActive(text string) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#02BA59")).
		Padding(0, 1).
		Render(text)
}

// BadgeDrain renders a styled yellow DRAIN status badge.
func BadgeDrain(text string) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color("#EAD027")).
		Padding(0, 1).
		Render(text)
}

// BadgeDown renders a styled red DOWN status badge.
func BadgeDown(text string) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#FF4672")).
		Padding(0, 1).
		Render(text)
}

// BadgeSealed renders a styled amber SEALED status badge.
func BadgeSealed(text string) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#D97706")).
		Padding(0, 1).
		Render(text)
}
