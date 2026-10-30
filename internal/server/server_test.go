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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gotcode.org/crucible/internal/auth"
	"gotcode.org/crucible/internal/config"
	"gotcode.org/crucible/internal/server"
	"gotcode.org/crucible/internal/storage"
	"gotcode.org/crucible/internal/tokens"
)

func setupTestServer(
	t *testing.T,
	migrateDB bool,
) (*server.HTTPServer, *storage.SQLiteAdapter, string) {
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

	srv, err := server.NewServer(srvCfg, testServerDependencies(engine))
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

func testServerDependencies(engine *storage.SQLiteAdapter) server.Dependencies {
	authStore := storage.NewSQLiteAuthStore(engine)
	return server.Dependencies{
		Engine:       engine,
		QueryService: storage.NewQueryService(engine),
		TokenService: tokens.NewTokenService(
			storage.NewSQLiteTokenStore(engine),
			authStore,
		),
	}
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
	if health.Database.MigrationsApplied != 5 {
		t.Errorf(
			"expected 5 migrations applied, got %d",
			health.Database.MigrationsApplied,
		)
	}
}

func testPublicDashboardContent(t *testing.T, srv *server.HTTPServer) {
	t.Helper()
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
	pubHTML := string(body)

	pubTokens := []string{
		"CRUCIBLE",
		"PUBLIC MONITOR",
		"Public Pipeline Monitor",
		"Operator Sign In",
		"Sovereign Execution",
	}

	for _, token := range pubTokens {
		if !strings.Contains(pubHTML, token) {
			t.Errorf("expected public dashboard HTML to contain %q", token)
		}
	}
}

func testOperatorDashboardContent(t *testing.T, srv *server.HTTPServer) {
	t.Helper()
	opResp, err := http.Get("http://" + srv.HTTPAddr() + "/operator")
	if err != nil {
		t.Fatalf("GET /operator failed: %v", err)
	}
	defer func() {
		if closeErr := opResp.Body.Close(); closeErr != nil {
			t.Errorf("closing response body: %v", closeErr)
		}
	}()

	if opResp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", opResp.StatusCode)
	}

	opBody, err := io.ReadAll(opResp.Body)
	if err != nil {
		t.Fatalf("failed to read operator response body: %v", err)
	}
	opHTML := string(opBody)

	opTokens := []string{
		"CRUCIBLE",
		"Command Overview",
		"ONLINE",
		"SQLITE",
		"001_initial.sql",
		"002_indexes.sql",
		"003_audit_ledger.sql",
		"curl --unix-socket",
	}

	for _, token := range opTokens {
		if !strings.Contains(opHTML, token) {
			t.Errorf("expected operator dashboard HTML to contain %q", token)
		}
	}
}

func testNotFoundPath(t *testing.T, srv *server.HTTPServer) {
	t.Helper()
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

func TestServer_Dashboard(t *testing.T) {
	t.Parallel()

	srv, _, _ := setupTestServer(t, true)

	testPublicDashboardContent(t, srv)
	testOperatorDashboardContent(t, srv)
	testNotFoundPath(t, srv)
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
	}, testServerDependencies(engine))
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

func setupAuthServer(
	t *testing.T,
) (*server.HTTPServer, string, *auth.CommandService) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "auth_test.db")
	sockPath := filepath.Join(tmpDir, "auth_test.sock")

	engine, err := storage.NewSQLiteAdapter(
		context.Background(),
		config.StorageConfig{
			DatabasePath:  dbPath,
			WALMode:       false,
			BusyTimeoutMs: 5000,
		},
	)
	if err != nil {
		t.Fatalf("opening sqlite engine: %v", err)
	}

	if mErr := engine.Migrate(context.Background()); mErr != nil {
		t.Fatalf("running initial migration: %v", mErr)
	}

	memAuth := auth.NewMemoryAuthenticator()
	memAuth.AddUser("alice", "correct-horse", auth.RoleAdmin)

	authStore := storage.NewSQLiteAuthStore(engine)
	crypter := auth.NewPlaintextCrypter()
	cmdSvc := auth.NewCommandService(
		memAuth,
		authStore,
		authStore,
		authStore,
		authStore,
		authStore,
		authStore,
		crypter,
		false,
	)
	querySvc := auth.NewQueryService(authStore, authStore, authStore)

	srv, err := server.NewServer(server.Config{
		HTTPBind:   "127.0.0.1",
		HTTPPort:   0,
		SocketPath: sockPath,
		DaemonMode: "standalone",
		Version:    "v0.1.0-authtest",
	}, server.Dependencies{
		Engine:       engine,
		QueryService: storage.NewQueryService(engine),
		AuthCommands: cmdSvc,
		AuthQueries:  querySvc,
		TokenService: tokens.NewTokenService(
			storage.NewSQLiteTokenStore(engine),
			authStore,
		),
	})
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}

	if sErr := srv.Start(context.Background()); sErr != nil {
		t.Fatalf("starting server: %v", sErr)
	}

	t.Cleanup(func() {
		if cErr := srv.Shutdown(context.Background()); cErr != nil {
			t.Logf("shutdown server: %v", cErr)
		}
		if cErr := engine.Close(); cErr != nil {
			t.Logf("close engine: %v", cErr)
		}
	})

	return srv, "http://" + srv.HTTPAddr(), cmdSvc
}

func closeBody(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp != nil && resp.Body != nil {
		if err := resp.Body.Close(); err != nil {
			t.Logf("closing response body: %v", err)
		}
	}
}

func TestAuthWeb_UnauthenticatedRedirect(t *testing.T) {
	t.Parallel()
	_, baseURL, _ := setupAuthServer(t)

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. Root / is public and accessible unauthenticated (200 OK)
	pubResp, err := client.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("get / unauthenticated: %v", err)
	}
	defer closeBody(t, pubResp)

	if pubResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK on public /, got %d", pubResp.StatusCode)
	}

	// 2. /operator requires authentication and redirects to login (302)
	opResp, err := client.Get(baseURL + "/operator")
	if err != nil {
		t.Fatalf("get /operator unauthenticated: %v", err)
	}
	defer closeBody(t, opResp)

	if opResp.StatusCode != http.StatusFound {
		t.Errorf(
			"expected 302 redirect to /login on /operator, got %d",
			opResp.StatusCode,
		)
	}
	loc := opResp.Header.Get("Location")
	if !strings.Contains(loc, "/login") || !strings.Contains(loc, "next=") {
		t.Errorf("expected redirect to login with next param, got %q", loc)
	}
}

func testWebLoginPage(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()
	resp, err := client.Get(baseURL + "/login")
	if err != nil {
		t.Fatalf("get /login: %v", err)
	}
	body, rErr := io.ReadAll(resp.Body)
	closeBody(t, resp)
	if rErr != nil {
		t.Fatalf("reading login body: %v", rErr)
	}
	if resp.StatusCode != http.StatusOK ||
		!strings.Contains(string(body), "Operator Authentication") ||
		!strings.Contains(string(body), "Native SQLite / TOTP 2FA") {
		t.Errorf("unexpected /login response: %d", resp.StatusCode)
	}
}

func testWebLoginBadPassword(
	t *testing.T,
	client *http.Client,
	baseURL string,
) {
	t.Helper()
	resp, err := client.PostForm(baseURL+"/login", url.Values{
		"username": {"alice"},
		"password": {"wrong-password"},
	})
	if err != nil {
		t.Fatalf("post bad login: %v", err)
	}
	closeBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 for bad password, got %d", resp.StatusCode)
	}
}

func testWebLoginValid(
	t *testing.T,
	client *http.Client,
	baseURL string,
) *http.Cookie {
	t.Helper()
	resp, err := client.PostForm(baseURL+"/login", url.Values{
		"username": {"alice"},
		"password": {"correct-horse"},
	})
	if err != nil {
		t.Fatalf("post valid login: %v", err)
	}
	closeBody(t, resp)
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302 redirect after login, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/operator" {
		t.Errorf("expected redirect to /operator after login, got %q", loc)
	}

	for _, c := range resp.Cookies() {
		if c.Name == server.SessionCookieName {
			return c
		}
	}
	t.Fatalf("expected session cookie set after successful login")
	return nil
}

