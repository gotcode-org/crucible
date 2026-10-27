// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"gotcode.org/crucible/internal/version"
)

func TestMessages(t *testing.T) {
	t.Parallel()

	s := SuccessMsg("operation %s", "completed")
	if !strings.Contains(s, "operation completed") {
		t.Errorf("SuccessMsg() = %q, want it to contain 'operation completed'", s)
	}

	e := ErrorMsg("fatal %s", "error")
	if !strings.Contains(e, "Error: fatal error") {
		t.Errorf("ErrorMsg() = %q, want it to contain 'Error: fatal error'", e)
	}

	w := WarningMsg("check %s", "config")
	if !strings.Contains(w, "check config") {
		t.Errorf("WarningMsg() = %q, want it to contain 'check config'", w)
	}
}

func TestBadges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fn   func(string) string
		text string
	}{
		{"BadgeActive", BadgeActive, "ACTIVE"},
		{"BadgeDrain", BadgeDrain, "DRAIN"},
		{"BadgeDown", BadgeDown, "DOWN"},
		{"BadgeSealed", BadgeSealed, "SEALED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := tt.fn(tt.text)
			if !strings.Contains(out, tt.text) {
				t.Errorf("%s(%q) = %q, want it to contain %q", tt.name, tt.text, out, tt.text)
			}
		})
	}
}

func TestPrintVersion(t *testing.T) {
	t.Parallel()

	info := version.Info{
		AppName: "crucibled",
		Build: version.BuildMetadata{
			Version:      "v0.1.0-alpha.1",
			GitBranch:    "development",
			GitCommit:    "a1b2c3d",
			GitTreeState: "clean",
			BuildDate:    "2026-10-26T22:00:00Z",
			GoVersion:    "go1.24.4",
			Compiler:     "gc",
			Platform:     "linux/amd64",
		},
		Dependencies: []version.DependencyInfo{
			{Path: "github.com/spf13/cobra", Version: "v1.9.1"},
		},
	}

	var buf bytes.Buffer
	if err := PrintVersion(&buf, info); err != nil {
		t.Fatalf("PrintVersion() error: %v", err)
	}

	out := buf.String()
	wantTokens := []string{
		"crucibled",
		"v0.1.0-alpha.1",
		"development",
		"a1b2c3d",
		"linux/amd64",
		"github.com/spf13/cobra",
		"v1.9.1",
	}

	for _, token := range wantTokens {
		if !strings.Contains(out, token) {
			t.Errorf("PrintVersion() output missing token %q:\n%s", token, out)
		}
	}
}

func TestPrintVersionNoDeps(t *testing.T) {
	t.Parallel()

	info := version.Info{
		AppName: "cruciblectl",
		Build: version.BuildMetadata{
			Version:   "v0.1.0",
			Platform:  "linux/amd64",
			GitCommit: "abcdef",
		},
		Dependencies: []version.DependencyInfo{},
	}

	var buf bytes.Buffer
	if err := PrintVersion(&buf, info); err != nil {
		t.Fatalf("PrintVersion() error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "none (pure Go standard library)") {
		t.Errorf("expected no deps message, got:\n%s", out)
	}
}

func TestPrintVersionDepsOmitted(t *testing.T) {
	t.Parallel()

	info := version.Info{
		AppName: "crucibled",
		Build: version.BuildMetadata{
			Version:   "v0.1.0",
			Platform:  "linux/amd64",
			GitCommit: "abcdef",
		},
		Dependencies: nil,
	}

	var buf bytes.Buffer
	if err := PrintVersion(&buf, info); err != nil {
		t.Fatalf("PrintVersion() error: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "Dependencies") || strings.Contains(out, "───") {
		t.Errorf("expected dependencies section omitted when nil, got:\n%s", out)
	}
}

func TestPrintVersionAlignment(t *testing.T) {
	t.Parallel()

	info := version.Info{
		AppName: "crucibled",
		Build: version.BuildMetadata{
			Version:      "v0.1.0-alpha.1",
			GitBranch:    "development",
			GitCommit:    "a1b2c3d",
			GitTreeState: "clean",
			BuildDate:    "2026-10-26T22:00:00Z",
			GoVersion:    "go1.24.4",
			Compiler:     "gc",
			Platform:     "linux/amd64",
		},
		Dependencies: []version.DependencyInfo{
			{Path: "github.com/aymanbagabas/go-osc52/v2", Version: "v2.0.1"},
			{Path: "github.com/charmbracelet/lipgloss", Version: "v1.0.0"},
			{Path: "golang.org/x/sys", Version: "v0.26.0"},
		},
	}

	var buf bytes.Buffer
	if err := PrintVersion(&buf, info); err != nil {
		t.Fatalf("PrintVersion() error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) == 0 {
		t.Fatalf("expected non-empty output")
	}

	// Longest col1 item is "github.com/aymanbagabas/go-osc52/v2" (35 runes).
	// Column 2 starts at rune index 37 (35 runes + 2 spaces).
	expectedCol2Start := 37
	for i, line := range lines {
		runes := []rune(line)
		if len(runes) < expectedCol2Start {
			t.Errorf("line %d too short in runes (%d < %d): %q", i, len(runes), expectedCol2Start, line)
			continue
		}
		// Indices expectedCol2Start-2 and expectedCol2Start-1 must be spaces
		if runes[expectedCol2Start-2] != ' ' || runes[expectedCol2Start-1] != ' ' {
			t.Errorf("line %d missing gap spaces at column %d: %q", i, expectedCol2Start, line)
		}
		// Index expectedCol2Start must NOT be a space (start of col2)
		if runes[expectedCol2Start] == ' ' {
			t.Errorf("line %d col2 not aligned at index %d: %q", i, expectedCol2Start, line)
		}
	}
}

func TestHandleUsage(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{
		Use:   "testapp",
		Short: "Test Application Short Description",
		Long:  "Test Application Long Description",
	}
	cmd.Flags().String("config", "", "Path to configuration file")

	sub := &cobra.Command{
		Use:   "subcmd",
		Short: "Subcommand description",
	}
	cmd.AddCommand(sub)

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	if err := HandleUsage(cmd); err != nil {
		t.Fatalf("HandleUsage() returned error: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Test Application Short Description") {
		t.Errorf("HandleUsage() missing short description:\n%s", out)
	}
	if !strings.Contains(out, "subcmd") {
		t.Errorf("HandleUsage() missing subcommand:\n%s", out)
	}
	if !strings.Contains(out, "--config") {
		t.Errorf("HandleUsage() missing flag in output:\n%s", out)
	}
}

func TestHandleHelp(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{
		Use:   "helpapp",
		Short: "Help test app",
	}
	cmd.SetUsageFunc(HandleUsage)
	var buf bytes.Buffer
	cmd.SetOut(&buf)

	HandleHelp(cmd, []string{})
	out := buf.String()
	if !strings.Contains(out, "helpapp") {
		t.Errorf("HandleHelp() missing 'helpapp' in output:\n%s", out)
	}
}

func TestGetTerminalWidth(t *testing.T) {
	t.Parallel()

	w := GetTerminalWidth()
	if w < 80 {
		t.Errorf("GetTerminalWidth() = %d, expected >= 80", w)
	}
}
