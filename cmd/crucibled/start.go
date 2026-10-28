// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"gotcode.org/crucible/internal/config"
	"gotcode.org/crucible/internal/server"
	"gotcode.org/crucible/internal/storage"
	"gotcode.org/crucible/internal/ui"
	"gotcode.org/crucible/internal/version"
)

func startCmd() *cobra.Command {
	var (
		configPath string
		dbPath     string
		httpPort   int
		httpBind   string
		socketPath string
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the crucibled server daemon and embedded web shell",
		Long: "The start command initializes the crucible daemon runtime, " +
			"auto-migrates SQLite schema, and exposes dual HTTP and UNIX domain " +
			"socket listeners for operator control and web shell access.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg, err := loadDaemonConfig(
				ctx,
				configPath,
				dbPath,
				httpBind,
				socketPath,
				httpPort,
			)
			if err != nil {
				return err
			}

			engine, err := storage.NewSQLiteAdapter(ctx, cfg.Storage)
			if err != nil {
				return fmt.Errorf("initializing storage engine: %w", err)
			}
			defer func() {
				if closeErr := engine.Close(); closeErr != nil {
					_ = closeErr
				}
			}()

			if mErr := engine.Migrate(ctx); mErr != nil {
				return fmt.Errorf("running initial schema migration: %w", mErr)
			}

			appVersion := resolveAppVersion(ctx)
			srv, sErr := server.NewServer(server.Config{
				HTTPBind:   cfg.Server.HTTPBind,
				HTTPPort:   cfg.Server.HTTPPort,
				SocketPath: cfg.Server.SocketPath,
				SocketMode: 0660,
				DaemonMode: cfg.Mode,
				Version:    appVersion,
			}, engine, nil)
			if sErr != nil {
				return fmt.Errorf("initializing server: %w", sErr)
			}

			return runServerLifecycle(cmd, srv, cfg, appVersion)
		},
	}

	cmd.Flags().StringVarP(
		&configPath,
		"config",
		"c",
		"",
		"Path to server configuration file",
	)
	cmd.Flags().StringVarP(
		&dbPath,
		"database-path",
		"d",
		"",
		"Override SQLite database file path",
	)
	cmd.Flags().IntVarP(
		&httpPort,
		"http-port",
		"p",
		0,
		"Override HTTP listener port",
	)
	cmd.Flags().StringVarP(
		&httpBind,
		"http-bind",
		"b",
		"",
		"Override HTTP listener bind interface",
	)
	cmd.Flags().StringVarP(
		&socketPath,
		"socket-path",
		"s",
		"",
		"Override UNIX domain socket path",
	)

	return cmd
}

func loadDaemonConfig(
	ctx context.Context,
	configPath, dbPath, httpBind, socketPath string,
	httpPort int,
) (*config.ServerConfig, error) {
	svc := config.DefaultQueryService()
	var flagOverrides config.ServerFlagOverrides
	if dbPath != "" {
		flagOverrides.DatabasePath = &dbPath
	}
	if httpPort != 0 {
		flagOverrides.HTTPPort = &httpPort
	}
	if httpBind != "" {
		flagOverrides.HTTPBind = &httpBind
	}
	if socketPath != "" {
		flagOverrides.SocketPath = &socketPath
	}

	cfg, err := svc.GetServerConfig(ctx, config.GetServerConfigQuery{
		ConfigFile:     configPath,
		Flags:          flagOverrides,
		SkipValidation: true,
	})
	if err != nil {
		return nil, fmt.Errorf("loading server configuration: %w", err)
	}
	return cfg, nil
}

func resolveAppVersion(ctx context.Context) string {
	verService := version.DefaultService()
	vInfo, err := verService.GetVersion(
		ctx,
		version.GetVersionQuery{AppName: "crucibled"},
	)
	if err == nil && vInfo.Build.Version != "" {
		return vInfo.Build.Version
	}
	return "v0.1.0"
}

func runServerLifecycle(
	cmd *cobra.Command,
	srv server.Server,
	cfg *config.ServerConfig,
	appVersion string,
) error {
	sigCtx, stop := signal.NotifyContext(
		cmd.Context(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()

	if err := srv.Start(sigCtx); err != nil {
		return fmt.Errorf("starting server: %w", err)
	}

	w := cmd.OutOrStdout()
	if err := printServerBanner(w, srv, cfg, appVersion); err != nil {
		return err
	}

	<-sigCtx.Done()

	stop()
	if _, err := fmt.Fprintln(
		w,
		"\nReceived termination signal, draining connections...",
	); err != nil {
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil &&
		!errors.Is(err, context.Canceled) {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	msg := ui.SuccessMsg("Crucible server daemon stopped cleanly")
	_, pErr := fmt.Fprintln(w, msg)
	return pErr
}

func printServerBanner(
	w io.Writer,
	srv server.Server,
	cfg *config.ServerConfig,
	version string,
) error {
	header := ui.BoldStyle.Render("CRUCIBLE SERVER DAEMON")
	online := ui.BadgeActive("ONLINE")
	if _, err := fmt.Fprintf(w, "%s %s\n\n", header, online); err != nil {
		return err
	}

	rows := []struct {
		label string
		val   string
	}{
		{"Version", version},
		{"Mode", strings.ToUpper(cfg.Mode)},
		{"TCP Listener", fmt.Sprintf("http://%s", srv.HTTPAddr())},
		{"UNIX Socket", srv.SocketPath()},
		{
			"Database",
			fmt.Sprintf("%s (sqlite)", cfg.Storage.DatabasePath),
		},
	}

	maxLabel := 14
	for _, r := range rows {
		pad := strings.Repeat(" ", maxLabel-len(r.label))
		renderedLabel := ui.LabelStyle.Render(r.label)
		if _, err := fmt.Fprintf(
			w,
			"%s%s  %s\n",
			renderedLabel,
			pad,
			r.val,
		); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintln(w)
	return err
}