func testWebDashboardAuth(
	t *testing.T,
	client *http.Client,
	baseURL string,
	sessionCookie *http.Cookie,
) {
	t.Helper()

	// 1. Operator console at /operator
	req, rErr := http.NewRequest(http.MethodGet, baseURL+"/operator", nil)
	if rErr != nil {
		t.Fatalf("creating req: %v", rErr)
	}
	req.AddCookie(sessionCookie)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get /operator: %v", err)
	}
	dashBody, rErr := io.ReadAll(resp.Body)
	closeBody(t, resp)
	if rErr != nil {
		t.Fatalf("reading dashboard body: %v", rErr)
	}
	if resp.StatusCode != http.StatusOK ||
		!strings.Contains(string(dashBody), "Command Overview") {
		t.Errorf("unexpected dashboard response: %d", resp.StatusCode)
	}

	// 2. Public portal at / with active session shows console button
	pubReq, rErr := http.NewRequest(http.MethodGet, baseURL+"/", nil)
	if rErr != nil {
		t.Fatalf("creating pub req: %v", rErr)
	}
	pubReq.AddCookie(sessionCookie)
	pubResp, err := client.Do(pubReq)
	if err != nil {
		t.Fatalf("get / with session: %v", err)
	}
	pubBody, rErr := io.ReadAll(pubResp.Body)
	closeBody(t, pubResp)
	if rErr != nil {
		t.Fatalf("reading public body: %v", rErr)
	}
	if pubResp.StatusCode != http.StatusOK ||
		!strings.Contains(string(pubBody), "Console") {
		t.Errorf("unexpected public response with auth: %d", pubResp.StatusCode)
	}
}

func testWebLogout(
	t *testing.T,
	client *http.Client,
	baseURL string,
	sessionCookie *http.Cookie,
) {
	t.Helper()
	outReq, oErr := http.NewRequest(http.MethodGet, baseURL+"/logout", nil)
	if oErr != nil {
		t.Fatalf("creating logout req: %v", oErr)
	}
	outReq.AddCookie(sessionCookie)
	resp, err := client.Do(outReq)
	if err != nil {
		t.Fatalf("logout req: %v", err)
	}
	closeBody(t, resp)
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302 on logout, got %d", resp.StatusCode)
	}
}

func TestAuthWeb_LoginAndDashboard(t *testing.T) {
	t.Parallel()
	_, baseURL, _ := setupAuthServer(t)

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	testWebLoginPage(t, client, baseURL)
	testWebLoginBadPassword(t, client, baseURL)
	sessionCookie := testWebLoginValid(t, client, baseURL)
	testWebDashboardAuth(t, client, baseURL, sessionCookie)
	testWebLogout(t, client, baseURL, sessionCookie)
}

func TestAuthWeb_Settings2FA(t *testing.T) {
	t.Parallel()
	_, baseURL, cmdSvc := setupAuthServer(t)

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	loginRes, err := cmdSvc.Login(context.Background(), auth.LoginCommand{
		Username: "alice",
		Password: "correct-horse",
	})
	if err != nil || loginRes.Session == nil {
		t.Fatalf("login failed: %v", err)
	}

	req, rErr := http.NewRequest(http.MethodGet, baseURL+"/settings/2fa", nil)
	if rErr != nil {
		t.Fatalf("creating req: %v", rErr)
	}
	req.AddCookie(&http.Cookie{
		Name:  server.SessionCookieName,
		Value: loginRes.Session.ID,
	})

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get settings 2fa: %v", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	closeBody(t, resp)
	if readErr != nil {
		t.Fatalf("reading settings body: %v", readErr)
	}

	if resp.StatusCode != http.StatusOK ||
		!strings.Contains(string(body), "Two-Factor Authentication") {
		t.Errorf("unexpected settings response: %d", resp.StatusCode)
	}
}

func testAPILoginDirect(
	t *testing.T,
	client *http.Client,
	baseURL string,
) string {
	t.Helper()
	loginJSON := `{"username":"alice","password":"correct-horse"}`
	resp, err := client.Post(
		baseURL+"/api/v1/auth/login",
		"application/json",
		strings.NewReader(loginJSON),
	)
	if err != nil {
		t.Fatalf("api login: %v", err)
	}
	var apiLoginRes struct {
		Requires2FA bool          `json:"requires_2fa"`
		Session     *auth.Session `json:"session"`
	}
	if dErr := json.NewDecoder(resp.Body).Decode(&apiLoginRes); dErr != nil {
		t.Fatalf("decoding api login response: %v", dErr)
	}
	closeBody(t, resp)

	if apiLoginRes.Requires2FA || apiLoginRes.Session == nil {
		t.Fatalf("expected direct login before device enrollment")
	}
	return apiLoginRes.Session.ID
}

func testAPIEnrollDevice(
	t *testing.T,
	client *http.Client,
	baseURL string,
	sessionID string,
) string {
	t.Helper()
	secret, secErr := auth.GenerateSecret()
	if secErr != nil {
		t.Fatalf("generating secret: %v", secErr)
	}
	code, codeErr := auth.GenerateCode(secret, time.Now().UTC())
	if codeErr != nil {
		t.Fatalf("generating code: %v", codeErr)
	}
	enrollJSON, mErr := json.Marshal(map[string]string{
		"device_name":   "API Key",
		"secret_base32": secret,
		"passcode":      code,
	})
	if mErr != nil {
		t.Fatalf("marshal enroll JSON: %v", mErr)
	}

	req, reqErr := http.NewRequest(
		http.MethodPost,
		baseURL+"/api/v1/auth/devices",
		strings.NewReader(string(enrollJSON)),
	)
	if reqErr != nil {
		t.Fatalf("creating enroll request: %v", reqErr)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+sessionID)
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("device enrollment API failed: %v", err)
	}
	closeBody(t, resp)
	return secret
}

func testAPILogin2FAChallenge(
	t *testing.T,
	client *http.Client,
	baseURL string,
) string {
	t.Helper()
	loginJSON := `{"username":"alice","password":"correct-horse"}`
	resp, err := client.Post(
		baseURL+"/api/v1/auth/login",
		"application/json",
		strings.NewReader(loginJSON),
	)
	if err != nil {
		t.Fatalf("second api login: %v", err)
	}
	var api2FARes struct {
		Requires2FA    bool   `json:"requires_2fa"`
		ChallengeToken string `json:"challenge_token"`
	}
	if dErr := json.NewDecoder(resp.Body).Decode(&api2FARes); dErr != nil {
		t.Fatalf("decoding api 2fa response: %v", dErr)
	}
	closeBody(t, resp)

	if !api2FARes.Requires2FA || api2FARes.ChallengeToken == "" {
		t.Fatalf("expected 2FA challenge on second login")
	}
	return api2FARes.ChallengeToken
}

func testAPIVerify2FA(
	t *testing.T,
	client *http.Client,
	baseURL string,
	challengeToken string,
	secret string,
) {
	t.Helper()
	code2FA, c2Err := auth.GenerateCode(secret, time.Now().UTC())
	if c2Err != nil {
		t.Fatalf("generating 2fa code: %v", c2Err)
	}
	verifyJSON, vmErr := json.Marshal(map[string]string{
		"challenge_token": challengeToken,
		"passcode":        code2FA,
	})
	if vmErr != nil {
		t.Fatalf("marshal verify JSON: %v", vmErr)
	}
	resp, err := client.Post(
		baseURL+"/api/v1/auth/verify-2fa",
		"application/json",
		strings.NewReader(string(verifyJSON)),
	)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("verify 2fa failed: %v", err)
	}
	closeBody(t, resp)
}

