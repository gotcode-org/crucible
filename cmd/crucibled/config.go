// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"gotcode.org/crucible/internal/config"
	"gotcode.org/crucible/internal/ui"
)

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage and inspect server daemon configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(configInitCmd())
	cmd.AddCommand(configValidateCmd())

	return cmd
}

func configInitCmd() *cobra.Command {
	var (
		prod      bool
		output    string
		overwrite bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Generate a fully commented server YAML configuration template",
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc := config.DefaultCommandService()
			path, err := svc.InitServerConfig(cmd.Context(), config.InitServerConfigCommand{
				OutputPath:   output,
				IsProduction: prod,
				Overwrite:    overwrite,
			})
			if err != nil {
				return fmt.Errorf("initializing server configuration: %w", err)
			}

			_, printErr := fmt.Fprintln(cmd.OutOrStdout(), ui.SuccessMsg("Server configuration written to %s", path))
			return printErr
		},
	}

	cmd.Flags().BoolVarP(&prod, "prod", "p", false, "Generate production paths (/var/lib/crucible, /etc/crucible)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Destination file path (default: crucible.yaml or /etc/crucible/crucible.yaml)")
	cmd.Flags().BoolVarP(&overwrite, "force", "f", false, "Overwrite existing configuration file if present")

	return cmd
}

func configValidateCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate syntax and domain rules of a server configuration file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc := config.DefaultQueryService()
			targetPath := configPath
			if targetPath == "" {
				targetPath = "crucible.yaml"
			}

			err := svc.ValidateServerConfig(cmd.Context(), config.ValidateConfigQuery{
				ConfigFile: targetPath,
			})
			if err != nil {
				return fmt.Errorf("configuration %s is invalid: %w", targetPath, err)
			}

			_, printErr := fmt.Fprintln(cmd.OutOrStdout(), ui.SuccessMsg("Server configuration %s is valid", targetPath))
			return printErr
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "crucible.yaml", "Path to server configuration file to validate")

	return cmd
}
