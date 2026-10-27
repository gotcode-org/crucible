// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"gotcode.org/crucible/internal/ui"
	"gotcode.org/crucible/internal/version"
)

func versionCmd() *cobra.Command {
	var showDeps bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version information and dependencies",
		RunE: func(cmd *cobra.Command, _ []string) error {
			service := version.DefaultService()
			query := version.GetVersionQuery{
				AppName:             "crucibled",
				IncludeDependencies: showDeps,
			}
			info, err := service.GetVersion(cmd.Context(), query)
			if err != nil {
				return fmt.Errorf("fetching version info: %w", err)
			}

			return ui.PrintVersion(cmd.OutOrStdout(), info)
		},
	}

	cmd.Flags().BoolVarP(&showDeps, "dependencies", "d", true, "Include third-party module dependency tree")

	return cmd
}