func TestAuthAPI_2FAFlow(t *testing.T) {
	t.Parallel()
	_, baseURL, _ := setupAuthServer(t)
	client := &http.Client{}

	sessID := testAPILoginDirect(t, client, baseURL)
	secret := testAPIEnrollDevice(t, client, baseURL, sessID)
	challengeToken := testAPILogin2FAChallenge(t, client, baseURL)
	testAPIVerify2FA(t, client, baseURL, challengeToken, secret)
}

func setupRequire2FAServer(t *testing.T) (string, *http.Client) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "crucible_enforce2fa.db")
	sockPath := filepath.Join(dir, "crucible_enforce2fa.sock")

	engine, err := storage.NewSQLiteAdapter(
		context.Background(),
		config.StorageConfig{
			DatabasePath:  dbPath,
			WALMode:       false,
			BusyTimeoutMs: 5000,
		},
	)
	if err != nil {
		t.Fatalf("creating engine: %v", err)
	}

	if mErr := engine.Migrate(context.Background()); mErr != nil {
		t.Fatalf("running migrations: %v", mErr)
	}

	memAuth := auth.NewMemoryAuthenticator()
	memAuth.AddUser("dave", "securepass123", auth.RoleAdmin)
	authStore := storage.NewSQLiteAuthStore(engine)
	crypter := auth.NewPlaintextCrypter()
	cmdSvc := auth.NewCommandService(
		memAuth,
		authStore,
		authStore,
		authStore,
		authStore,
		authStore,
		authStore,
		crypter,
		false,
	)
	querySvc := auth.NewQueryService(authStore, authStore, authStore)

	srv, err := server.NewServer(server.Config{
		HTTPBind:     "127.0.0.1",
		HTTPPort:     0,
		SocketPath:   sockPath,
		DaemonMode:   "standalone",
		Version:      "v0.1.0-authtest",
		AuthProvider: "sqlite",
		Require2FA:   true,
	}, server.Dependencies{
		Engine:       engine,
		QueryService: storage.NewQueryService(engine),
		AuthCommands: cmdSvc,
		AuthQueries:  querySvc,
		TokenService: tokens.NewTokenService(
			storage.NewSQLiteTokenStore(engine),
			authStore,
		),
	})
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}

	if sErr := srv.Start(context.Background()); sErr != nil {
		t.Fatalf("starting server: %v", sErr)
	}

	t.Cleanup(func() {
		if cErr := srv.Shutdown(context.Background()); cErr != nil {
			t.Logf("shutdown server: %v", cErr)
		}
		if cErr := engine.Close(); cErr != nil {
			t.Logf("close engine: %v", cErr)
		}
	})

	baseURL := "http://" + srv.HTTPAddr()
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return baseURL, client
}

func verifyEnforcePage(
	t *testing.T,
	client *http.Client,
	baseURL, loc string,
	cookie *http.Cookie,
) {
	t.Helper()
	enforceReq, err := http.NewRequest(
		http.MethodGet,
		baseURL+loc,
		nil,
	)
	if err != nil {
		t.Fatalf("creating enforce request: %v", err)
	}
	enforceReq.AddCookie(cookie)
	enforceResp, err := client.Do(enforceReq)
	if err != nil {
		t.Fatalf("getting enforce page: %v", err)
	}
	defer closeBody(t, enforceResp)
	if enforceResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from enforce page, got %d",
			enforceResp.StatusCode)
	}
	enforceBody, err := io.ReadAll(enforceResp.Body)
	if err != nil {
		t.Fatalf("reading enforce body: %v", err)
	}
	enforceHTML := string(enforceBody)
	if !strings.Contains(
		enforceHTML,
		"Two-Factor Authentication Required",
	) {
		t.Errorf("enforce page missing requirement heading")
	}
	if strings.Contains(enforceHTML, "<aside") ||
		strings.Contains(enforceHTML, "account-menu-btn") {
		t.Errorf("enforce page must not contain sidebar or account menu")
	}
}

func TestAuth_Require2FAEnforcement(t *testing.T) {
	t.Parallel()
	baseURL, client := setupRequire2FAServer(t)

	// 1. Web login: should redirect to /settings/2fa?enforce=true
	resp, err := client.PostForm(baseURL+"/login", url.Values{
		"username": {"dave"},
		"password": {"securepass123"},
	})
	if err != nil {
		t.Fatalf("web login error: %v", err)
	}
	closeBody(t, resp)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "/settings/2fa?enforce=true" {
		t.Fatalf("expected redirect to /settings/2fa?enforce=true, got %q", loc)
	}

	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == server.SessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie from login redirect")
	}

	// 2. Enforce 2FA page: renders small standalone window without sidebar
	verifyEnforcePage(t, client, baseURL, loc, sessionCookie)

	// 3. API login: should return 428 Precondition Required
	loginJSON := `{"username":"dave","password":"securepass123"}`
	apiResp, err := client.Post(
		baseURL+"/api/v1/auth/login",
		"application/json",
		strings.NewReader(loginJSON),
	)
	if err != nil {
		t.Fatalf("api login error: %v", err)
	}
	defer closeBody(t, apiResp)
	if apiResp.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf(
			"expected 428 Precondition Required, got %d",
			apiResp.StatusCode,
		)
	}
}

func setupPasswordLifecycleServer(
	t *testing.T,
	policy config.PasswordPolicyConfig,
) (*server.HTTPServer, string, *auth.CommandService, *storage.SQLiteAuthStore) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "pwd_test.db")
	sockPath := filepath.Join(tmpDir, "pwd_test.sock")

	engine, err := storage.NewSQLiteAdapter(
		context.Background(),
		config.StorageConfig{
			DatabasePath:  dbPath,
			WALMode:       false,
			BusyTimeoutMs: 5000,
		},
	)
	if err != nil {
		t.Fatalf("opening sqlite engine: %v", err)
	}

	if mErr := engine.Migrate(context.Background()); mErr != nil {
		t.Fatalf("running initial migration: %v", mErr)
	}

	authStore := storage.NewSQLiteAuthStore(engine)
	expiry, pErr := policy.ExpiryDuration()
	if pErr != nil {
		t.Fatalf("parsing policy duration: %v", pErr)
	}
	sqliteAuth := auth.NewSQLiteAuthenticator(
		authStore,
	).WithPasswordExpiry(expiry)

	crypter := auth.NewPlaintextCrypter()
	cmdSvc := auth.NewCommandService(
		sqliteAuth,
		authStore,
		authStore,
		authStore,
		authStore,
		authStore,
		authStore,
		crypter,
		false,
	)
	querySvc := auth.NewQueryService(
		authStore,
		authStore,
		authStore,
		authStore,
	)

	adminHash, hErr := auth.HashPassword("AdminPass123!")
	if hErr != nil {
		t.Fatalf("hashing admin password: %v", hErr)
	}
	if cErr := authStore.CreateUser(
		context.Background(),
		&auth.User{
			Username:           "admin",
			PasswordHash:       &adminHash,
			Role:               auth.RoleAdmin,
			IsActive:           true,
			MustChangePassword: false,
		},
	); cErr != nil {
		t.Fatalf("creating admin user: %v", cErr)
	}

	srv, err := server.NewServer(server.Config{
		HTTPBind:       "127.0.0.1",
		HTTPPort:       0,
		SocketPath:     sockPath,
		DaemonMode:     "standalone",
		Version:        "v0.1.0-pwdtest",
		PasswordPolicy: policy,
	}, server.Dependencies{
		Engine:       engine,
		QueryService: storage.NewQueryService(engine),
		AuthCommands: cmdSvc,
		AuthQueries:  querySvc,
		TokenService: tokens.NewTokenService(
			storage.NewSQLiteTokenStore(engine),
			authStore,
		),
	})
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}

	if sErr := srv.Start(context.Background()); sErr != nil {
		t.Fatalf("starting server: %v", sErr)
	}

	t.Cleanup(func() {
		if cErr := srv.Shutdown(context.Background()); cErr != nil {
			t.Logf("shutdown server: %v", cErr)
		}
		if cErr := engine.Close(); cErr != nil {
			t.Logf("close engine: %v", cErr)
		}
	})

	return srv, "http://" + srv.HTTPAddr(), cmdSvc, authStore
}

