// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"gotcode.org/crucible/internal/version"
)

type kvPair struct {
	label string
	val   string
}

// calculateColumnWidths computes column 1 and column 2 widths across metadata and dependencies.
func calculateColumnWidths(lines []kvPair, deps []version.DependencyInfo) (int, int) {
	maxCol1, maxCol2 := 32, 32

	update := func(col1, col2 string) {
		if w := lipgloss.Width(col1); w > maxCol1 {
			maxCol1 = w
		}
		if w := lipgloss.Width(col2); w > maxCol2 {
			maxCol2 = w
		}
	}

	for _, l := range lines {
		update(l.label, l.val)
	}

	if deps != nil {
		if len(deps) > 0 {
			for _, dep := range deps {
				update(dep.Path, dep.Version)
			}
		} else {
			update("Dependencies", "none (pure Go standard library)")
		}
	}

	return maxCol1, maxCol2
}

// writeRow formats and prints a single key-value row with visual padding and gap spacing.
func writeRow(w io.Writer, col1, col2 string, maxCol1 int) error {
	pad := strings.Repeat(" ", maxCol1-lipgloss.Width(col1))
	_, err := fmt.Fprintf(w, "%s%s  %s\n", LabelStyle.Render(col1), pad, col2)
	return err
}

// PrintVersion formats and writes binary version, build metadata, and dependencies to w.
// It computes column widths dynamically so that all metadata values and dependency versions
// are visually aligned regardless of whether ANSI colors are enabled or stripped.
func PrintVersion(w io.Writer, info version.Info) error {
	lines := []kvPair{
		{"Application", info.AppName},
		{"Version", info.Build.Version},
		{"Git Commit", info.Build.GitCommit},
		{"Git Branch", info.Build.GitBranch},
		{"Git Tree State", info.Build.GitTreeState},
		{"Build Date", info.Build.BuildDate},
		{"Go Version", info.Build.GoVersion},
		{"Compiler", info.Build.Compiler},
		{"OS/Arch", info.Build.Platform},
	}

	maxCol1, maxCol2 := calculateColumnWidths(lines, info.Dependencies)

	for _, l := range lines {
		if err := writeRow(w, l.label, l.val, maxCol1); err != nil {
			return err
		}
	}

	if info.Dependencies == nil {
		return nil
	}

	divStyle := lipgloss.NewStyle().Foreground(Subtle)
	div1 := divStyle.Render(strings.Repeat("─", maxCol1))
	div2 := divStyle.Render(strings.Repeat("─", maxCol2))
	if _, err := fmt.Fprintf(w, "%s  %s\n", div1, div2); err != nil {
		return err
	}

	if len(info.Dependencies) > 0 {
		for _, dep := range info.Dependencies {
			if err := writeRow(w, dep.Path, dep.Version, maxCol1); err != nil {
				return err
			}
		}
		return nil
	}

	return writeRow(w, "Dependencies", "none (pure Go standard library)", maxCol1)
}
