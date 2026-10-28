// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

// Package main provides the entrypoint for the crucibled server daemon.
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
		Use:           "crucibled",
		Short:         "Crucible Automation Server Daemon",
		Long:          "Crucible is a sovereign, lightweight automation orchestrator for Ansible playbooks and systems tasks.",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	rootCmd.SetHelpFunc(ui.HandleHelp)
	rootCmd.SetUsageFunc(ui.HandleUsage)

	var configFile string
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "Path to configuration file")

	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(configCmd())
	rootCmd.AddCommand(systemdCmd())
	rootCmd.AddCommand(dbCmd())

	return rootCmd
}

func main() {
	cmd := newRootCmd()
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, ui.ErrorMsg("%s", err))
		os.Exit(1)
	}
}
