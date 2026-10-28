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

func defaultClientConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "cruciblectl.yaml"
	}
	return filepath.Join(home, ".config", "crucible", "cruciblectl.yaml")
}

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage client connection profiles and credentials",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(configInitCmd())
	cmd.AddCommand(configValidateCmd())
	cmd.AddCommand(configUseContextCmd())

	return cmd
}

func configInitCmd() *cobra.Command {
	var (
		serverURL string
		token     string
		output    string
		overwrite bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize client configuration file and default connection profile",
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc := config.DefaultCommandService()
			path, err := svc.InitClientConfig(cmd.Context(), config.InitClientConfigCommand{
				OutputPath: output,
				ServerURL:  serverURL,
				Token:      token,
				Overwrite:  overwrite,
			})
			if err != nil {
				return fmt.Errorf("initializing client configuration: %w", err)
			}

			_, printErr := fmt.Fprintln(cmd.OutOrStdout(), ui.SuccessMsg("Client configuration written to %s (permissions 0600)", path))
			return printErr
		},
	}

	cmd.Flags().StringVarP(&serverURL, "server", "s", "https://127.0.0.1:8443", "Target Crucible server URL")
	cmd.Flags().StringVarP(&token, "token", "t", "", "Personal Access Token (PAT) for API authentication")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Destination file path (default: ~/.config/crucible/cruciblectl.yaml)")
	cmd.Flags().BoolVarP(&overwrite, "force", "f", false, "Overwrite existing configuration file if present")

	return cmd
}

func configValidateCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate syntax and domain rules of a client configuration file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			svc := config.DefaultQueryService()
			targetPath := configPath
			if targetPath == "" {
				targetPath = defaultClientConfigPath()
			}

			err := svc.ValidateClientConfig(cmd.Context(), config.ValidateConfigQuery{
				ConfigFile: targetPath,
			})
			if err != nil {
				return fmt.Errorf("configuration %s is invalid: %w", targetPath, err)
			}

			_, printErr := fmt.Fprintln(cmd.OutOrStdout(), ui.SuccessMsg("Client configuration %s is valid", targetPath))
			return printErr
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to client configuration file to validate")

	return cmd
}

func configUseContextCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "use-context <profile>",
		Short: "Switch active connection profile context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profileName := args[0]
			targetPath := configPath
			if targetPath == "" {
				targetPath = defaultClientConfigPath()
			}

			querySvc := config.DefaultQueryService()
			cfg, err := querySvc.GetClientConfig(cmd.Context(), config.GetClientConfigQuery{
				ConfigFile: targetPath,
			})
			if err != nil {
				return fmt.Errorf("loading client configuration from %s: %w", targetPath, err)
			}

			profile, exists := cfg.Profiles[profileName]
			if !exists {
				return fmt.Errorf("profile %q does not exist in %s", profileName, targetPath)
			}

			cmdSvc := config.DefaultCommandService()
			err = cmdSvc.SetClientProfile(cmd.Context(), config.SetClientProfileCommand{
				ConfigFile:  targetPath,
				ProfileName: profileName,
				Profile:     profile,
			})
			if err != nil {
				return fmt.Errorf("switching context to %q: %w", profileName, err)
			}

			_, printErr := fmt.Fprintln(cmd.OutOrStdout(), ui.SuccessMsg("Switched active profile context to %q", profileName))
			return printErr
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to client configuration file")

	return cmd
}