func doHTTPReq(
	t *testing.T,
	client *http.Client,
	method, urlStr string,
	body io.Reader,
	contentType string,
	cookies ...*http.Cookie,
) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		t.Fatalf("creating %s request: %v", method, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for _, c := range cookies {
		if c != nil {
			req.AddCookie(c)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("performing %s %s: %v", method, urlStr, err)
	}
	bodyBytes, rErr := io.ReadAll(resp.Body)
	if rErr != nil {
		t.Fatalf("reading response body: %v", rErr)
	}
	if cErr := resp.Body.Close(); cErr != nil {
		t.Logf("closing response body: %v", cErr)
	}
	return resp, string(bodyBytes)
}

func findSessionCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == server.SessionCookieName {
			return c
		}
	}
	return nil
}

func TestAuthWeb_SettingsPassword(t *testing.T) {
	t.Parallel()
	_, baseURL, _, authStore := setupPasswordLifecycleServer(
		t,
		config.PasswordPolicyConfig{
			RequireChangeOnFirstLogin: true,
			MinLength:                 8,
		},
	)

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	testSettingsPasswordUnauth(t, client, baseURL)
	testSettingsPasswordAdmin(t, client, baseURL)
	testSettingsPasswordEnforcementAndChange(t, client, baseURL, authStore)
}

func testSettingsPasswordUnauth(
	t *testing.T,
	client *http.Client,
	baseURL string,
) {
	t.Helper()
	resp, _ := doHTTPReq(
		t,
		client,
		http.MethodGet,
		baseURL+"/settings/password",
		nil,
		"",
	)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 for unauth password page, got %d",
			resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Location"), "/login") {
		t.Fatalf("expected redirect to /login, got %q",
			resp.Header.Get("Location"))
	}
}

func testSettingsPasswordAdmin(
	t *testing.T,
	client *http.Client,
	baseURL string,
) {
	t.Helper()
	loginForm := url.Values{
		"username": {"admin"},
		"password": {"AdminPass123!"},
	}
	resp, _ := doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/login",
		strings.NewReader(loginForm.Encode()),
		"application/x-www-form-urlencoded",
	)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 login, got %d", resp.StatusCode)
	}
	adminCookie := findSessionCookie(resp)
	if adminCookie == nil {
		t.Fatal("expected session cookie for admin")
	}

	resp, body := doHTTPReq(
		t,
		client,
		http.MethodGet,
		baseURL+"/settings/password",
		nil,
		"",
		adminCookie,
	)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for admin password page, got %d",
			resp.StatusCode)
	}
	if !strings.Contains(body, "Change Password") ||
		!strings.Contains(body, "Current Password") {
		t.Fatal("expected password change form in body")
	}
	if !strings.Contains(body, "<aside") {
		t.Fatal("expected normal password page to contain sidebar")
	}
}

func testSettingsPasswordEnforcementAndChange(
	t *testing.T,
	client *http.Client,
	baseURL string,
	authStore *storage.SQLiteAuthStore,
) {
	t.Helper()
	opHash, hErr := auth.HashPassword("InitPass123!")
	if hErr != nil {
		t.Fatalf("hashing initial password: %v", hErr)
	}
	if err := authStore.CreateUser(
		context.Background(),
		&auth.User{
			Username:           "alice",
			PasswordHash:       &opHash,
			Role:               auth.RoleOperator,
			IsActive:           true,
			MustChangePassword: true,
		},
	); err != nil {
		t.Fatalf("creating user alice: %v", err)
	}

	aliceCookie := loginExpectingPasswordEnforce(t, client, baseURL)
	testPasswordMismatchAndWrongCurrent(t, client, baseURL, aliceCookie)
	testPasswordChangeSuccess(t, client, baseURL, aliceCookie)
}

func loginExpectingPasswordEnforce(
	t *testing.T,
	client *http.Client,
	baseURL string,
) *http.Cookie {
	t.Helper()
	aliceForm := url.Values{
		"username": {"alice"},
		"password": {"InitPass123!"},
	}
	resp, _ := doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/login",
		strings.NewReader(aliceForm.Encode()),
		"application/x-www-form-urlencoded",
	)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 login for alice, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "/settings/password?enforce=true" {
		t.Fatalf("expected redirect to password enforce, got %q",
			resp.Header.Get("Location"))
	}
	aliceCookie := findSessionCookie(resp)
	if aliceCookie == nil {
		t.Fatal("expected session cookie for alice")
	}

	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodGet,
		baseURL+"/operator",
		nil,
		"",
		aliceCookie,
	)
	if resp.StatusCode != http.StatusFound ||
		resp.Header.Get("Location") != "/settings/password?enforce=true" {
		t.Fatalf("expected 302 to password enforce, got status %d loc %q",
			resp.StatusCode, resp.Header.Get("Location"))
	}

	enforceResp, enforceBody := doHTTPReq(
		t,
		client,
		http.MethodGet,
		baseURL+"/settings/password?enforce=true",
		nil,
		"",
		aliceCookie,
	)
	if enforceResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on enforce password page, got %d",
			enforceResp.StatusCode)
	}
	if !strings.Contains(enforceBody, "Password Change Required") {
		t.Fatal("expected Password Change Required in enforce body")
	}
	if strings.Contains(enforceBody, "<aside") ||
		strings.Contains(enforceBody, "account-menu-btn") {
		t.Fatal("expected enforce password page to have no sidebar")
	}

	return aliceCookie
}

func testPasswordMismatchAndWrongCurrent(
	t *testing.T,
	client *http.Client,
	baseURL string,
	aliceCookie *http.Cookie,
) {
	t.Helper()
	mismatchForm := url.Values{
		"current_password": {"InitPass123!"},
		"new_password":     {"NewSecurePass123!"},
		"confirm_password": {"DifferentPass123!"},
	}
	resp, _ := doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/password",
		strings.NewReader(mismatchForm.Encode()),
		"application/x-www-form-urlencoded",
		aliceCookie,
	)
	if resp.StatusCode != http.StatusFound ||
		!strings.Contains(resp.Header.Get("Location"), "error=") {
		t.Fatalf("expected redirect with error for mismatch, got %d loc %q",
			resp.StatusCode, resp.Header.Get("Location"))
	}

	wrongCurrentForm := url.Values{
		"current_password": {"WrongCurrentPass123!"},
		"new_password":     {"NewSecurePass123!"},
		"confirm_password": {"NewSecurePass123!"},
	}
	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/password",
		strings.NewReader(wrongCurrentForm.Encode()),
		"application/x-www-form-urlencoded",
		aliceCookie,
	)
	if resp.StatusCode != http.StatusFound ||
		!strings.Contains(resp.Header.Get("Location"), "error=") {
		t.Fatalf("expected redirect with error for wrong current password")
	}
}

func testPasswordChangeSuccess(
	t *testing.T,
	client *http.Client,
	baseURL string,
	aliceCookie *http.Cookie,
) {
	t.Helper()
	successForm := url.Values{
		"current_password": {"InitPass123!"},
		"new_password":     {"NewSecurePass123!"},
		"confirm_password": {"NewSecurePass123!"},
	}
	resp, _ := doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/password",
		strings.NewReader(successForm.Encode()),
		"application/x-www-form-urlencoded",
		aliceCookie,
	)
	if resp.StatusCode != http.StatusFound ||
		!strings.Contains(resp.Header.Get("Location"), "/operator") {
		t.Fatalf("expected redirect to /operator after password change, got %q",
			resp.Header.Get("Location"))
	}

	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodGet,
		baseURL+"/operator",
		nil,
		"",
		aliceCookie,
	)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on /operator after password change, got %d",
			resp.StatusCode)
	}

	newLoginForm := url.Values{
		"username": {"alice"},
		"password": {"NewSecurePass123!"},
	}
	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/login",
		strings.NewReader(newLoginForm.Encode()),
		"application/x-www-form-urlencoded",
	)
	if resp.StatusCode != http.StatusFound ||
		resp.Header.Get("Location") != "/operator" {
		t.Fatalf("expected redirect to /operator on new password login, got %q",
			resp.Header.Get("Location"))
	}
}

