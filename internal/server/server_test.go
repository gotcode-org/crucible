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

	srv, err := server.NewServer(srvCfg, engine, nil, nil, nil)
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
	if health.Database.MigrationsApplied != 4 {
		t.Errorf(
			"expected 4 migrations applied, got %d",
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
	}, engine, nil, nil, nil)
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

	authStore := auth.NewSQLiteStore(engine.DB())
	crypter := auth.NewPlaintextCrypter()
	cmdSvc := auth.NewCommandService(
		memAuth,
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
	}, engine, nil, cmdSvc, querySvc)
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

	resp, err := client.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("get / unauthenticated: %v", err)
	}
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected 302 redirect to /login, got %d", resp.StatusCode)
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
	req, rErr := http.NewRequest(http.MethodGet, baseURL+"/", nil)
	if rErr != nil {
		t.Fatalf("creating req: %v", rErr)
	}
	req.AddCookie(sessionCookie)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get dashboard: %v", err)
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
	authStore := auth.NewSQLiteStore(engine.DB())
	crypter := auth.NewPlaintextCrypter()
	cmdSvc := auth.NewCommandService(
		memAuth,
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
	}, engine, nil, cmdSvc, querySvc)
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
