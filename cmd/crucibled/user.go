// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"gotcode.org/crucible/internal/auth"
	"gotcode.org/crucible/internal/ui"
)

func userCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage native SQLite user credentials and RBAC roles",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(userAddCmd())
	cmd.AddCommand(userListCmd())
	cmd.AddCommand(userPasswdCmd())
	cmd.AddCommand(userDeleteCmd())

	return cmd
}

func userAddCmd() *cobra.Command {
	var (
		configPath string
		dbPath     string
		username   string
		roleStr    string
		password   string
		mustChange bool
	)

	cmd := &cobra.Command{
		Use:   "add [username]",
		Short: "Create a new user with Argon2id password hash in SQLite",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && username == "" {
				username = args[0]
			}
			username = strings.TrimSpace(username)
			if username == "" {
				return errors.New("username is required (--username or arg)")
			}

			role := auth.Role(strings.ToLower(strings.TrimSpace(roleStr)))
			if !role.IsValid() {
				return fmt.Errorf(
					"invalid role %q: must be admin, operator, or viewer",
					roleStr,
				)
			}

			pass, pErr := readNewPasswordWithConfirm(cmd, password)
			if pErr != nil {
				return pErr
			}

			hash, hErr := auth.HashPassword(pass)
			if hErr != nil {
				return fmt.Errorf("hashing password: %w", hErr)
			}

			ctx := cmd.Context()
			engine, err := openEngine(ctx, configPath, dbPath)
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := engine.Close(); closeErr != nil {
					_ = closeErr
				}
			}()

			store := auth.NewSQLiteStore(engine.DB())
			now := time.Now().UTC()
			u := &auth.User{
				Username:           username,
				PasswordHash:       &hash,
				Role:               role,
				IsActive:           true,
				MustChangePassword: mustChange,
				PasswordChangedAt:  now,
				CreatedAt:          now,
			}

			if err := store.CreateUser(ctx, u); err != nil {
				return err
			}

			msg := ui.SuccessMsg(
				"User %q created successfully with role %s",
				username,
				role,
			)
			_, printErr := fmt.Fprintln(cmd.OutOrStdout(), msg)
			return printErr
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Config path")
	cmd.Flags().StringVarP(&dbPath, "database-path", "d", "", "DB path")
	cmd.Flags().StringVarP(&username, "username", "u", "", "Username")
	cmd.Flags().StringVarP(
		&roleStr,
		"role",
		"r",
		string(auth.RoleOperator),
		"Role (admin, operator, viewer)",
	)
	cmd.Flags().StringVarP(
		&password,
		"password",
		"p",
		"",
		"Password (prompted if omitted)",
	)
	cmd.Flags().BoolVar(
		&mustChange,
		"must-change-password",
		true,
		"Require password change on first login (default true)",
	)

	return cmd
}

func userListCmd() *cobra.Command {
	var (
		configPath string
		dbPath     string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered native SQLite users",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			engine, err := openEngine(ctx, configPath, dbPath)
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := engine.Close(); closeErr != nil {
					_ = closeErr
				}
			}()

			store := auth.NewSQLiteStore(engine.DB())
			users, err := store.ListUsers(ctx)
			if err != nil {
				return fmt.Errorf("listing users: %w", err)
			}

			return printUsersTable(cmd.OutOrStdout(), users)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Config path")
	cmd.Flags().StringVarP(&dbPath, "database-path", "d", "", "DB path")

	return cmd
}

func userPasswdCmd() *cobra.Command {
	var (
		configPath string
		dbPath     string
		password   string
		mustChange bool
	)

	cmd := &cobra.Command{
		Use:   "passwd <username>",
		Short: "Update password for an existing native SQLite user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := strings.TrimSpace(args[0])

			pass, pErr := readNewPasswordWithConfirm(cmd, password)
			if pErr != nil {
				return pErr
			}

			hash, hErr := auth.HashPassword(pass)
			if hErr != nil {
				return fmt.Errorf("hashing password: %w", hErr)
			}

			ctx := cmd.Context()
			engine, err := openEngine(ctx, configPath, dbPath)
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := engine.Close(); closeErr != nil {
					_ = closeErr
				}
			}()

			store := auth.NewSQLiteStore(engine.DB())
			if err := store.UpdatePassword(ctx, username, hash); err != nil {
				return err
			}

			if mustChange {
				if sErr := store.SetMustChangePassword(ctx, username, true); sErr != nil {
					return sErr
				}
			}

			msg := ui.SuccessMsg(
				"Password updated successfully for user %q",
				username,
			)
			_, printErr := fmt.Fprintln(cmd.OutOrStdout(), msg)
			return printErr
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Config path")
	cmd.Flags().StringVarP(&dbPath, "database-path", "d", "", "DB path")
	cmd.Flags().StringVarP(
		&password,
		"password",
		"p",
		"",
		"New password (prompted if omitted)",
	)
	cmd.Flags().BoolVar(
		&mustChange,
		"must-change-password",
		false,
		"Require user to change password upon next login",
	)

	return cmd
}