func TestAuthWeb_SettingsUsers(t *testing.T) {
	t.Parallel()
	_, baseURL, _, authStore := setupPasswordLifecycleServer(
		t,
		config.PasswordPolicyConfig{
			RequireChangeOnFirstLogin: true,
			MinLength:                 8,
		},
	)

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	adminCookie := testSettingsUsersAuthGuards(t, client, baseURL, authStore)
	testSettingsUsersAdminCRUD(t, client, baseURL, authStore, adminCookie)
}

func testSettingsUsersAuthGuards(
	t *testing.T,
	client *http.Client,
	baseURL string,
	authStore *storage.SQLiteAuthStore,
) *http.Cookie {
	t.Helper()
	resp, _ := doHTTPReq(
		t,
		client,
		http.MethodGet,
		baseURL+"/settings/users",
		nil,
		"",
	)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 unauth users redirect, got %d", resp.StatusCode)
	}

	opHash, hErr := auth.HashPassword("OpPass1234!")
	if hErr != nil {
		t.Fatalf("hashing op password: %v", hErr)
	}
	if err := authStore.CreateUser(
		context.Background(),
		&auth.User{
			Username:           "bob",
			PasswordHash:       &opHash,
			Role:               auth.RoleOperator,
			IsActive:           true,
			MustChangePassword: false,
		},
	); err != nil {
		t.Fatalf("creating user bob: %v", err)
	}
	bobLoginForm := url.Values{
		"username": {"bob"},
		"password": {"OpPass1234!"},
	}
	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/login",
		strings.NewReader(bobLoginForm.Encode()),
		"application/x-www-form-urlencoded",
	)
	bobCookie := findSessionCookie(resp)

	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodGet,
		baseURL+"/settings/users",
		nil,
		"",
		bobCookie,
	)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for operator on users page, got %d",
			resp.StatusCode)
	}

	adminLoginForm := url.Values{
		"username": {"admin"},
		"password": {"AdminPass123!"},
	}
	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/login",
		strings.NewReader(adminLoginForm.Encode()),
		"application/x-www-form-urlencoded",
	)
	adminCookie := findSessionCookie(resp)
	if adminCookie == nil {
		t.Fatal("expected admin cookie")
	}

	resp, body := doHTTPReq(
		t,
		client,
		http.MethodGet,
		baseURL+"/settings/users",
		nil,
		"",
		adminCookie,
	)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for admin on users page, got %d",
			resp.StatusCode)
	}
	if !strings.Contains(body, "User Management") {
		t.Fatal("expected User Management on admin page")
	}

	return adminCookie
}

func testSettingsUsersAdminCRUD(
	t *testing.T,
	client *http.Client,
	baseURL string,
	authStore *storage.SQLiteAuthStore,
	adminCookie *http.Cookie,
) {
	t.Helper()
	testAdminCreateUser(t, client, baseURL, authStore, adminCookie)
	testAdminResetUserPassword(t, client, baseURL, adminCookie)
	testAdminUserStatusToggle(t, client, baseURL, authStore, adminCookie)
	testAdminUserUpdate(t, client, baseURL, authStore, adminCookie)
	testAdminUserSelfGuards(t, client, baseURL, adminCookie)
	testAdminRevokeDevice(t, client, baseURL, authStore, adminCookie)
	testAdminUserDelete(t, client, baseURL, authStore, adminCookie)
}

func testAdminCreateUser(
	t *testing.T,
	client *http.Client,
	baseURL string,
	authStore *storage.SQLiteAuthStore,
	adminCookie *http.Cookie,
) {
	t.Helper()
	createUserForm := url.Values{
		"username":             {"carol"},
		"password":             {"CarolPass123!"},
		"role":                 {"operator"},
		"must_change_password": {"true"},
	}
	resp, _ := doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/users/create",
		strings.NewReader(createUserForm.Encode()),
		"application/x-www-form-urlencoded",
		adminCookie,
	)
	if resp.StatusCode != http.StatusFound ||
		!strings.Contains(resp.Header.Get("Location"), "msg=") {
		t.Fatalf("expected redirect with msg on user create, got %d loc %q",
			resp.StatusCode, resp.Header.Get("Location"))
	}
	carol, cErr := authStore.GetUser(context.Background(), "carol")
	if cErr != nil || carol == nil {
		t.Fatalf("expected user carol to exist in store: %v", cErr)
	}
	if !carol.MustChangePassword {
		t.Fatal("expected carol.MustChangePassword to be true")
	}
}

func testAdminResetUserPassword(
	t *testing.T,
	client *http.Client,
	baseURL string,
	adminCookie *http.Cookie,
) {
	t.Helper()
	resetForm := url.Values{
		"username":             {"carol"},
		"new_password":         {"CarolReset123!"},
		"must_change_password": {"true"},
	}
	resp, _ := doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/users/reset-password",
		strings.NewReader(resetForm.Encode()),
		"application/x-www-form-urlencoded",
		adminCookie,
	)
	if resp.StatusCode != http.StatusFound ||
		!strings.Contains(resp.Header.Get("Location"), "msg=") {
		t.Fatalf("expected redirect with msg on reset password, got %d loc %q",
			resp.StatusCode, resp.Header.Get("Location"))
	}
}

func testAdminUserStatusToggle(
	t *testing.T,
	client *http.Client,
	baseURL string,
	authStore *storage.SQLiteAuthStore,
	adminCookie *http.Cookie,
) {
	t.Helper()
	deactForm := url.Values{
		"username": {"carol"},
		"active":   {"false"},
	}
	resp, _ := doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/users/status",
		strings.NewReader(deactForm.Encode()),
		"application/x-www-form-urlencoded",
		adminCookie,
	)
	if resp.StatusCode != http.StatusFound ||
		!strings.Contains(resp.Header.Get("Location"), "msg=") {
		t.Fatalf("expected redirect on deactivation, got %d loc %q",
			resp.StatusCode, resp.Header.Get("Location"))
	}
	carolDeact, gErr := authStore.GetUser(context.Background(), "carol")
	if gErr != nil || carolDeact.IsActive {
		t.Fatal("expected carol to be deactivated")
	}

	deactLogin := url.Values{
		"username": {"carol"},
		"password": {"CarolReset123!"},
	}
	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/login",
		strings.NewReader(deactLogin.Encode()),
		"application/x-www-form-urlencoded",
	)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for deactivated user login, got %d",
			resp.StatusCode)
	}

	reactForm := url.Values{
		"username": {"carol"},
		"active":   {"true"},
	}
	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/users/status",
		strings.NewReader(reactForm.Encode()),
		"application/x-www-form-urlencoded",
		adminCookie,
	)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected redirect on reactivate, got %d", resp.StatusCode)
	}
}

func testAdminUserUpdate(
	t *testing.T,
	client *http.Client,
	baseURL string,
	authStore *storage.SQLiteAuthStore,
	adminCookie *http.Cookie,
) {
	t.Helper()
	updateForm := url.Values{
		"username":             {"carol"},
		"role":                 {"admin"},
		"active":               {"true"},
		"must_change_password": {"false"},
	}
	resp, _ := doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/users/update",
		strings.NewReader(updateForm.Encode()),
		"application/x-www-form-urlencoded",
		adminCookie,
	)
	if resp.StatusCode != http.StatusFound ||
		!strings.Contains(resp.Header.Get("Location"), "msg=") {
		t.Fatalf("expected redirect on user update, got %d loc %q",
			resp.StatusCode, resp.Header.Get("Location"))
	}
	carol, err := authStore.GetUser(context.Background(), "carol")
	if err != nil || carol == nil {
		t.Fatalf("expected carol in store: %v", err)
	}
	if carol.Role != auth.RoleAdmin {
		t.Fatalf("expected carol role admin, got %s", carol.Role)
	}
	if carol.MustChangePassword {
		t.Fatal("expected carol.MustChangePassword to be false")
	}
}

