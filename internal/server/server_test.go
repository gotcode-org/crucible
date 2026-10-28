// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gotcode.org/crucible/internal/config"
	"gotcode.org/crucible/internal/server"
	"gotcode.org/crucible/internal/storage"
)

func setupTestServer(
	t *testing.T,
	migrateDB bool,
) (*server.HTTPServer, storage.Engine, string) {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	sockPath := filepath.Join(tmpDir, "test.sock")

	storageCfg := config.StorageConfig{
		DatabasePath:  dbPath,
		WALMode:       true,
		BusyTimeoutMs: 5000,
	}

	engine, err := storage.NewSQLiteAdapter(context.Background(), storageCfg)
	if err != nil {
		t.Fatalf("failed to create sqlite adapter: %v", err)
	}

	if migrateDB {
		if mErr := engine.Migrate(context.Background()); mErr != nil {
			t.Fatalf("failed to migrate test db: %v", mErr)
		}
	}

	srvCfg := server.Config{
		HTTPBind:   "127.0.0.1",
		HTTPPort:   0, // dynamic free port
		SocketPath: sockPath,
		SocketMode: 0660,
		DaemonMode: "standalone",
		Version:    "v0.1.0-test",
	}

	srv, err := server.NewServer(srvCfg, engine, nil)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := srv.Start(ctx); err != nil {
		cancel()
		t.Fatalf("failed to start server: %v", err)
	}

	t.Cleanup(func() {
		cancel()
		shutdownCtx, sCancel := context.WithTimeout(
			context.Background(),
			2*time.Second,
		)
		defer sCancel()
		if sErr := srv.Shutdown(shutdownCtx); sErr != nil {
			t.Errorf("cleanup server shutdown: %v", sErr)
		}
		if cErr := engine.Close(); cErr != nil {
			t.Errorf("cleanup close engine: %v", cErr)
		}
	})

	return srv, engine, sockPath
}

func TestServer_Healthz_OK(t *testing.T) {
	t.Parallel()

	srv, _, _ := setupTestServer(t, true)

	url := "http://" + srv.HTTPAddr() + "/healthz"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("closing response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var health server.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("failed to decode health JSON: %v", err)
	}

	if health.Status != "ok" {
		t.Errorf("expected status 'ok', got %q", health.Status)
	}
	if health.Version != "v0.1.0-test" {
		t.Errorf("expected version 'v0.1.0-test', got %q", health.Version)
	}
	if health.Database.Driver != "sqlite" {
		t.Errorf("expected driver 'sqlite', got %q", health.Database.Driver)
	}
	if !health.Database.IntegrityOK {
		t.Errorf("expected integrity_ok to be true")
	}
	if health.Database.MigrationsApplied != 3 {
		t.Errorf(
			"expected 3 migrations applied, got %d",
			health.Database.MigrationsApplied,
		)
	}
}

