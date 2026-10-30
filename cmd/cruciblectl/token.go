// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"
	"gotcode.org/crucible/internal/tokens"
	"gotcode.org/crucible/internal/ui"
)

func tokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "token",
		Aliases: []string{"tokens", "pat"},
		Short:   "Manage Personal Access Tokens (PAT) for automation",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(tokenCreateCmd())
	cmd.AddCommand(tokenListCmd())
	cmd.AddCommand(tokenRevokeCmd())

	return cmd
}

func tokenCreateCmd() *cobra.Command {
	var (
		name       string
		role       string
		expiresIn  string
		user       string
		scopes     []string
		socketPath string
		serverURL  string
		token      string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Generate a new Personal Access Token",
		RunE: func(cmd *cobra.Command, _ []string) error {
			name = strings.TrimSpace(name)
			if name == "" {
				return errors.New(
					"token name is required (--name)",
				)
			}

			if len(scopes) == 0 {
				return errors.New(
					"token scopes are required (--scopes); " +
						"use '*' for all permissions permitted by role",
				)
			}

			transport, err := ResolveClientTransport(
				cmd.Context(),
				ClientOptions{
					SocketPath: socketPath,
					ServerURL:  serverURL,
					Token:      token,
				},
			)
			if err != nil {
				return err
			}

			if !transport.IsUnixSock && transport.Token == "" {
				return errors.New(
					"authentication token is required " +
						"for remote access (--token)",
				)
			}

			payload, mErr := json.Marshal(map[string]any{
				"name":       name,
				"role":       role,
				"expires_in": expiresIn,
				"username":   user,
				"scopes":     scopes,
			})
			if mErr != nil {
				return fmt.Errorf("encoding payload: %w", mErr)
			}

			resp, err := transport.Request(
				cmd.Context(),
				http.MethodPost,
				"/api/v1/tokens",
				bytes.NewReader(payload),
			)
			if err != nil {
				return fmt.Errorf(
					"connecting to server: %w",
					err,
				)
			}
			defer func() {
				if cErr := resp.Body.Close(); cErr != nil {
					_ = cErr
				}
			}()

			if resp.StatusCode != http.StatusCreated {
				body := readResponseBody(resp.Body)
				return fmt.Errorf(
					"server error (%d): %s",
					resp.StatusCode,
					body,
				)
			}

			var res tokens.CreateTokenResult
			if dErr := json.NewDecoder(
				resp.Body,
			).Decode(&res); dErr != nil {
				return fmt.Errorf(
					"decoding token response: %w",
					dErr,
				)
			}

			return printCreateTokenResult(cmd.OutOrStdout(), &res)
		},
	}

	cmd.Flags().StringVarP(
		&name,
		"name",
		"n",
		"",
		"Human-readable token name (required)",
	)
	cmd.Flags().StringVarP(
		&role,
		"role",
		"r",
		"operator",
		"Token access role: admin, operator, viewer",
	)
	cmd.Flags().StringVarP(
		&expiresIn,
		"expires-in",
		"e",
		"720h",
		"Token lifetime duration (e.g. 720h, 30d, 90d, 1y, or 0)",
	)
	cmd.Flags().StringVarP(
		&user,
		"user",
		"u",
		"",
		"Target operator username (admin only; defaults to self)",
	)
	cmd.Flags().StringSliceVar(
		&scopes,
		"scopes",
		nil,
		"Token permission scopes (required; comma-separated or "+
			"repeated; e.g. tokens:read,system:read or *)",
	)
	cmd.Flags().StringVar(
		&socketPath,
		"socket",
		"",
		"Explicit path to UNIX domain socket",
	)
	cmd.Flags().StringVarP(
		&serverURL,
		"server",
		"s",
		"http://127.0.0.1:8080",
		"Target Crucible server URL",
	)
	cmd.Flags().StringVarP(
		&token,
		"token",
		"t",
		"",
		"Bearer token for remote TCP authentication",
	)

	return cmd
}

func tokenListCmd() *cobra.Command {
	var (
		all        bool
		user       string
		socketPath string
		serverURL  string
		token      string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List Personal Access Tokens",
		RunE: func(cmd *cobra.Command, _ []string) error {
			transport, err := ResolveClientTransport(
				cmd.Context(),
				ClientOptions{
					SocketPath: socketPath,
					ServerURL:  serverURL,
					Token:      token,
				},
			)
			if err != nil {
				return err
			}

			if !transport.IsUnixSock && transport.Token == "" {
				return errors.New(
					"authentication token is required " +
						"for remote access (--token)",
				)
			}

			endpoint := "/api/v1/tokens"
			if all {
				endpoint += "?all=true"
			} else if user != "" {
				endpoint += "?username=" + user
			}

			resp, err := transport.Request(
				cmd.Context(),
				http.MethodGet,
				endpoint,
				nil,
			)
			if err != nil {
				return fmt.Errorf(
					"connecting to server: %w",
					err,
				)
			}
			defer func() {
				if cErr := resp.Body.Close(); cErr != nil {
					_ = cErr
				}
			}()

			if resp.StatusCode != http.StatusOK {
				body := readResponseBody(resp.Body)
				return fmt.Errorf(
					"server error (%d): %s",
					resp.StatusCode,
					body,
				)
			}

			var toks []tokens.APIToken
			if dErr := json.NewDecoder(
				resp.Body,
			).Decode(&toks); dErr != nil {
				return fmt.Errorf("decoding tokens: %w", dErr)
			}

			return printTokenList(cmd.OutOrStdout(), toks)
		},
	}

	cmd.Flags().BoolVarP(
		&all,
		"all",
		"a",
		false,
		"List all tokens across operators (admin only)",
	)
	cmd.Flags().StringVarP(
		&user,
		"user",
		"u",
		"",
		"Filter tokens by operator username (admin only)",
	)
	cmd.Flags().StringVar(
		&socketPath,
		"socket",
		"",
		"Explicit path to UNIX domain socket",
	)
	cmd.Flags().StringVarP(
		&serverURL,
		"server",
		"s",
		"http://127.0.0.1:8080",
		"Target Crucible server URL",
	)
	cmd.Flags().StringVarP(
		&token,
		"token",
		"t",
		"",
		"Bearer token for remote TCP authentication",
	)

	return cmd
}