func testAdminUserSelfGuards(
	t *testing.T,
	client *http.Client,
	baseURL string,
	adminCookie *http.Cookie,
) {
	t.Helper()
	selfDeact := url.Values{
		"username": {"admin"},
		"active":   {"false"},
	}
	resp, _ := doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/users/status",
		strings.NewReader(selfDeact.Encode()),
		"application/x-www-form-urlencoded",
		adminCookie,
	)
	if !strings.Contains(resp.Header.Get("Location"), "error=") {
		t.Fatal("expected error on self-deactivation attempt")
	}

	selfDemote := url.Values{
		"username": {"admin"},
		"role":     {"operator"},
		"active":   {"true"},
	}
	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/users/update",
		strings.NewReader(selfDemote.Encode()),
		"application/x-www-form-urlencoded",
		adminCookie,
	)
	if !strings.Contains(resp.Header.Get("Location"), "error=") {
		t.Fatal("expected error on self-demote attempt")
	}

	selfUpdateDeact := url.Values{
		"username": {"admin"},
		"role":     {"admin"},
	}
	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/users/update",
		strings.NewReader(selfUpdateDeact.Encode()),
		"application/x-www-form-urlencoded",
		adminCookie,
	)
	if !strings.Contains(resp.Header.Get("Location"), "error=") {
		t.Fatal("expected error on self-deactivate attempt via update")
	}

	selfDel := url.Values{
		"username": {"admin"},
	}
	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/users/delete",
		strings.NewReader(selfDel.Encode()),
		"application/x-www-form-urlencoded",
		adminCookie,
	)
	if !strings.Contains(resp.Header.Get("Location"), "error=") {
		t.Fatal("expected error on self-delete attempt")
	}
}

func testAdminRevokeDevice(
	t *testing.T,
	client *http.Client,
	baseURL string,
	authStore *storage.SQLiteAuthStore,
	adminCookie *http.Cookie,
) {
	t.Helper()
	now := time.Now().UTC()
	dev := &auth.TOTPDevice{
		ID:              "dev-carol-1",
		Username:        "carol",
		Name:            "Carol Key",
		EncryptedSecret: "fake-encrypted-secret",
		CreatedAt:       now,
		LastUsedAt:      &now,
		Enabled:         true,
	}
	if err := authStore.CreateDevice(
		context.Background(),
		dev,
	); err != nil {
		t.Fatalf("creating carol device: %v", err)
	}

	resp, body := doHTTPReq(
		t,
		client,
		http.MethodGet,
		baseURL+"/settings/users?inspect=carol",
		nil,
		"",
		adminCookie,
	)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf(
			"expected 200 on inspect users, got %d",
			resp.StatusCode,
		)
	}
	if !strings.Contains(body, "Carol Key") {
		t.Fatal("expected device name Carol Key in inspect view")
	}

	revokeForm := url.Values{
		"username":  {"carol"},
		"device_id": {"dev-carol-1"},
	}
	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/users/revoke-device",
		strings.NewReader(revokeForm.Encode()),
		"application/x-www-form-urlencoded",
		adminCookie,
	)
	if resp.StatusCode != http.StatusFound ||
		!strings.Contains(resp.Header.Get("Location"), "msg=") {
		t.Fatalf("expected redirect with msg on revoke, got %d loc %q",
			resp.StatusCode, resp.Header.Get("Location"))
	}

	devs, lErr := authStore.ListUserDevices(context.Background(), "carol")
	if lErr != nil {
		t.Fatalf("listing devices: %v", lErr)
	}
	if len(devs) != 0 {
		t.Fatalf(
			"expected 0 active devices after revocation, got %d",
			len(devs),
		)
	}
}

func testAdminUserDelete(
	t *testing.T,
	client *http.Client,
	baseURL string,
	authStore *storage.SQLiteAuthStore,
	adminCookie *http.Cookie,
) {
	t.Helper()
	delForm := url.Values{
		"username": {"carol"},
	}
	resp, _ := doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/users/delete",
		strings.NewReader(delForm.Encode()),
		"application/x-www-form-urlencoded",
		adminCookie,
	)
	if resp.StatusCode != http.StatusFound ||
		!strings.Contains(resp.Header.Get("Location"), "msg=") {
		t.Fatalf("expected redirect on user delete, got %d loc %q",
			resp.StatusCode, resp.Header.Get("Location"))
	}
	_, dErr := authStore.GetUser(context.Background(), "carol")
	if dErr == nil {
		t.Fatal("expected carol to be deleted from store")
	}
}

func TestAuthWeb_SettingsTokens(t *testing.T) {
	t.Parallel()
	_, baseURL, _, _ := setupPasswordLifecycleServer(
		t,
		config.PasswordPolicyConfig{
			RequireChangeOnFirstLogin: false,
			MinLength:                 8,
		},
	)

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. Unauthenticated GET /settings/tokens redirects to /login
	unauthResp, _ := doHTTPReq(
		t,
		client,
		http.MethodGet,
		baseURL+"/settings/tokens",
		nil,
		"",
	)
	if unauthResp.StatusCode != http.StatusFound {
		t.Fatalf(
			"expected 302 for unauth tokens page, got %d",
			unauthResp.StatusCode,
		)
	}
	if !strings.Contains(unauthResp.Header.Get("Location"), "/login") {
		t.Fatalf(
			"expected redirect to /login, got %q",
			unauthResp.Header.Get("Location"),
		)
	}

	// 2. Login as admin
	loginForm := url.Values{
		"username": {"admin"},
		"password": {"AdminPass123!"},
	}
	loginResp, _ := doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/login",
		strings.NewReader(loginForm.Encode()),
		"application/x-www-form-urlencoded",
	)
	if loginResp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 login, got %d", loginResp.StatusCode)
	}
	adminCookie := findSessionCookie(loginResp)
	if adminCookie == nil {
		t.Fatal("expected admin cookie")
	}

	// 3. Authenticated GET /settings/tokens -> 200 OK
	getResp, getBody := doHTTPReq(
		t,
		client,
		http.MethodGet,
		baseURL+"/settings/tokens",
		nil,
		"",
		adminCookie,
	)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf(
			"expected 200 OK for tokens page, got %d",
			getResp.StatusCode,
		)
	}
	if !strings.Contains(getBody, "Personal Access Tokens") ||
		!strings.Contains(
			getBody,
			"Dual-Transport Security Architecture",
		) {
		t.Fatal("expected tokens page content in body")
	}

	// 4. POST /settings/tokens/create
	createForm := url.Values{
		"name":       {"ci-pipeline"},
		"role":       {"operator"},
		"expires_in": {"720h"},
	}
	createResp, createBody := doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/tokens/create",
		strings.NewReader(createForm.Encode()),
		"application/x-www-form-urlencoded",
		adminCookie,
	)
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf(
			"expected 200 OK on token create, got %d",
			createResp.StatusCode,
		)
	}
	if !strings.Contains(createBody, "cb_pat_") ||
		!strings.Contains(
			createBody,
			"Copy Your New Personal Access Token",
		) {
		t.Fatal("expected raw token in create response body")
	}
}

