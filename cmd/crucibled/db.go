// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"gotcode.org/crucible/internal/config"
	"gotcode.org/crucible/internal/storage"
	"gotcode.org/crucible/internal/ui"
)

func dbCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Inspect and manage the SQLite database lifecycle",
		Long: "The db command provides subcommands to inspect database " +
			"status, verify WAL integrity, and apply embedded schema migrations.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(dbMigrateCmd())
	cmd.AddCommand(dbStatusCmd())

	return cmd
}

func dbMigrateCmd() *cobra.Command {
	var (
		configPath string
		dbPath     string
	)

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply all pending embedded schema migrations to the database",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			dbInstance, err := openDatabase(ctx, configPath, dbPath)
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := dbInstance.Close(); closeErr != nil {
					_ = closeErr
				}
			}()

			if mErr := dbInstance.Migrate(ctx); mErr != nil {
				return fmt.Errorf("executing migrations: %w", mErr)
			}

			records, recErr := storage.GetAppliedMigrations(ctx, dbInstance.DB())
			if recErr != nil {
				return fmt.Errorf("retrieving applied migrations: %w", recErr)
			}

			w := cmd.OutOrStdout()
			if err := printMigrationsTable(w, records); err != nil {
				return err
			}

			msg := ui.SuccessMsg(
				"Database successfully migrated (%s)",
				dbInstance.Path(),
			)
			_, printErr := fmt.Fprintln(w, msg)
			return printErr
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

	return cmd
}

func dbStatusCmd() *cobra.Command {
	var (
		configPath string
		dbPath     string
	)

	cmd := &cobra.Command{
		Use: "status",
		Short: "Display operational metrics, WAL mode, integrity, " +
			"and schema version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			dbInstance, err := openDatabase(ctx, configPath, dbPath)
			if err != nil {
				return err
			}
			defer func() {
				if closeErr := dbInstance.Close(); closeErr != nil {
					_ = closeErr
				}
			}()

			st, stErr := dbInstance.Status(ctx)
			if stErr != nil {
				return fmt.Errorf("querying database status: %w", stErr)
			}

			w := cmd.OutOrStdout()
			return printStatusTable(w, st)
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

	return cmd
}

func openDatabase(
	ctx context.Context,
	configPath, dbOverride string,
) (*storage.DB, error) {
	svc := config.DefaultQueryService()
	var flagOverrides config.ServerFlagOverrides
	if dbOverride != "" {
		flagOverrides.DatabasePath = &dbOverride
	}

	cfg, err := svc.GetServerConfig(ctx, config.GetServerConfigQuery{
		ConfigFile:     configPath,
		Flags:          flagOverrides,
		SkipValidation: true,
	})
	if err != nil {
		return nil, fmt.Errorf("loading server configuration: %w", err)
	}

	dbInstance, err := storage.Open(ctx, cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf(
			"opening database at %s: %w",
			cfg.Storage.DatabasePath,
			err,
		)
	}

	return dbInstance, nil
}

func printMigrationsTable(
	w io.Writer,
	records []storage.MigrationRecord,
) error {
	col1Width := 7
	col2Width := 24
	col3Width := 22

	for _, r := range records {
		if len(r.Name) > col2Width {
			col2Width = len(r.Name)
		}
	}

	hCol1 := ui.BoldStyle.Render("VERSION")
	hCol2 := ui.BoldStyle.Render("MIGRATION SCRIPT")
	hCol3 := ui.BoldStyle.Render("APPLIED AT")

	pad1 := strings.Repeat(" ", col1Width-lipgloss.Width(hCol1))
	pad2 := strings.Repeat(" ", col2Width-lipgloss.Width(hCol2))

	if _, err := fmt.Fprintf(
		w,
		"%s%s  %s%s  %s\n",
		hCol1, pad1,
		hCol2, pad2,
		hCol3,
	); err != nil {
		return err
	}

	divStyle := lipgloss.NewStyle().Foreground(ui.Subtle)
	div1 := divStyle.Render(strings.Repeat("─", col1Width))
	div2 := divStyle.Render(strings.Repeat("─", col2Width))
	div3 := divStyle.Render(strings.Repeat("─", col3Width))

	if _, err := fmt.Fprintf(w, "%s  %s  %s\n", div1, div2, div3); err != nil {
		return err
	}

	for _, r := range records {
		vStr := fmt.Sprintf("%03d", r.Version)
		timeStr := r.AppliedAt.UTC().Format("2006-01-02 15:04:05 UTC")
		if r.AppliedAt.IsZero() {
			timeStr = "pending"
		}

		p1 := strings.Repeat(" ", col1Width-len(vStr))
		p2 := strings.Repeat(" ", col2Width-len(r.Name))

		renderedV := ui.LabelStyle.Render(vStr)
		if _, err := fmt.Fprintf(
			w,
			"%s%s  %s%s  %s\n",
			renderedV, p1,
			r.Name, p2,
			timeStr,
		); err != nil {
			return err
		}
	}

	_, err := fmt.Fprintln(w)
	return err
}

func printStatusTable(w io.Writer, st *storage.Status) error {
	type kv struct {
		label string
		val   string
	}

	integrityStr := ui.BadgeActive("OK")
	if !st.IntegrityOK {
		integrityStr = ui.BadgeDown("FAILED")
	}

	fkStr := ui.BadgeActive("ENABLED")
	if !st.ForeignKeysOn {
		fkStr = ui.BadgeDown("DISABLED")
	}

	stateStr := ui.BadgeActive("READY")
	if !st.FileExists {
		stateStr = ui.BadgeDown("NOT FOUND")
	}

	lastMigStr := "none"
	if !st.LastMigrationAt.IsZero() {
		lastMigStr = st.LastMigrationAt.UTC().Format(time.RFC3339)
	}

	rows := []kv{
		{"Database Path", st.DatabasePath},
		{"Status", stateStr},
		{"File Size", formatBytes(st.FileSizeBytes)},
		{"Integrity Check", integrityStr},
		{"Journal Mode", strings.ToUpper(st.JournalMode)},
		{"Foreign Keys", fkStr},
		{"Busy Timeout", fmt.Sprintf("%d ms", st.BusyTimeoutMs)},
		{"Schema Version", fmt.Sprintf("%d", st.LatestVersion)},
		{"Tables Count", fmt.Sprintf("%d", st.TablesCount)},
		{"Applied Migrations", fmt.Sprintf("%d", st.MigrationCount)},
		{"Last Migration At", lastMigStr},
	}

	maxLabelWidth := 20
	for _, r := range rows {
		if w := lipgloss.Width(r.label); w > maxLabelWidth {
			maxLabelWidth = w
		}
	}

	for _, r := range rows {
		pad := strings.Repeat(" ", maxLabelWidth-lipgloss.Width(r.label))
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

	return nil
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
