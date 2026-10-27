// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

// Package main provides the entrypoint for the cruciblectl operator CLI.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gotcode.org/crucible/internal/ui"
)

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "cruciblectl",
		Aliases:       []string{"c", "cb"},
		Short:         "Crucible Operator Command-Line Interface",
		Long:          "cruciblectl is the administrative and automation client for Crucible.",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	rootCmd.SetHelpFunc(ui.HandleHelp)
	rootCmd.SetUsageFunc(ui.HandleUsage)

	rootCmd.AddCommand(versionCmd())

	return rootCmd
}

func main() {
	cmd := newRootCmd()
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, ui.ErrorMsg("%s", err))
		os.Exit(1)
	}
}