func TestAuthAPI_TokensAndDualTransport(t *testing.T) {
	t.Parallel()
	srv, baseURL, _, _ := setupPasswordLifecycleServer(
		t,
		config.PasswordPolicyConfig{
			RequireChangeOnFirstLogin: false,
			MinLength:                 8,
		},
	)

	tcpClient := &http.Client{Timeout: 5 * time.Second}
	sockPath := srv.SocketPath()
	unixClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(
				ctx context.Context,
				_, _ string,
			) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", sockPath)
			},
		},
		Timeout: 5 * time.Second,
	}

	testAPIDualTransportTCPUnauth(t, tcpClient, baseURL)
	testAPIDualTransportUnixSocket(t, unixClient)
	rawToken, tokenID := testAPIDualTransportUnixCreate(t, unixClient)
	testAPIBodyLimits(t, tcpClient, baseURL, rawToken)
	testAPIDualTransportTCPBearer(t, tcpClient, baseURL, rawToken, tokenID)
}

func testAPIBodyLimits(
	t *testing.T,
	client *http.Client,
	baseURL string,
	rawToken string,
) {
	t.Helper()
	getTokenCount := func() int {
		t.Helper()
		req, err := http.NewRequestWithContext(
			context.Background(),
			http.MethodGet,
			baseURL+"/api/v1/tokens",
			nil,
		)
		if err != nil {
			t.Fatalf("creating token-list request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+rawToken)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("listing tokens: %v", err)
		}
		defer closeBody(t, resp)
		var tokenList []tokens.APIToken
		if err := json.NewDecoder(resp.Body).Decode(&tokenList); err != nil {
			t.Fatalf("decoding token list: %v", err)
		}
		return len(tokenList)
	}
	tokenCount := getTokenCount()
	oversized := `{"username":"alice","password":"correct-horse"}` +
		strings.Repeat(" ", (1<<20)+1)
	for _, endpoint := range []struct {
		path  string
		token string
	}{
		{path: "/api/v1/auth/login"},
		{path: "/api/v1/tokens", token: rawToken},
	} {
		req, err := http.NewRequestWithContext(
			context.Background(),
			http.MethodPost,
			baseURL+endpoint.path,
			strings.NewReader(oversized),
		)
		if err != nil {
			t.Fatalf("creating oversized request for %s: %v", endpoint.path, err)
		}
		req.Header.Set("Content-Type", "application/json")
		if endpoint.token != "" {
			req.Header.Set("Authorization", "Bearer "+endpoint.token)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("sending oversized request to %s: %v", endpoint.path, err)
		}
		closeBody(t, resp)
		if resp.StatusCode != http.StatusRequestEntityTooLarge {
			t.Errorf("%s status = %d, want 413", endpoint.path, resp.StatusCode)
		}
	}
	if got := getTokenCount(); got != tokenCount {
		t.Errorf("oversized token request changed token count: %d -> %d",
			tokenCount, got)
	}
}

func testAPIDualTransportTCPUnauth(
	t *testing.T,
	client *http.Client,
	baseURL string,
) {
	t.Helper()
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		baseURL+"/api/v1/tokens",
		nil,
	)
	if err != nil {
		t.Fatalf("creating unauth request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("tcp unauth request: %v", err)
	}
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf(
			"expected 401 for unauth TCP, got %d",
			resp.StatusCode,
		)
	}
}

func testAPIDualTransportUnixSocket(t *testing.T, client *http.Client) {
	t.Helper()
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		"http://unix/api/v1/tokens",
		nil,
	)
	if err != nil {
		t.Fatalf("creating unix socket request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unix socket request: %v", err)
	}
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf(
			"expected 200 OK over socket without credentials: %d",
			resp.StatusCode,
		)
	}
}

func testAPIDualTransportUnixCreate(
	t *testing.T,
	client *http.Client,
) (string, string) {
	t.Helper()
	createPayload := strings.NewReader(`{
		"name": "remote-workstation",
		"username": "admin",
		"role": "admin",
		"expires_in": "720h"
	}`)
	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"http://unix/api/v1/tokens",
		createPayload,
	)
	if err != nil {
		t.Fatalf("creating unix token create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("creating token over unix socket: %v", err)
	}
	defer closeBody(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", resp.StatusCode)
	}

	var createRes tokens.CreateTokenResult
	if dErr := json.NewDecoder(resp.Body).Decode(&createRes); dErr != nil {
		t.Fatalf("decoding token create result: %v", dErr)
	}
	if !strings.HasPrefix(createRes.RawToken, "cb_pat_") {
		t.Fatalf("expected cb_pat_ prefix, got %q", createRes.RawToken)
	}
	return createRes.RawToken, createRes.Token.ID
}

func testAPIDualTransportTCPBearer(
	t *testing.T,
	client *http.Client,
	baseURL string,
	rawToken string,
	tokenID string,
) {
	t.Helper()
	// 1. Authenticate over TCP using Authorization: Bearer cb_pat_...
	authReq, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		baseURL+"/api/v1/tokens",
		nil,
	)
	if err != nil {
		t.Fatalf("creating auth TCP request: %v", err)
	}
	authReq.Header.Set("Authorization", "Bearer "+rawToken)
	authResp, err := client.Do(authReq)
	if err != nil {
		t.Fatalf("authenticated TCP request: %v", err)
	}
	defer closeBody(t, authResp)
	if authResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for bearer PAT auth, got %d",
			authResp.StatusCode)
	}

	// 2. Access protected /operator over TCP using Bearer cb_pat_...
	opReq, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		baseURL+"/operator",
		nil,
	)
	if err != nil {
		t.Fatalf("creating op request: %v", err)
	}
	opReq.Header.Set("Authorization", "Bearer "+rawToken)
	opResp, err := client.Do(opReq)
	if err != nil {
		t.Fatalf("bearer auth to /operator: %v", err)
	}
	defer closeBody(t, opResp)
	if opResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for /operator with PAT, got %d",
			opResp.StatusCode)
	}

	// 3. Revoke token via DELETE /api/v1/tokens?id=... over TCP
	delReq, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodDelete,
		baseURL+"/api/v1/tokens?id="+tokenID,
		nil,
	)
	if err != nil {
		t.Fatalf("creating del request: %v", err)
	}
	delReq.Header.Set("Authorization", "Bearer "+rawToken)
	delResp, err := client.Do(delReq)
	if err != nil {
		t.Fatalf("delete token request: %v", err)
	}
	defer closeBody(t, delResp)
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 No Content for delete, got %d",
			delResp.StatusCode)
	}

	// 4. Requesting over TCP with the revoked token must now return 401
	revokedReq, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		baseURL+"/api/v1/tokens",
		nil,
	)
	if err != nil {
		t.Fatalf("creating revoked request: %v", err)
	}
	revokedReq.Header.Set("Authorization", "Bearer "+rawToken)
	revokedResp, err := client.Do(revokedReq)
	if err != nil {
		t.Fatalf("revoked TCP request: %v", err)
	}
	defer closeBody(t, revokedResp)
	if revokedResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for revoked token, got %d",
			revokedResp.StatusCode)
	}
}