func TestServer_Dashboard(t *testing.T) {
	t.Parallel()

	srv, _, _ := setupTestServer(t, true)

	// 1. Root dashboard request
	resp, err := http.Get("http://" + srv.HTTPAddr() + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("closing response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	html := string(body)

	expectedTokens := []string{
		"CRUCIBLE",
		"Command Overview",
		"ONLINE",
		"SQLITE",
		"001_initial.sql",
		"002_indexes.sql",
		"003_audit_ledger.sql",
		"curl --unix-socket",
	}

	for _, token := range expectedTokens {
		if !strings.Contains(html, token) {
			t.Errorf("expected dashboard HTML to contain %q", token)
		}
	}

	// 2. Not found path
	nfResp, err := http.Get("http://" + srv.HTTPAddr() + "/invalid-route")
	if err != nil {
		t.Fatalf("GET /invalid-route failed: %v", err)
	}
	defer func() {
		if closeErr := nfResp.Body.Close(); closeErr != nil {
			t.Errorf("closing not-found response body: %v", closeErr)
		}
	}()

	if nfResp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for invalid route, got %d", nfResp.StatusCode)
	}
}

func TestServer_StaticAssets(t *testing.T) {
	t.Parallel()

	srv, _, _ := setupTestServer(t, false)

	tests := []struct {
		path       string
		wantStatus int
		wantSubstr string
	}{
		{
			path:       "/static/css/style.css",
			wantStatus: http.StatusOK,
			wantSubstr: "Plus Jakarta Sans",
		},
		{
			path:       "/static/js/app.js",
			wantStatus: http.StatusOK,
			wantSubstr: "Crucible Web Shell Client Script",
		},
		{
			path:       "/static/nonexistent.file",
			wantStatus: http.StatusNotFound,
			wantSubstr: "404",
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			resp, err := http.Get("http://" + srv.HTTPAddr() + tt.path)
			if err != nil {
				t.Fatalf("GET %s failed: %v", tt.path, err)
			}
			defer func() {
				if closeErr := resp.Body.Close(); closeErr != nil {
					t.Errorf("closing response body: %v", closeErr)
				}
			}()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf(
					"expected status %d, got %d",
					tt.wantStatus,
					resp.StatusCode,
				)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("reading body: %v", err)
			}
			if !strings.Contains(string(body), tt.wantSubstr) {
				t.Errorf(
					"expected response to contain %q, got: %s",
					tt.wantSubstr,
					string(body),
				)
			}
		})
	}
}

func TestServer_UnixSocket(t *testing.T) {
	t.Parallel()

	srv, _, _ := setupTestServer(t, true)
	sockPath := srv.SocketPath()

	// Verify file mode
	info, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("stat on unix socket failed: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Errorf("expected socket mode, got %v", info.Mode())
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(
				ctx context.Context,
				_, _ string,
			) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sockPath)
			},
		},
	}

	resp, err := client.Get("http://localhost/healthz")
	if err != nil {
		t.Fatalf("request over unix socket failed: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("closing response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var health server.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("decoding unix socket health JSON: %v", err)
	}

	if health.Status != "ok" {
		t.Errorf("expected 'ok', got %q", health.Status)
	}
}

func TestServer_Healthz_Degraded(t *testing.T) {
	t.Parallel()

	srv, engine, _ := setupTestServer(t, false)

	// Close database engine to trigger health degradation
	if err := engine.Close(); err != nil {
		t.Fatalf("closing engine: %v", err)
	}

	resp, err := http.Get("http://" + srv.HTTPAddr() + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz failed: %v", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("closing response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf(
			"expected 503 Service Unavailable, got %d",
			resp.StatusCode,
		)
	}

	var health server.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("failed to decode health JSON: %v", err)
	}

	if health.Status != "degraded" {
		t.Errorf("expected status 'degraded', got %q", health.Status)
	}
	if health.Database.Status != "unreachable" {
		t.Errorf(
			"expected database status 'unreachable', got %q",
			health.Database.Status,
		)
	}
}

func TestServer_GracefulShutdown(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "shutdown.db")
	sockPath := filepath.Join(tmpDir, "shutdown.sock")

	engine, err := storage.NewSQLiteAdapter(
		context.Background(),
		config.StorageConfig{DatabasePath: dbPath},
	)
	if err != nil {
		t.Fatalf("creating adapter: %v", err)
	}
	defer func() {
		if cErr := engine.Close(); cErr != nil {
			t.Errorf("closing engine: %v", cErr)
		}
	}()

	srv, err := server.NewServer(server.Config{
		HTTPBind:   "127.0.0.1",
		HTTPPort:   0,
		SocketPath: sockPath,
	}, engine, nil)
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("starting server: %v", err)
	}

	// Verify socket exists while running
	if _, err := os.Stat(sockPath); err != nil {
		t.Fatalf("socket file should exist while running: %v", err)
	}

	// Shutdown
	sCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(sCtx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	// Verify socket file was deleted
	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Errorf("expected socket file %s to be removed, but still exists", sockPath)
	}
}
