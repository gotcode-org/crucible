// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gotcode.org/crucible/internal/config"
	"gotcode.org/crucible/internal/ui"
)

func systemdCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "systemd",
		Short: "Manage and generate systemd integration artifacts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(systemdUnitCmd())

	return cmd
}

func systemdUnitCmd() *cobra.Command {
	var (
		user       string
		group      string
		execPath   string
		configPath string
		port       int
		outputPath string
	)

	cmd := &cobra.Command{
		Use:   "unit",
		Short: "Generate a hardened systemd unit file with CAP_NET_BIND_SERVICE",
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc := config.DefaultQueryService()
			opts := config.SystemdOptions{
				User:                user,
				Group:               group,
				ExecPath:            execPath,
				ConfigPath:          configPath,
				Port:                port,
				AmbientCapabilities: true,
			}

			unit, err := svc.GenerateSystemdUnit(cmd.Context(), config.GenerateSystemdUnitQuery{
				Options: opts,
			})
			if err != nil {
				return fmt.Errorf("generating systemd unit: %w", err)
			}

			if outputPath != "" {
				cleanPath := filepath.Clean(outputPath)
				// #nosec G306 -- Systemd unit files require standard 0644 readable
				// permissions
				if err := os.WriteFile(cleanPath, []byte(unit), 0644); err != nil {
					return fmt.Errorf("writing systemd unit to %s: %w", cleanPath, err)
				}
				_, printErr := fmt.Fprintln(cmd.OutOrStdout(), ui.SuccessMsg("Systemd unit written to %s", cleanPath))
				return printErr
			}

			_, printErr := fmt.Fprint(cmd.OutOrStdout(), unit)
			return printErr
		},
	}

	cmd.Flags().StringVarP(&user, "user", "u", config.DefaultSystemdUser, "Unprivileged service user")
	cmd.Flags().StringVarP(&group, "group", "g", config.DefaultSystemdGroup, "Unprivileged service group")
	cmd.Flags().StringVarP(&execPath, "exec", "e", config.DefaultSystemdExec, "Absolute path to crucibled binary")
	cmd.Flags().StringVarP(&configPath, "config", "c", config.DefaultSystemdConfig, "Absolute path to production configuration")
	cmd.Flags().IntVarP(&port, "port", "p", config.DefaultProdHTTPPort, "Production HTTP/HTTPS port")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Destination file path (default: stdout)")

	return cmd
}