func setupFreshServerWithoutUsers(
	t *testing.T,
	policy config.PasswordPolicyConfig,
) (*server.HTTPServer, string, *storage.SQLiteAuthStore) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "fresh.db")
	sockPath := filepath.Join(dir, "fresh.sock")

	engine, err := storage.NewSQLiteAdapter(
		context.Background(),
		config.StorageConfig{
			DatabasePath:  dbPath,
			WALMode:       false,
			BusyTimeoutMs: 5000,
		},
	)
	if err != nil {
		t.Fatalf("opening sqlite engine: %v", err)
	}

	if mErr := engine.Migrate(context.Background()); mErr != nil {
		t.Fatalf("running initial migration: %v", mErr)
	}

	authStore := storage.NewSQLiteAuthStore(engine)
	expiry, pErr := policy.ExpiryDuration()
	if pErr != nil {
		t.Fatalf("parsing policy duration: %v", pErr)
	}
	sqliteAuth := auth.NewSQLiteAuthenticator(
		authStore,
	).WithPasswordExpiry(expiry)

	crypter := auth.NewPlaintextCrypter()
	cmdSvc := auth.NewCommandService(
		sqliteAuth,
		authStore,
		authStore,
		authStore,
		authStore,
		authStore,
		authStore,
		crypter,
		false,
	)
	querySvc := auth.NewQueryService(
		authStore,
		authStore,
		authStore,
		authStore,
	)

	srv, err := server.NewServer(server.Config{
		HTTPBind:       "127.0.0.1",
		HTTPPort:       0,
		SocketPath:     sockPath,
		DaemonMode:     "standalone",
		Version:        "v0.1.0-setuptest",
		PasswordPolicy: policy,
	}, server.Dependencies{
		Engine:       engine,
		QueryService: storage.NewQueryService(engine),
		AuthCommands: cmdSvc,
		AuthQueries:  querySvc,
		TokenService: tokens.NewTokenService(
			storage.NewSQLiteTokenStore(engine),
			authStore,
		),
	})
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}

	if sErr := srv.Start(context.Background()); sErr != nil {
		t.Fatalf("starting server: %v", sErr)
	}

	t.Cleanup(func() {
		if cErr := srv.Shutdown(context.Background()); cErr != nil {
			t.Logf("shutdown server: %v", cErr)
		}
		if cErr := engine.Close(); cErr != nil {
			t.Logf("close engine: %v", cErr)
		}
	})

	return srv, "http://" + srv.HTTPAddr(), authStore
}

func TestAuthWeb_FirstBootSetup_Redirects(t *testing.T) {
	t.Parallel()
	_, baseURL, _ := setupFreshServerWithoutUsers(
		t,
		config.PasswordPolicyConfig{MinLength: 8},
	)

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. Fresh install: GET / redirects to /setup
	resp, _ := doHTTPReq(t, client, http.MethodGet, baseURL+"/", nil, "")
	if resp.StatusCode != http.StatusFound ||
		resp.Header.Get("Location") != "/setup" {
		t.Fatalf(
			"expected redirect to /setup from /, got %d loc %q",
			resp.StatusCode,
			resp.Header.Get("Location"),
		)
	}

	// 2. Fresh install: GET /login redirects to /setup
	resp, _ = doHTTPReq(t, client, http.MethodGet, baseURL+"/login", nil, "")
	if resp.StatusCode != http.StatusFound ||
		resp.Header.Get("Location") != "/setup" {
		t.Fatalf(
			"expected redirect to /setup from /login, got %d loc %q",
			resp.StatusCode,
			resp.Header.Get("Location"),
		)
	}

	// 3. Fresh install: GET /operator redirects to /setup
	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodGet,
		baseURL+"/operator",
		nil,
		"",
	)
	if resp.StatusCode != http.StatusFound ||
		resp.Header.Get("Location") != "/setup" {
		t.Fatalf(
			"expected redirect to /setup from /operator, got %d loc %q",
			resp.StatusCode,
			resp.Header.Get("Location"),
		)
	}

	// 4. GET /setup renders setup page (200 OK)
	resp, body := doHTTPReq(
		t,
		client,
		http.MethodGet,
		baseURL+"/setup",
		nil,
		"",
	)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /setup, got %d", resp.StatusCode)
	}
	if !strings.Contains(body, "Create Admin Account") ||
		!strings.Contains(body, "SINGLE-RUN ONLY") {
		t.Fatalf("expected setup page content in body")
	}
}

func TestAuthWeb_FirstBootSetup_WebSubmissionAndBarrier(t *testing.T) {
	t.Parallel()
	_, baseURL, authStore := setupFreshServerWithoutUsers(
		t,
		config.PasswordPolicyConfig{MinLength: 8},
	)

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. POST /setup with validation error (password mismatch)
	badForm := url.Values{
		"username":         {"admin"},
		"password":         {"Pass123456!"},
		"confirm_password": {"DifferentPass123!"},
	}
	resp, body := doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/setup",
		strings.NewReader(badForm.Encode()),
		"application/x-www-form-urlencoded",
	)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 on password mismatch, got %d", resp.StatusCode)
	}
	if !strings.Contains(body, "passwords do not match") {
		t.Fatalf("expected error message in body, got: %s", body)
	}

	// 2. Successful POST /setup
	goodForm := url.Values{
		"username":         {"sysadmin"},
		"password":         {"SuperSecret123!"},
		"confirm_password": {"SuperSecret123!"},
	}
	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/setup",
		strings.NewReader(goodForm.Encode()),
		"application/x-www-form-urlencoded",
	)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected 302 after setup, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "/operator" {
		t.Fatalf(
			"expected redirect to /operator, got %q",
			resp.Header.Get("Location"),
		)
	}
	adminCookie := findSessionCookie(resp)
	if adminCookie == nil {
		t.Fatal("expected session cookie after setup")
	}

	// 3. Single-run barrier: subsequent GET /setup redirects to /login
	resp, _ = doHTTPReq(t, client, http.MethodGet, baseURL+"/setup", nil, "")
	if resp.StatusCode != http.StatusFound ||
		resp.Header.Get("Location") != "/login" {
		t.Fatalf(
			"expected redirect to /login for completed setup, got %d loc %q",
			resp.StatusCode,
			resp.Header.Get("Location"),
		)
	}

	// 4. Single-run barrier: subsequent POST /setup returns 403 Forbidden
	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/setup",
		strings.NewReader(goodForm.Encode()),
		"application/x-www-form-urlencoded",
	)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf(
			"expected 403 Forbidden on second setup POST, got %d",
			resp.StatusCode,
		)
	}

	// 5. Verify user in DB
	count, cErr := authStore.CountUsers(context.Background())
	if cErr != nil || count != 1 {
		t.Fatalf(
			"expected exactly 1 user in db, got %d (err: %v)",
			count,
			cErr,
		)
	}
}

func TestAuthWeb_FirstBootSetup_APISetupAndBarrier(t *testing.T) {
	t.Parallel()
	_, baseURL, authStore := setupFreshServerWithoutUsers(
		t,
		config.PasswordPolicyConfig{MinLength: 8},
	)

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// 1. GET /api/v1/setup returns needs_setup: true
	resp, body := doHTTPReq(
		t,
		client,
		http.MethodGet,
		baseURL+"/api/v1/setup",
		nil,
		"",
	)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /api/v1/setup, got %d", resp.StatusCode)
	}
	if !strings.Contains(body, `"needs_setup":true`) {
		t.Fatalf("expected needs_setup:true in body, got: %s", body)
	}

	// 2. Successful POST /api/v1/setup
	setupJSON := `{"username":"apiadmin","password":"SuperSecret123!",` +
		`"confirm_password":"SuperSecret123!"}`
	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/api/v1/setup",
		strings.NewReader(setupJSON),
		"application/json",
	)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 on API setup, got %d", resp.StatusCode)
	}

	// 3. GET /api/v1/setup returns needs_setup: false
	resp, body = doHTTPReq(
		t,
		client,
		http.MethodGet,
		baseURL+"/api/v1/setup",
		nil,
		"",
	)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /api/v1/setup, got %d", resp.StatusCode)
	}
	if !strings.Contains(body, `"needs_setup":false`) {
		t.Fatalf("expected needs_setup:false in body, got: %s", body)
	}

	// 4. Subsequent POST /api/v1/setup returns 403 Forbidden
	resp, _ = doHTTPReq(
		t,
		client,
		http.MethodPost,
		baseURL+"/api/v1/setup",
		strings.NewReader(`{"username":"hacker","password":"Password123!"}`),
		"application/json",
	)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf(
			"expected 403 on API setup after completion, got %d",
			resp.StatusCode,
		)
	}

	// 5. Verify user in DB
	count, cErr := authStore.CountUsers(context.Background())
	if cErr != nil || count != 1 {
		t.Fatalf(
			"expected exactly 1 user in db, got %d (err: %v)",
			count,
			cErr,
		)
	}
}