func tokenRevokeCmd() *cobra.Command {
	var (
		socketPath string
		serverURL  string
		token      string
	)

	cmd := &cobra.Command{
		Use:   "revoke <token-id>",
		Short: "Revoke an active Personal Access Token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tokenID := strings.TrimSpace(args[0])
			if tokenID == "" {
				return errors.New("token ID is required")
			}

			transport, err := ResolveClientTransport(
				cmd.Context(),
				ClientOptions{
					SocketPath: socketPath,
					ServerURL:  serverURL,
					Token:      token,
				},
			)
			if err != nil {
				return err
			}

			if !transport.IsUnixSock && transport.Token == "" {
				return errors.New(
					"authentication token is required " +
						"for remote access (--token)",
				)
			}

			endpoint := "/api/v1/tokens?id=" + tokenID
			resp, err := transport.Request(
				cmd.Context(),
				http.MethodDelete,
				endpoint,
				nil,
			)
			if err != nil {
				return fmt.Errorf(
					"connecting to server: %w",
					err,
				)
			}
			defer func() {
				if cErr := resp.Body.Close(); cErr != nil {
					_ = cErr
				}
			}()

			if resp.StatusCode == http.StatusNotFound {
				return fmt.Errorf(
					"token %s not found",
					tokenID,
				)
			}
			if resp.StatusCode != http.StatusNoContent {
				body := readResponseBody(resp.Body)
				return fmt.Errorf(
					"server error (%d): %s",
					resp.StatusCode,
					body,
				)
			}

			msg := ui.SuccessMsg(
				"Token %s revoked successfully.",
				tokenID,
			)
			_, pErr := fmt.Fprintln(cmd.OutOrStdout(), msg)
			return pErr
		},
	}

	cmd.Flags().StringVar(
		&socketPath,
		"socket",
		"",
		"Explicit path to UNIX domain socket",
	)
	cmd.Flags().StringVarP(
		&serverURL,
		"server",
		"s",
		"http://127.0.0.1:8080",
		"Target Crucible server URL",
	)
	cmd.Flags().StringVarP(
		&token,
		"token",
		"t",
		"",
		"Bearer token for remote TCP authentication",
	)

	return cmd
}

func readResponseBody(r io.Reader) string {
	b, err := io.ReadAll(r)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func printCreateTokenResult(
	out io.Writer,
	res *tokens.CreateTokenResult,
) error {
	if _, err := fmt.Fprintln(
		out,
		ui.SuccessMsg("Personal Access Token created successfully!"),
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		out,
		"  ID:          %s\n",
		res.Token.ID,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		out,
		"  Name:        %s\n",
		res.Token.Name,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		out,
		"  Role:        %s\n",
		res.Token.Role,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		out,
		"  Owner:       %s\n",
		res.Token.Username,
	); err != nil {
		return err
	}
	scopesStr := strings.Join(res.Token.Scopes, ", ")
	if scopesStr == "" {
		scopesStr = "*"
	}
	if _, err := fmt.Fprintf(
		out,
		"  Scopes:      %s\n",
		scopesStr,
	); err != nil {
		return err
	}
	if res.Token.ExpiresAt != nil {
		expStr := res.Token.ExpiresAt.UTC().Format(
			"2006-01-02 15:04:05 UTC",
		)
		if _, err := fmt.Fprintf(
			out,
			"  Expires:     %s\n",
			expStr,
		); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintln(
			out,
			"  Expires:     Never",
		); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		out,
		"  Raw Token:   %s\n",
		res.RawToken,
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	warn := ui.WarningMsg(
		"IMPORTANT: Copy token now. It will not be shown again!",
	)
	_, err := fmt.Fprintln(out, warn)
	return err
}

func printTokenList(out io.Writer, toks []tokens.APIToken) error {
	if len(toks) == 0 {
		_, err := fmt.Fprintln(out, "No personal access tokens found.")
		return err
	}

	if _, err := fmt.Fprintf(
		out,
		"%-26s %-16s %-12s %-10s %-12s %-20s %-20s %-8s\n",
		"TOKEN ID",
		"NAME",
		"PREFIX",
		"ROLE",
		"OWNER",
		"EXPIRES",
		"LAST USED",
		"STATUS",
	); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out, strings.Repeat("-", 120)); err != nil {
		return err
	}

	for _, t := range toks {
		expStr := "never"
		if t.ExpiresAt != nil && !t.ExpiresAt.IsZero() {
			expStr = t.ExpiresAt.UTC().Format(
				"2006-01-02 15:04:05",
			)
		}
		lastUsedStr := "never"
		if t.LastUsedAt != nil && !t.LastUsedAt.IsZero() {
			lastUsedStr = t.LastUsedAt.UTC().Format(
				"2006-01-02 15:04:05",
			)
		}
		status := t.Status()

		if _, err := fmt.Fprintf(
			out,
			"%-26s %-16s %-12s %-10s %-12s %-20s %-20s %-8s\n",
			t.ID,
			t.Name,
			t.TokenPrefix+"...",
			t.Role,
			t.Username,
			expStr,
			lastUsedStr,
			status,
		); err != nil {
			return err
		}
	}
	return nil
}