func userDeleteCmd() *cobra.Command {
	var (
		configPath string
		dbPath     string
		force      bool
	)

	cmd := &cobra.Command{
		Use:   "delete <username>",
		Short: "Delete a user account and associated credentials in SQLite",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := strings.TrimSpace(args[0])

			if !force {
				confirmed, cErr := promptConfirmation(
					cmd,
					fmt.Sprintf("Delete user %q? (y/N): ", username),
				)
				if cErr != nil {
					return cErr
				}
				if !confirmed {
					if _, pErr := fmt.Fprintln(
						cmd.OutOrStdout(),
						"Aborted.",
					); pErr != nil {
						return pErr
					}
					return nil
				}
			}

			ctx := cmd.Context()
			engine, err := openEngine(ctx, configPath, dbPath)
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := engine.Close(); closeErr != nil {
					_ = closeErr
				}
			}()

			store := auth.NewSQLiteStore(engine.DB())
			if err := store.DeleteUser(ctx, username); err != nil {
				return err
			}

			msg := ui.SuccessMsg("User %q deleted successfully", username)
			_, printErr := fmt.Fprintln(cmd.OutOrStdout(), msg)
			return printErr
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Config path")
	cmd.Flags().StringVarP(&dbPath, "database-path", "d", "", "DB path")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")

	return cmd
}

func promptSecurePassword(
	cmd *cobra.Command,
	promptText string,
) (string, error) {
	if _, err := fmt.Fprint(cmd.OutOrStdout(), promptText); err != nil {
		return "", err
	}
	in := cmd.InOrStdin()
	if f, ok := in.(*os.File); ok && term.IsTerminal(f.Fd()) {
		b, err := term.ReadPassword(f.Fd())
		if _, pErr := fmt.Fprintln(cmd.OutOrStdout()); pErr != nil {
			return "", pErr
		}
		if err == nil {
			return string(b), nil
		}
	}
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading password: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func readNewPasswordWithConfirm(
	cmd *cobra.Command,
	explicitPassword string,
) (string, error) {
	if explicitPassword != "" {
		return explicitPassword, nil
	}
	p1, err := promptSecurePassword(cmd, "Password: ")
	if err != nil {
		return "", err
	}
	if len(p1) == 0 {
		return "", errors.New("password cannot be empty")
	}
	p2, err := promptSecurePassword(cmd, "Confirm Password: ")
	if err != nil {
		return "", err
	}
	if p1 != p2 {
		return "", errors.New("passwords do not match")
	}
	return p1, nil
}

func promptConfirmation(
	cmd *cobra.Command,
	promptText string,
) (bool, error) {
	if _, err := fmt.Fprint(cmd.OutOrStdout(), promptText); err != nil {
		return false, err
	}
	reader := bufio.NewReader(cmd.InOrStdin())
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("reading confirmation: %w", err)
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "y" || answer == "yes", nil
}

func printUsersTable(w io.Writer, users []auth.User) error {
	if len(users) == 0 {
		_, err := fmt.Fprintln(w, "No users found in SQLite database.")
		return err
	}

	uWidth := len("USERNAME")
	rWidth := len("ROLE")
	aWidth := len("ACTIVE")
	cWidth := 19
	lWidth := 19

	for _, u := range users {
		if len(u.Username) > uWidth {
			uWidth = len(u.Username)
		}
		if len(string(u.EffectiveRole())) > rWidth {
			rWidth = len(string(u.EffectiveRole()))
		}
	}

	hCol1 := ui.BoldStyle.Render("USERNAME")
	hCol2 := ui.BoldStyle.Render("ROLE")
	hCol3 := ui.BoldStyle.Render("ACTIVE")
	hCol4 := ui.BoldStyle.Render("CREATED")
	hCol5 := ui.BoldStyle.Render("LAST LOGIN")

	pad1 := strings.Repeat(" ", uWidth-lipgloss.Width(hCol1))
	pad2 := strings.Repeat(" ", rWidth-lipgloss.Width(hCol2))
	pad3 := strings.Repeat(" ", aWidth-lipgloss.Width(hCol3))
	pad4 := strings.Repeat(" ", cWidth-lipgloss.Width(hCol4))

	if _, err := fmt.Fprintf(
		w,
		"%s%s  %s%s  %s%s  %s%s  %s\n",
		hCol1, pad1,
		hCol2, pad2,
		hCol3, pad3,
		hCol4, pad4,
		hCol5,
	); err != nil {
		return err
	}

	divStyle := lipgloss.NewStyle().Foreground(ui.Subtle)
	div1 := divStyle.Render(strings.Repeat("─", uWidth))
	div2 := divStyle.Render(strings.Repeat("─", rWidth))
	div3 := divStyle.Render(strings.Repeat("─", aWidth))
	div4 := divStyle.Render(strings.Repeat("─", cWidth))
	div5 := divStyle.Render(strings.Repeat("─", lWidth))

	if _, err := fmt.Fprintf(
		w,
		"%s  %s  %s  %s  %s\n",
		div1, div2, div3, div4, div5,
	); err != nil {
		return err
	}

	for _, u := range users {
		activeStr := "yes"
		if !u.IsActive {
			activeStr = "no"
		}
		lastLoginStr := "never"
		if u.LastLogin != nil {
			lastLoginStr = u.LastLogin.UTC().Format("2006-01-02 15:04:05")
		}
		createdStr := u.CreatedAt.UTC().Format("2006-01-02 15:04:05")
		roleStr := string(u.EffectiveRole())

		p1 := strings.Repeat(" ", uWidth-len(u.Username))
		p2 := strings.Repeat(" ", rWidth-len(roleStr))
		p3 := strings.Repeat(" ", aWidth-len(activeStr))
		p4 := strings.Repeat(" ", cWidth-len(createdStr))

		if _, err := fmt.Fprintf(
			w,
			"%s%s  %s%s  %s%s  %s%s  %s\n",
			u.Username, p1,
			roleStr, p2,
			activeStr, p3,
			createdStr, p4,
			lastLoginStr,
		); err != nil {
			return err
		}
	}

	return nil
}
