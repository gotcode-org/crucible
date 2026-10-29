// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"gotcode.org/crucible/internal/auth"
	"gotcode.org/crucible/internal/ui"
)

func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage operator authentication, sessions, and multi-device 2FA",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(authLoginCmd())
	cmd.AddCommand(auth2FACmd())

	return cmd
}

func authLoginCmd() *cobra.Command {
	var (
		serverURL string
		username  string
		passcode  string
	)

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate operator credentials and establish an active session",
		RunE: func(cmd *cobra.Command, _ []string) error {
			reader := bufio.NewReader(cmd.InOrStdin())

			user, uErr := promptUsername(cmd, reader, username)
			if uErr != nil {
				return uErr
			}

			pass, pErr := promptPassword(cmd, reader)
			if pErr != nil {
				return pErr
			}

			client := &http.Client{Timeout: 10 * time.Second}
			serverURL = strings.TrimRight(serverURL, "/")

			payload, mErr := json.Marshal(map[string]string{
				"username": user,
				"password": pass,
			})
			if mErr != nil {
				return fmt.Errorf("encoding login payload: %w", mErr)
			}

			req, err := http.NewRequestWithContext(
				cmd.Context(),
				http.MethodPost,
				serverURL+"/api/v1/auth/login",
				bytes.NewReader(payload),
			)
			if err != nil {
				return fmt.Errorf("creating login request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf(
					"connecting to server %s: %w",
					serverURL,
					err,
				)
			}
			defer func() {
				if cErr := resp.Body.Close(); cErr != nil {
					return
				}
			}()

			if sErr := checkLoginHTTPStatus(resp); sErr != nil {
				return sErr
			}

			var loginRes struct {
				Requires2FA    bool          `json:"requires_2fa"`
				ChallengeToken string        `json:"challenge_token"`
				Session        *auth.Session `json:"session"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&loginRes); err != nil {
				return fmt.Errorf("decoding login response: %w", err)
			}

			if loginRes.Requires2FA {
				sess, sErr := performCLI2FAVerification(
					cmd.Context(),
					cmd,
					reader,
					client,
					serverURL,
					loginRes.ChallengeToken,
					passcode,
				)
				if sErr != nil {
					return sErr
				}
				loginRes.Session = sess
			}

			return printLoginResult(cmd.OutOrStdout(), loginRes.Session)
		},
	}

	cmd.Flags().StringVarP(
		&serverURL,
		"server",
		"s",
		"http://127.0.0.1:8080",
		"Target Crucible server URL",
	)
	cmd.Flags().StringVarP(
		&username,
		"username",
		"u",
		"",
		"Operator username",
	)
	cmd.Flags().StringVar(
		&passcode,
		"passcode",
		"",
		"6-digit TOTP passcode (if 2FA is required)",
	)

	return cmd
}

func auth2FACmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "2fa",
		Short: "Manage multi-device two-factor authentication tokens and invites",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(auth2FADevicesCmd())
	cmd.AddCommand(auth2FARevokeCmd())
	cmd.AddCommand(auth2FAInviteCmd())

	return cmd
}

func auth2FADevicesCmd() *cobra.Command {
	var (
		socketPath string
		serverURL  string
		token      string
	)

	cmd := &cobra.Command{
		Use:   "devices",
		Short: "List enrolled 2FA devices for the active session",
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
					"session token is required (--token)",
				)
			}

			resp, err := transport.Request(
				cmd.Context(),
				http.MethodGet,
				"/api/v1/auth/devices",
				nil,
			)
			if err != nil {
				return fmt.Errorf("connecting to server: %w", err)
			}
			defer func() {
				if cErr := resp.Body.Close(); cErr != nil {
					return
				}
			}()

			if resp.StatusCode == http.StatusUnauthorized {
				return errors.New("unauthorized: session token invalid or expired")
			}
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("server returned status: %d", resp.StatusCode)
			}

			var devices []auth.TOTPDevice
			if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
				return fmt.Errorf("decoding devices: %w", err)
			}

			out := cmd.OutOrStdout()
			if len(devices) == 0 {
				_, pErr := fmt.Fprintln(out, "No 2FA devices enrolled.")
				return pErr
			}

			if _, pErr := fmt.Fprintf(
				out,
				"%-30s %-20s %-20s\n",
				"DEVICE ID",
				"NAME",
				"CREATED",
			); pErr != nil {
				return pErr
			}
			if _, pErr := fmt.Fprintln(out, strings.Repeat("-", 72)); pErr != nil {
				return pErr
			}
			for _, d := range devices {
				if _, pErr := fmt.Fprintf(
					out,
					"%-30s %-20s %-20s\n",
					d.ID,
					d.Name,
					d.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
				); pErr != nil {
					return pErr
				}
			}
			return nil
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
		"Active operator session token",
	)

	return cmd
}

func auth2FARevokeCmd() *cobra.Command {
	var (
		socketPath string
		serverURL  string
		token      string
	)

	cmd := &cobra.Command{
		Use:   "revoke <device-id>",
		Short: "Revoke an enrolled two-factor authentication device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deviceID := args[0]
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
					"session token is required (--token)",
				)
			}

			resp, err := transport.Request(
				cmd.Context(),
				http.MethodDelete,
				"/api/v1/auth/devices?id="+deviceID,
				nil,
			)
			if err != nil {
				return fmt.Errorf("connecting to server: %w", err)
			}
			defer func() {
				if cErr := resp.Body.Close(); cErr != nil {
					return
				}
			}()

			if resp.StatusCode == http.StatusUnauthorized {
				return errors.New("unauthorized: session token invalid or expired")
			}
			if resp.StatusCode != http.StatusNoContent &&
				resp.StatusCode != http.StatusOK {
				return fmt.Errorf(
					"device revocation failed (status %d)",
					resp.StatusCode,
				)
			}

			msg := ui.SuccessMsg("Device %s revoked successfully", deviceID)
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
		"Active operator session token",
	)

	return cmd
}

func auth2FAInviteCmd() *cobra.Command {
	var (
		socketPath string
		serverURL  string
		token      string
		username   string
		label      string
	)

	cmd := &cobra.Command{
		Use:   "invite",
		Short: "Generate a single-use 24-hour onboarding invite URL and QR code",
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
					"session token is required (--token)",
				)
			}

			payload, mErr := json.Marshal(map[string]string{
				"username": username,
				"label":    label,
			})
			if mErr != nil {
				return fmt.Errorf("encoding invite payload: %w", mErr)
			}

			resp, err := transport.Request(
				cmd.Context(),
				http.MethodPost,
				"/api/v1/auth/invite",
				bytes.NewReader(payload),
			)
			if err != nil {
				return fmt.Errorf("connecting to server: %w", err)
			}
			defer func() {
				if cErr := resp.Body.Close(); cErr != nil {
					return
				}
			}()

			if resp.StatusCode == http.StatusUnauthorized {
				return errors.New("unauthorized: session token invalid or expired")
			}
			if resp.StatusCode != http.StatusOK {
				body, rErr := io.ReadAll(resp.Body)
				if rErr != nil {
					return fmt.Errorf("reading response: %w", rErr)
				}
				return fmt.Errorf(
					"invite failed (%d): %s",
					resp.StatusCode,
					string(body),
				)
			}

			var inv auth.TOTPInvite
			if err := json.NewDecoder(resp.Body).Decode(&inv); err != nil {
				return fmt.Errorf("decoding invite response: %w", err)
			}

			return printInviteResult(cmd.OutOrStdout(), &inv)
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
		"Active operator session token",
	)
	cmd.Flags().StringVarP(
		&username,
		"username",
		"u",
		"root",
		"Target account username for invitation",
	)
	cmd.Flags().StringVarP(
		&label,
		"label",
		"l",
		"Workstation",
		"Device label description for new token",
	)

	return cmd
}

func performCLI2FAVerification(
	ctx context.Context,
	cmd *cobra.Command,
	reader *bufio.Reader,
	client *http.Client,
	serverURL string,
	challengeToken string,
	passcode string,
) (*auth.Session, error) {
	code, pErr := promptPasscode(cmd, reader, passcode)
	if pErr != nil {
		return nil, pErr
	}

	verifyPayload, vmErr := json.Marshal(map[string]string{
		"challenge_token": challengeToken,
		"passcode":        code,
	})
	if vmErr != nil {
		return nil, fmt.Errorf("encoding 2fa payload: %w", vmErr)
	}

	vReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		serverURL+"/api/v1/auth/verify-2fa",
		bytes.NewReader(verifyPayload),
	)
	if err != nil {
		return nil, fmt.Errorf("creating 2fa verify request: %w", err)
	}
	vReq.Header.Set("Content-Type", "application/json")

	vResp, err := client.Do(vReq)
	if err != nil {
		return nil, fmt.Errorf("sending 2fa verification: %w", err)
	}
	defer func() {
		if cErr := vResp.Body.Close(); cErr != nil {
			return
		}
	}()

	if vResp.StatusCode != http.StatusOK {
		return nil, errors.New("2fa verification failed: invalid passcode")
	}

	var sessRes struct {
		Session *auth.Session `json:"session"`
	}
	if err := json.NewDecoder(vResp.Body).Decode(&sessRes); err != nil {
		return nil, fmt.Errorf("decoding 2fa response: %w", err)
	}
	return sessRes.Session, nil
}

func promptUsername(
	cmd *cobra.Command,
	reader *bufio.Reader,
	username string,
) (string, error) {
	username = strings.TrimSpace(username)
	if username != "" {
		return username, nil
	}

	if _, err := fmt.Fprint(
		cmd.OutOrStdout(),
		"Operator Username: ",
	); err != nil {
		return "", err
	}

	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading username: %w", err)
	}

	username = strings.TrimSpace(line)
	if username == "" {
		return "", errors.New("operator username cannot be empty")
	}
	return username, nil
}

func promptPassword(
	cmd *cobra.Command,
	reader *bufio.Reader,
) (string, error) {
	if _, err := fmt.Fprint(cmd.OutOrStdout(), "Password: "); err != nil {
		return "", err
	}

	in := cmd.InOrStdin()
	if f, ok := in.(*os.File); ok && term.IsTerminal(f.Fd()) {
		passBytes, err := term.ReadPassword(f.Fd())
		if _, pErr := fmt.Fprintln(cmd.OutOrStdout()); pErr != nil {
			return "", pErr
		}
		if err == nil {
			pass := string(passBytes)
			if len(pass) == 0 {
				return "", errors.New("operator password cannot be empty")
			}
			return pass, nil
		}
	}

	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading password: %w", err)
	}
	pass := strings.TrimRight(line, "\r\n")
	if len(pass) == 0 {
		return "", errors.New("operator password cannot be empty")
	}
	return pass, nil
}

func promptPasscode(
	cmd *cobra.Command,
	reader *bufio.Reader,
	passcode string,
) (string, error) {
	passcode = strings.TrimSpace(passcode)
	if passcode != "" {
		return passcode, nil
	}

	if _, err := fmt.Fprint(
		cmd.OutOrStdout(),
		"2FA Passcode: ",
	); err != nil {
		return "", err
	}

	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading passcode: %w", err)
	}
	code := strings.TrimSpace(line)
	if code == "" {
		return "", errors.New("passcode cannot be empty")
	}
	return code, nil
}

func printLoginResult(out io.Writer, sess *auth.Session) error {
	if _, pErr := fmt.Fprintln(
		out,
		ui.SuccessMsg(
			"Authenticated as %s (%s)",
			sess.Username,
			sess.Role,
		),
	); pErr != nil {
		return pErr
	}
	if _, pErr := fmt.Fprintf(
		out,
		"Session ID: %s\n",
		sess.ID,
	); pErr != nil {
		return pErr
	}
	_, pErr := fmt.Fprintf(
		out,
		"Expires:    %s\n",
		sess.ExpiresAt.UTC().Format(time.RFC3339),
	)
	return pErr
}

func printInviteResult(out io.Writer, inv *auth.TOTPInvite) error {
	if _, pErr := fmt.Fprintln(
		out,
		ui.SuccessMsg(
			"Generated 2FA onboarding invite for user %s",
			inv.Username,
		),
	); pErr != nil {
		return pErr
	}
	if _, pErr := fmt.Fprintf(
		out,
		"Invite Token: %s\n",
		inv.Token,
	); pErr != nil {
		return pErr
	}
	if _, pErr := fmt.Fprintf(
		out,
		"Label:        %s\n",
		inv.Label,
	); pErr != nil {
		return pErr
	}
	if _, pErr := fmt.Fprintf(
		out,
		"Expires:      %s\n",
		inv.ExpiresAt.UTC().Format(time.RFC3339),
	); pErr != nil {
		return pErr
	}

	otpauthURI := auth.BuildOTPAuthURI(
		inv.Username,
		auth.DefaultIssuer,
		inv.Token,
	)
	if qrTerm, qErr := auth.GenerateQRCodeTerminal(otpauthURI); qErr == nil {
		if _, pErr := fmt.Fprintln(
			out,
			"\nScan with Authenticator App:",
		); pErr != nil {
			return pErr
		}
		if _, pErr := fmt.Fprintln(out, qrTerm); pErr != nil {
			return pErr
		}
	}

	return nil
}

func checkLoginHTTPStatus(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("authentication failed: invalid credentials")
	}
	if resp.StatusCode == http.StatusForbidden {
		return errors.New(
			"authentication failed: user is not in an authorized group",
		)
	}
	body, rErr := io.ReadAll(resp.Body)
	if rErr != nil {
		return fmt.Errorf("reading response: %w", rErr)
	}
	return fmt.Errorf(
		"server returned error (%d): %s",
		resp.StatusCode,
		string(body),
	)
}
