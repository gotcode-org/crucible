// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gotcode.org/crucible/internal/auth"
	"gotcode.org/crucible/internal/storage"
	"gotcode.org/crucible/internal/tokens"
	"gotcode.org/crucible/web"
)

// SessionCookieName defines the standard name of the session cookie.
const SessionCookieName = "crucible_session"

// HTTPServer implements the Server interface hosting the embedded web shell,
// metrics, and JSON health endpoints over dual TCP and UNIX domain sockets.
type HTTPServer struct {
	cfg          Config
	engine       storage.Engine
	querySvc     storage.QueryHandler
	authCmd      auth.CommandHandler
	authQuery    auth.QueryHandler
	tokenSvc     tokens.Service
	mux          *http.ServeMux
	tcpListener  net.Listener
	unixListener net.Listener
	tcpServer    *http.Server
	unixServer   *http.Server
	tcpAddr      string
	startedAt    time.Time
	running      bool
	mu           sync.Mutex
	wg           sync.WaitGroup
	errMu        sync.Mutex
	errs         []error
}

// Dependencies lists the application and storage ports required by the HTTP
// adapter. Concrete implementations are selected by the composition root.
type Dependencies struct {
	Engine       storage.Engine
	QueryService storage.QueryHandler
	// Auth handlers may be nil when authentication is not configured.
	AuthCommands auth.CommandHandler
	AuthQueries  auth.QueryHandler
	TokenService tokens.Service
}

// NewServer initializes a new HTTPServer with embedded web routes and static
// asset handlers.
func NewServer(
	cfg Config,
	deps Dependencies,
) (*HTTPServer, error) {
	if cfg.HTTPBind == "" {
		cfg.HTTPBind = "127.0.0.1"
	}
	if cfg.DaemonMode == "" {
		cfg.DaemonMode = "standalone"
	}
	if cfg.Version == "" {
		cfg.Version = "v0.1.0"
	}

	if deps.Engine == nil || deps.QueryService == nil ||
		deps.TokenService == nil {
		return nil, errors.New(
			"server engine, query service, and token service are required",
		)
	}
	if cfg.AuthProvider != "" &&
		(deps.AuthCommands == nil || deps.AuthQueries == nil) {
		return nil, errors.New(
			"authentication command and query services are required " +
				"when authentication is configured",
		)
	}

	s := &HTTPServer{
		cfg:       cfg,
		engine:    deps.Engine,
		querySvc:  deps.QueryService,
		authCmd:   deps.AuthCommands,
		authQuery: deps.AuthQueries,
		tokenSvc:  deps.TokenService,
		mux:       http.NewServeMux(),
	}

	if err := s.setupRoutes(); err != nil {
		return nil, fmt.Errorf("registering server routes: %w", err)
	}

	return s, nil
}

func (s *HTTPServer) setupRoutes() error {
	staticFS, err := fs.Sub(web.FS, "static")
	if err != nil {
		return fmt.Errorf("creating static sub filesystem: %w", err)
	}

	s.mux.Handle(
		"/static/",
		http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))),
	)
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc("/setup", s.handleSetup)
	s.mux.HandleFunc("/login", s.handleLogin)
	s.mux.HandleFunc("/auth/2fa", s.handle2FA)
	s.mux.HandleFunc("/logout", s.handleLogout)
	s.mux.HandleFunc("/settings/2fa", s.handleSettings2FA)
	s.mux.HandleFunc("/settings/2fa/enroll", s.handleSettings2FAEnroll)
	s.mux.HandleFunc("/settings/2fa/revoke", s.handleSettings2FARevoke)
	s.mux.HandleFunc("/settings/2fa/invite", s.handleSettings2FAInvite)
	s.mux.HandleFunc("/settings/password", s.handleSettingsPassword)
	s.mux.HandleFunc("/settings/users", s.handleSettingsUsers)
	s.mux.HandleFunc("/settings/users/create", s.handleSettingsUsersCreate)
	s.mux.HandleFunc("/settings/users/update", s.handleSettingsUsersUpdate)
	s.mux.HandleFunc(
		"/settings/users/reset-password",
		s.handleSettingsUsersResetPassword,
	)
	s.mux.HandleFunc("/settings/users/status", s.handleSettingsUsersStatus)
	s.mux.HandleFunc(
		"/settings/users/revoke-device",
		s.handleSettingsUsersRevokeDevice,
	)
	s.mux.HandleFunc("/settings/users/delete", s.handleSettingsUsersDelete)
	s.mux.HandleFunc("/settings/tokens", s.handleSettingsTokens)
	s.mux.HandleFunc(
		"/settings/tokens/create",
		s.handleSettingsTokensCreate,
	)
	s.mux.HandleFunc(
		"/settings/tokens/revoke",
		s.handleSettingsTokensRevoke,
	)

	// JSON API endpoints
	s.mux.HandleFunc("/api/v1/setup", s.handleAPISetup)
	s.mux.HandleFunc("/api/v1/tokens", s.handleAPITokens)
	s.mux.HandleFunc("/api/v1/auth/login", s.handleAPILogin)
	s.mux.HandleFunc("/api/v1/auth/verify-2fa", s.handleAPIVerify2FA)
	s.mux.HandleFunc("/api/v1/auth/devices", s.handleAPIDevices)
	s.mux.HandleFunc("/api/v1/auth/invite", s.handleAPIInvite)
	s.mux.HandleFunc("/api/v1/auth/session", s.handleAPISession)

	s.mux.HandleFunc("/operator", s.handleOperatorDashboard)
	s.mux.HandleFunc("/", s.handlePublicDashboard)

	return nil
}

// Start binds listeners and starts serving requests in background goroutines.
func (s *HTTPServer) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errors.New("server is already running")
	}
	s.running = true
	s.startedAt = time.Now().UTC()
	s.mu.Unlock()

	if err := s.initTCPListener(); err != nil {
		return err
	}

	if err := s.initUnixListener(); err != nil {
		if s.tcpListener != nil {
			if closeErr := s.tcpListener.Close(); closeErr != nil {
				s.recordError(closeErr)
			}
		}
		return err
	}

	s.initHTTPServers()
	s.spawnWorkers()

	// Drain when root context completes
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()
		if err := s.Shutdown(shutdownCtx); err != nil {
			s.recordError(err)
		}
	}()

	return nil
}

func (s *HTTPServer) initTCPListener() error {
	bindAddr := fmt.Sprintf("%s:%d", s.cfg.HTTPBind, s.cfg.HTTPPort)
	l, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return fmt.Errorf("listening on tcp %s: %w", bindAddr, err)
	}
	s.tcpListener = l
	s.tcpAddr = l.Addr().String()
	return nil
}

func (s *HTTPServer) initUnixListener() error {
	if s.cfg.SocketPath == "" {
		return nil
	}

	dir := filepath.Dir(s.cfg.SocketPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("creating socket directory %s: %w", dir, err)
	}

	if err := s.cleanStaleSocket(); err != nil {
		return err
	}

	l, err := net.Listen("unix", s.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf(
			"listening on unix socket %s: %w",
			s.cfg.SocketPath,
			err,
		)
	}
	s.unixListener = l

	mode := s.cfg.SocketMode
	if mode == 0 {
		mode = 0660
	}
	if err := os.Chmod(s.cfg.SocketPath, mode); err != nil {
		if closeErr := l.Close(); closeErr != nil {
			s.recordError(closeErr)
		}
		return fmt.Errorf(
			"setting permissions on socket %s: %w",
			s.cfg.SocketPath,
			err,
		)
	}

	return nil
}

func (s *HTTPServer) cleanStaleSocket() error {
	if _, err := os.Stat(s.cfg.SocketPath); err != nil {
		return nil
	}

	conn, dialErr := net.DialTimeout(
		"unix",
		s.cfg.SocketPath,
		100*time.Millisecond,
	)
	if dialErr == nil {
		if closeErr := conn.Close(); closeErr != nil {
			s.recordError(closeErr)
		}
		return fmt.Errorf(
			"socket %s is already in use by active process",
			s.cfg.SocketPath,
		)
	}

	if rmErr := os.Remove(s.cfg.SocketPath); rmErr != nil {
		return fmt.Errorf(
			"removing stale socket %s: %w",
			s.cfg.SocketPath,
			rmErr,
		)
	}

	return nil
}

func (s *HTTPServer) initHTTPServers() {
	s.tcpServer = &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	if s.unixListener != nil {
		s.unixServer = &http.Server{
			Handler:           s.mux,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       15 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
			BaseContext: func(_ net.Listener) context.Context {
				return context.WithValue(
					context.Background(),
					contextKeyIsUnixSocket,
					true,
				)
			},
		}
	}
}

func (s *HTTPServer) spawnWorkers() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		err := s.tcpServer.Serve(s.tcpListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.recordError(fmt.Errorf("tcp serve error: %w", err))
		}
	}()

	if s.unixListener != nil && s.unixServer != nil {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			err := s.unixServer.Serve(s.unixListener)
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				s.recordError(fmt.Errorf("unix serve error: %w", err))
			}
		}()
	}
}

// Shutdown stops the servers gracefully, draining active connections.
func (s *HTTPServer) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	var errs []error
	if s.tcpServer != nil {
		if err := s.tcpServer.Shutdown(ctx); err != nil {
			errs = append(
				errs,
				fmt.Errorf("shutting down tcp server: %w", err),
			)
		}
	}

	if s.unixServer != nil {
		if err := s.unixServer.Shutdown(ctx); err != nil {
			errs = append(
				errs,
				fmt.Errorf("shutting down unix server: %w", err),
			)
		}
	}

	s.wg.Wait()

	if s.cfg.SocketPath != "" {
		if err := os.Remove(s.cfg.SocketPath); err != nil &&
			!os.IsNotExist(err) {
			errs = append(
				errs,
				fmt.Errorf("removing socket %s: %w", s.cfg.SocketPath, err),
			)
		}
	}

	return errors.Join(errs...)
}

// HTTPAddr returns the bound TCP host:port string.
func (s *HTTPServer) HTTPAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tcpAddr
}

// SocketPath returns the UNIX domain socket path.
func (s *HTTPServer) SocketPath() string {
	return s.cfg.SocketPath
}

func (s *HTTPServer) effectivePort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tcpListener != nil {
		if tcpAddr, ok := s.tcpListener.Addr().(*net.TCPAddr); ok {
			return tcpAddr.Port
		}
	}
	return s.cfg.HTTPPort
}

func (s *HTTPServer) recordError(err error) {
	if err == nil {
		return
	}
	s.errMu.Lock()
	defer s.errMu.Unlock()
	s.errs = append(s.errs, err)
}

func (s *HTTPServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	uptimeDuration := time.Since(s.startedAt).Truncate(time.Second)

	resp := HealthResponse{
		Status:        "ok",
		Version:       s.cfg.Version,
		Uptime:        uptimeDuration.String(),
		UptimeSeconds: int64(uptimeDuration.Seconds()),
		Timestamp:     time.Now().UTC(),
		Endpoints: HealthEndpoints{
			HTTP:   fmt.Sprintf("http://%s", s.HTTPAddr()),
			Socket: s.cfg.SocketPath,
		},
	}

	statusCode := http.StatusOK
	resp.Database = s.checkDatabaseHealth(r.Context(), &statusCode)
	if statusCode != http.StatusOK {
		resp.Status = "degraded"
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
		s.recordError(encErr)
	}
}

func (s *HTTPServer) checkDatabaseHealth(
	ctx context.Context,
	statusCode *int,
) DatabaseHealth {
	if s.engine == nil {
		*statusCode = http.StatusServiceUnavailable
		return DatabaseHealth{
			Status: "unconfigured",
			Error:  "storage engine not initialized",
		}
	}

	if pingErr := s.engine.Ping(ctx); pingErr != nil {
		*statusCode = http.StatusServiceUnavailable
		return DatabaseHealth{
			Status: "unreachable",
			Driver: s.engine.Driver(),
			Path:   s.engine.Path(),
			Error:  pingErr.Error(),
		}
	}

	if s.querySvc == nil {
		return DatabaseHealth{
			Status: "ok",
			Driver: s.engine.Driver(),
			Path:   s.engine.Path(),
		}
	}

	st, stErr := s.querySvc.GetDatabaseStatus(
		ctx,
		storage.GetDatabaseStatusQuery{},
	)
	if stErr != nil {
		return DatabaseHealth{
			Status: "ok",
			Driver: s.engine.Driver(),
			Path:   s.engine.Path(),
			Error:  stErr.Error(),
		}
	}

	return DatabaseHealth{
		Status:            "ok",
		Driver:            st.Driver,
		Path:              st.DatabasePath,
		IntegrityOK:       st.IntegrityOK,
		SchemaVersion:     st.LatestVersion,
		MigrationsApplied: st.MigrationCount,
		TablesCount:       st.TablesCount,
	}
}

func (s *HTTPServer) resolveNextPath(
	r *http.Request,
	defaultPath string,
) string {
	next := r.FormValue("next")
	if next == "" {
		next = r.URL.Query().Get("next")
	}
	if next == "" || !strings.HasPrefix(next, "/") ||
		strings.HasPrefix(next, "//") {
		return defaultPath
	}
	return next
}

func (s *HTTPServer) handlePublicDashboard(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	if s.authQuery != nil {
		if needs, nErr := s.authQuery.NeedsSetup(
			r.Context(),
		); nErr == nil && needs {
			http.Redirect(w, r, "/setup", http.StatusFound)
			return
		}
	}

	var isAuth bool
	var username, role string

	if s.authQuery != nil {
		if sess, err := s.extractSession(r); err == nil && sess != nil {
			isAuth = true
			username = sess.Username
			role = string(sess.Role)
		}
	}

	st := s.queryStorageStatus(r.Context())
	driver := "sqlite"
	integOK := true
	if st != nil {
		driver = st.Driver
		integOK = st.IntegrityOK
	}

	uptimeDuration := time.Since(s.startedAt).Truncate(time.Second)

	pubCtx := PublicDashboardContext{
		Title:           "Public Monitor",
		Version:         s.cfg.Version,
		HTTPBind:        s.cfg.HTTPBind,
		HTTPPort:        s.effectivePort(),
		DaemonMode:      s.cfg.DaemonMode,
		IsAuthenticated: isAuth,
		OperatorUser:    username,
		OperatorRole:    role,
		Uptime:          uptimeDuration.String(),
		StartedAt:       s.startedAt,
		DatabaseDriver:  driver,
		IntegrityOK:     integOK,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.RenderStandalone(
		w,
		"public_dashboard.html",
		pubCtx,
	); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *HTTPServer) handleOperatorDashboard(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.URL.Path != "/operator" {
		http.NotFound(w, r)
		return
	}

	if s.authQuery != nil {
		if needs, nErr := s.authQuery.NeedsSetup(
			r.Context(),
		); nErr == nil && needs {
			http.Redirect(w, r, "/setup", http.StatusFound)
			return
		}
	}

	var session *auth.Session
	if s.authQuery != nil {
		sess, err := s.extractSession(r)
		if err != nil || sess == nil {
			http.Redirect(
				w,
				r,
				"/login?next=/operator",
				http.StatusFound,
			)
			return
		}
		if sess.MustChangePassword {
			http.Redirect(
				w,
				r,
				"/settings/password?enforce=true",
				http.StatusFound,
			)
			return
		}
		if s.cfg.Require2FA {
			devices, dErr := s.authQuery.ListDevices(
				r.Context(),
				auth.ListDevicesQuery{Username: sess.Username},
			)
			if dErr == nil && len(devices) == 0 {
				http.Redirect(
					w,
					r,
					"/settings/2fa?enforce=true",
					http.StatusFound,
				)
				return
			}
		}
		session = sess
	}

	st := s.queryStorageStatus(r.Context())
	migrations := s.queryAppliedMigrations(r.Context())

	user := os.Getenv("USER")
	authState := "LOCAL_SESSION"
	opRole := "operator"
	if session != nil {
		user = session.Username
		authState = "AUTHENTICATED"
		opRole = string(session.Role)
	} else if user == "" {
		user = "operator@localhost"
	}

	uptimeDuration := time.Since(s.startedAt).Truncate(time.Second)

	pageCtx := PageContext{
		Title:        "Command Overview",
		Version:      s.cfg.Version,
		ActiveNav:    "overview",
		Breadcrumb:   "COMMAND_OVERVIEW",
		HTTPBind:     s.cfg.HTTPBind,
		HTTPPort:     s.effectivePort(),
		SocketPath:   s.cfg.SocketPath,
		DaemonMode:   s.cfg.DaemonMode,
		OperatorUser: user,
		OperatorRole: opRole,
		AuthProvider: s.cfg.AuthProvider,
		AuthState:    authState,
		Uptime:       uptimeDuration.String(),
		StartedAt:    s.startedAt,
		Storage:      st,
		Migrations:   migrations,
		Message:      r.URL.Query().Get("msg"),
		Error:        r.URL.Query().Get("error"),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.RenderTemplate(w, "dashboard.html", pageCtx); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (s *HTTPServer) extractSession(r *http.Request) (*auth.Session, error) {
	if authHdr := r.Header.Get("Authorization"); strings.HasPrefix(
		authHdr,
		"Bearer ",
	) {
		token := strings.TrimSpace(
			strings.TrimPrefix(authHdr, "Bearer "),
		)
		if token != "" {
			if strings.HasPrefix(token, tokens.TokenPrefix) {
				if s.tokenSvc == nil {
					return nil, errors.New(
						"token service unconfigured",
					)
				}
				tok, err := s.tokenSvc.ValidateToken(
					r.Context(),
					token,
					r.RemoteAddr,
				)
				if err != nil {
					return nil, fmt.Errorf(
						"validating token: %w",
						err,
					)
				}
				return &auth.Session{
					ID:       tok.ID,
					Username: tok.Username,
					Role:     tok.Role,
				}, nil
			}

			if s.authQuery == nil {
				return nil, errors.New(
					"auth query unconfigured",
				)
			}
			return s.authQuery.ValidateSession(
				r.Context(),
				auth.ValidateSessionQuery{SessionID: token},
			)
		}
	}

	if cookie, err := r.Cookie(SessionCookieName); err == nil &&
		cookie.Value != "" {
		if s.authQuery == nil {
			return nil, errors.New("auth query unconfigured")
		}
		return s.authQuery.ValidateSession(
			r.Context(),
			auth.ValidateSessionQuery{SessionID: cookie.Value},
		)
	}

	if isUnix, ok := r.Context().Value(
		contextKeyIsUnixSocket,
	).(bool); ok && isUnix {
		return &auth.Session{
			ID:       "local-unix-socket",
			Username: "local-operator",
			Role:     auth.RoleAdmin,
		}, nil
	}

	return nil, errors.New("missing session token")
}

func (s *HTTPServer) handleSetup(w http.ResponseWriter, r *http.Request) {
	if s.authQuery == nil || s.authCmd == nil {
		http.Redirect(w, r, "/operator", http.StatusFound)
		return
	}

	needs, err := s.authQuery.NeedsSetup(r.Context())
	if err != nil {
		http.Error(
			w,
			"Internal Server Error",
			http.StatusInternalServerError,
		)
		return
	}
	if !needs {
		if r.Method == http.MethodGet {
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		http.Error(
			w,
			"Setup has already been completed",
			http.StatusForbidden,
		)
		return
	}

	if r.Method == http.MethodGet {
		s.handleSetupGet(w, r)
		return
	}
	if r.Method == http.MethodPost {
		s.handleSetupPost(w, r)
		return
	}

	http.Error(
		w,
		"Method Not Allowed",
		http.StatusMethodNotAllowed,
	)
}

func (s *HTTPServer) handleSetupGet(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	minLen := s.cfg.PasswordPolicy.MinLength
	if minLen <= 0 {
		minLen = 8
	}
	ctx := SetupPageContext{
		Version:           s.cfg.Version,
		AuthProvider:      s.cfg.AuthProvider,
		DefaultUsername:   "admin",
		MinPasswordLength: minLen,
	}
	if err := web.RenderStandalone(w, "setup.html", ctx); err != nil {
		http.Error(w, "Render Error", http.StatusInternalServerError)
	}
}

func (s *HTTPServer) handleSetupPost(
	w http.ResponseWriter,
	r *http.Request,
) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")

	minLen := s.cfg.PasswordPolicy.MinLength
	if minLen <= 0 {
		minLen = 8
	}

	sess, err := s.authCmd.InitialSetup(
		r.Context(),
		auth.InitialSetupCommand{
			Username:        username,
			Password:        password,
			ConfirmPassword: confirmPassword,
			MinLength:       minLen,
		},
	)
	if err != nil {
		if errors.Is(err, auth.ErrSetupCompleted) {
			http.Error(
				w,
				"Setup has already been completed",
				http.StatusForbidden,
			)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		ctx := SetupPageContext{
			Version:           s.cfg.Version,
			AuthProvider:      s.cfg.AuthProvider,
			DefaultUsername:   username,
			MinPasswordLength: minLen,
			Error:             err.Error(),
		}
		if rErr := web.RenderStandalone(w, "setup.html", ctx); rErr != nil {
			http.Error(w, "Render Error", http.StatusInternalServerError)
		}
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sess.ID,
		Path:     "/",
		Expires:  sess.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	if s.cfg.Require2FA {
		http.Redirect(
			w,
			r,
			"/settings/2fa?enforce=true",
			http.StatusFound,
		)
		return
	}

	http.Redirect(w, r, "/operator", http.StatusFound)
}

func (s *HTTPServer) handleAPISetup(
	w http.ResponseWriter,
	r *http.Request,
) {
	if s.authQuery == nil || s.authCmd == nil {
		http.Error(
			w,
			"Auth unconfigured",
			http.StatusServiceUnavailable,
		)
		return
	}

	switch r.Method {
	case http.MethodGet:
		needs, err := s.authQuery.NeedsSetup(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if encErr := json.NewEncoder(w).Encode(map[string]any{
			"needs_setup": needs,
		}); encErr != nil {
			http.Error(w, encErr.Error(), http.StatusInternalServerError)
			return
		}

	case http.MethodPost:
		needs, err := s.authQuery.NeedsSetup(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !needs {
			http.Error(
				w,
				"Setup has already been completed",
				http.StatusForbidden,
			)
			return
		}

		var req struct {
			Username        string `json:"username"`
			Password        string `json:"password"`
			ConfirmPassword string `json:"confirm_password"`
		}
		if dErr := json.NewDecoder(r.Body).Decode(&req); dErr != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		minLen := s.cfg.PasswordPolicy.MinLength
		if minLen <= 0 {
			minLen = 8
		}

		sess, sErr := s.authCmd.InitialSetup(
			r.Context(),
			auth.InitialSetupCommand{
				Username:        req.Username,
				Password:        req.Password,
				ConfirmPassword: req.ConfirmPassword,
				MinLength:       minLen,
			},
		)
		if sErr != nil {
			if errors.Is(sErr, auth.ErrSetupCompleted) {
				http.Error(
					w,
					"Setup has already been completed",
					http.StatusForbidden,
				)
				return
			}
			http.Error(w, sErr.Error(), http.StatusBadRequest)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     SessionCookieName,
			Value:    sess.ID,
			Path:     "/",
			Expires:  sess.ExpiresAt,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if encErr := json.NewEncoder(w).Encode(map[string]any{
			"status":     "ok",
			"username":   sess.Username,
			"role":       sess.Role,
			"session_id": sess.ID,
		}); encErr != nil {
			http.Error(w, encErr.Error(), http.StatusInternalServerError)
			return
		}

	default:
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
	}
}

func (s *HTTPServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.authCmd == nil {
		http.Redirect(w, r, "/operator", http.StatusFound)
		return
	}

	if s.authQuery != nil {
		if needs, nErr := s.authQuery.NeedsSetup(
			r.Context(),
		); nErr == nil && needs {
			http.Redirect(w, r, "/setup", http.StatusFound)
			return
		}
	}

	authProv := s.cfg.AuthProvider
	if authProv == "" {
		authProv = "sqlite"
	}

	if r.Method == http.MethodGet {
		s.handleLoginGet(w, r, authProv)
		return
	}
	if r.Method == http.MethodPost {
		s.handleLoginPost(w, r, authProv)
		return
	}

	http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
}

func (s *HTTPServer) handleLoginGet(
	w http.ResponseWriter,
	r *http.Request,
	authProv string,
) {
	nextPath := s.resolveNextPath(r, "/operator")
	if s.authQuery != nil {
		if _, err := s.extractSession(r); err == nil {
			http.Redirect(w, r, nextPath, http.StatusFound)
			return
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.RenderStandalone(w, "login.html", LoginPageContext{
		Version:      s.cfg.Version,
		AuthProvider: authProv,
		Next:         nextPath,
	}); err != nil {
		http.Error(w, "Render Error", http.StatusInternalServerError)
	}
}

func (s *HTTPServer) handleLoginPost(
	w http.ResponseWriter,
	r *http.Request,
	authProv string,
) {
	nextPath := s.resolveNextPath(r, "/operator")
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	loginRes, err := s.authCmd.Login(r.Context(), auth.LoginCommand{
		Username: username,
		Password: password,
	})
	if err != nil {
		s.renderLoginError(w, authProv, username, nextPath, err)
		return
	}

	if loginRes.Requires2FA {
		redir := fmt.Sprintf(
			"/auth/2fa?token=%s&next=%s",
			url.QueryEscape(loginRes.ChallengeToken),
			url.QueryEscape(nextPath),
		)
		http.Redirect(w, r, redir, http.StatusFound)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    loginRes.Session.ID,
		Path:     "/",
		Expires:  loginRes.Session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	if loginRes.Session.MustChangePassword {
		http.Redirect(
			w,
			r,
			"/settings/password?enforce=true",
			http.StatusFound,
		)
		return
	}

	if s.cfg.Require2FA && s.authQuery != nil {
		devices, dErr := s.authQuery.ListDevices(
			r.Context(),
			auth.ListDevicesQuery{Username: loginRes.Session.Username},
		)
		if dErr == nil && len(devices) == 0 {
			http.Redirect(
				w,
				r,
				"/settings/2fa?enforce=true",
				http.StatusFound,
			)
			return
		}
	}

	http.Redirect(w, r, nextPath, http.StatusFound)
}

func (s *HTTPServer) renderLoginError(
	w http.ResponseWriter,
	authProv string,
	username string,
	nextPath string,
	err error,
) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	status := http.StatusUnauthorized
	errMsg := "Invalid operator username or password"
	if errors.Is(err, auth.ErrAccessDenied) {
		status = http.StatusForbidden
		errMsg = "Access denied: user is not in an authorized group"
	}
	w.WriteHeader(status)
	if rErr := web.RenderStandalone(w, "login.html", LoginPageContext{
		Version:      s.cfg.Version,
		AuthProvider: authProv,
		Username:     username,
		Error:        errMsg,
		Next:         nextPath,
	}); rErr != nil {
		http.Error(w, "Render Error", http.StatusInternalServerError)
	}
}

func (s *HTTPServer) handle2FA(w http.ResponseWriter, r *http.Request) {
	if s.authCmd == nil {
		http.Redirect(w, r, "/operator", http.StatusFound)
		return
	}

	nextPath := s.resolveNextPath(r, "/operator")

	if r.Method == http.MethodGet {
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Redirect(
				w,
				r,
				"/login?next="+url.QueryEscape(nextPath),
				http.StatusFound,
			)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := web.RenderStandalone(w, "2fa.html", TwoFactorPageContext{
			ChallengeToken: token,
			Next:           nextPath,
		}); err != nil {
			http.Error(w, "Render Error", http.StatusInternalServerError)
		}
		return
	}

	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	token := strings.TrimSpace(r.FormValue("challenge_token"))
	passcode := strings.TrimSpace(r.FormValue("passcode"))

	res, err := s.authCmd.Verify2FA(r.Context(), auth.Verify2FACommand{
		ChallengeToken: token,
		Passcode:       passcode,
	})
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		if rErr := web.RenderStandalone(w, "2fa.html", TwoFactorPageContext{
			ChallengeToken: token,
			Error:          "Invalid 2FA passcode or expired challenge",
			Next:           nextPath,
		}); rErr != nil {
			http.Error(w, "Render Error", http.StatusInternalServerError)
		}
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    res.Session.ID,
		Path:     "/",
		Expires:  res.Session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	if res.Session.MustChangePassword {
		http.Redirect(
			w,
			r,
			"/settings/password?enforce=true",
			http.StatusFound,
		)
		return
	}

	http.Redirect(w, r, nextPath, http.StatusFound)
}

func (s *HTTPServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookieName); err == nil &&
		s.authCmd != nil {
		if lErr := s.authCmd.Logout(r.Context(), auth.LogoutCommand{
			SessionID: cookie.Value,
		}); lErr != nil {
			_ = lErr
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *HTTPServer) handleSettings2FA(
	w http.ResponseWriter,
	r *http.Request,
) {
	sess, err := s.extractSession(r)
	if err != nil || sess == nil {
		http.Redirect(w, r, "/login?next=/settings/2fa", http.StatusFound)
		return
	}

	if sess.MustChangePassword {
		http.Redirect(
			w,
			r,
			"/settings/password?enforce=true",
			http.StatusFound,
		)
		return
	}

	var devices []auth.TOTPDevice
	if s.authQuery != nil {
		if devs, dErr := s.authQuery.ListDevices(
			r.Context(),
			auth.ListDevicesQuery{Username: sess.Username},
		); dErr == nil {
			devices = devs
		}
	}

	var prep *auth.PrepareEnrollmentResult
	if s.authCmd != nil {
		if p, pErr := s.authCmd.PrepareEnrollment(
			r.Context(),
			sess.Username,
		); pErr == nil {
			prep = p
		}
	}

	st := s.queryStorageStatus(r.Context())
	uptimeDuration := time.Since(s.startedAt).Truncate(time.Second)

	enforced := len(devices) == 0 &&
		(s.cfg.Require2FA || r.URL.Query().Get("enforce") == "true")

	pageCtx := Settings2FAPageContext{
		PageContext: PageContext{
			Title:        "Security & 2FA",
			Version:      s.cfg.Version,
			ActiveNav:    "settings_2fa",
			Breadcrumb:   "SECURITY / 2FA",
			HTTPBind:     s.cfg.HTTPBind,
			HTTPPort:     s.effectivePort(),
			SocketPath:   s.cfg.SocketPath,
			DaemonMode:   s.cfg.DaemonMode,
			OperatorUser: sess.Username,
			OperatorRole: string(sess.Role),
			AuthProvider: s.cfg.AuthProvider,
			AuthState:    "AUTHENTICATED",
			Uptime:       uptimeDuration.String(),
			StartedAt:    s.startedAt,
			Storage:      st,
			Message:      r.URL.Query().Get("msg"),
			Error:        r.URL.Query().Get("error"),
		},
		Devices:    devices,
		EnrollPrep: prep,
		Enforced:   enforced,
		Message:    r.URL.Query().Get("msg"),
		Error:      r.URL.Query().Get("error"),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if enforced {
		if err := web.RenderStandalone(
			w,
			"enforce_2fa.html",
			pageCtx,
		); err != nil {
			http.Error(w, "Render Error", http.StatusInternalServerError)
		}
		return
	}

	if err := web.RenderTemplate(
		w,
		"settings_2fa.html",
		pageCtx,
	); err != nil {
		http.Error(w, "Render Error", http.StatusInternalServerError)
	}
}

func (s *HTTPServer) handleSettings2FAEnroll(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	sess, err := s.extractSession(r)
	if err != nil || sess == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	name := strings.TrimSpace(r.FormValue("device_name"))
	secret := strings.TrimSpace(r.FormValue("secret_base32"))
	passcode := strings.TrimSpace(r.FormValue("passcode"))

	_, err = s.authCmd.EnrollDevice(r.Context(), auth.EnrollDeviceCommand{
		Username:     sess.Username,
		DeviceName:   name,
		SecretBase32: secret,
		Passcode:     passcode,
	})
	if err != nil {
		redir := "/settings/2fa?error=" +
			url.QueryEscape("Enrollment failed: "+err.Error())
		if s.cfg.Require2FA {
			redir += "&enforce=true"
		}
		http.Redirect(w, r, redir, http.StatusFound)
		return
	}

	redir := "/settings/2fa?msg=" +
		url.QueryEscape("Device registered successfully")
	if s.cfg.Require2FA {
		redir = "/?msg=" + url.QueryEscape(
			"Two-factor authentication configured successfully. Welcome!",
		)
	}
	http.Redirect(w, r, redir, http.StatusFound)
}

func (s *HTTPServer) handleSettings2FARevoke(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	sess, err := s.extractSession(r)
	if err != nil || sess == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	deviceID := strings.TrimSpace(r.FormValue("device_id"))
	if s.authCmd != nil {
		if rErr := s.authCmd.RevokeDevice(r.Context(), auth.RevokeDeviceCommand{
			DeviceID: deviceID,
			Username: sess.Username,
		}); rErr != nil {
			_ = rErr
		}
	}

	http.Redirect(w, r, "/settings/2fa", http.StatusFound)
}

func (s *HTTPServer) handleSettings2FAInvite(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	sess, err := s.extractSession(r)
	if err != nil || sess == nil {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	targetUser := strings.TrimSpace(r.FormValue("username"))
	label := strings.TrimSpace(r.FormValue("label"))
	if targetUser == "" {
		targetUser = sess.Username
	}

	inv, err := s.authCmd.CreateInvite(r.Context(), auth.CreateInviteCommand{
		Username:  targetUser,
		Label:     label,
		CreatedBy: sess.Username,
	})
	if err != nil {
		redir := "/settings/2fa?error=" +
			url.QueryEscape("Invite generation failed: "+err.Error())
		http.Redirect(w, r, redir, http.StatusFound)
		return
	}

	var devices []auth.TOTPDevice
	if s.authQuery != nil {
		if devs, dErr := s.authQuery.ListDevices(
			r.Context(),
			auth.ListDevicesQuery{Username: sess.Username},
		); dErr == nil {
			devices = devs
		}
	}

	var prep *auth.PrepareEnrollmentResult
	if s.authCmd != nil {
		if p, pErr := s.authCmd.PrepareEnrollment(
			r.Context(),
			sess.Username,
		); pErr == nil {
			prep = p
		}
	}

	st := s.queryStorageStatus(r.Context())
	uptimeDuration := time.Since(s.startedAt).Truncate(time.Second)

	pageCtx := Settings2FAPageContext{
		PageContext: PageContext{
			Title:        "Security & 2FA",
			Version:      s.cfg.Version,
			ActiveNav:    "settings_2fa",
			Breadcrumb:   "SECURITY / 2FA",
			HTTPBind:     s.cfg.HTTPBind,
			HTTPPort:     s.effectivePort(),
			SocketPath:   s.cfg.SocketPath,
			DaemonMode:   s.cfg.DaemonMode,
			OperatorUser: sess.Username,
			AuthState:    "AUTHENTICATED",
			Uptime:       uptimeDuration.String(),
			StartedAt:    s.startedAt,
			Storage:      st,
		},
		Devices:      devices,
		EnrollPrep:   prep,
		InviteResult: inv,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.RenderTemplate(
		w,
		"settings_2fa.html",
		pageCtx,
	); err != nil {
		http.Error(w, "Render Error", http.StatusInternalServerError)
	}
}

func (s *HTTPServer) handleSettingsPassword(
	w http.ResponseWriter,
	r *http.Request,
) {
	sess, err := s.extractSession(r)
	if err != nil || sess == nil {
		http.Redirect(w, r, "/login?next=/settings/password", http.StatusFound)
		return
	}

	if r.Method == http.MethodGet {
		s.handleSettingsPasswordGet(w, r, sess)
		return
	}

	if r.Method == http.MethodPost {
		s.handleSettingsPasswordPost(w, r, sess)
		return
	}

	http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
}

func (s *HTTPServer) handleSettingsPasswordGet(
	w http.ResponseWriter,
	r *http.Request,
	sess *auth.Session,
) {
	st := s.queryStorageStatus(r.Context())
	uptimeDuration := time.Since(s.startedAt).Truncate(time.Second)
	enforced := sess.MustChangePassword ||
		r.URL.Query().Get("enforce") == "true"

	pageCtx := SettingsPasswordPageContext{
		PageContext: PageContext{
			Title:        "Change Password",
			Version:      s.cfg.Version,
			ActiveNav:    "settings_password",
			Breadcrumb:   "SETTINGS / PASSWORD",
			HTTPBind:     s.cfg.HTTPBind,
			HTTPPort:     s.effectivePort(),
			SocketPath:   s.cfg.SocketPath,
			DaemonMode:   s.cfg.DaemonMode,
			OperatorUser: sess.Username,
			OperatorRole: string(sess.Role),
			AuthProvider: s.cfg.AuthProvider,
			AuthState:    "AUTHENTICATED",
			Uptime:       uptimeDuration.String(),
			StartedAt:    s.startedAt,
			Storage:      st,
			Message:      r.URL.Query().Get("msg"),
			Error:        r.URL.Query().Get("error"),
		},
		Enforced: enforced,
		Message:  r.URL.Query().Get("msg"),
		Error:    r.URL.Query().Get("error"),
		Next:     s.resolveNextPath(r, "/operator"),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if enforced {
		if err := web.RenderStandalone(
			w,
			"enforce_password.html",
			pageCtx,
		); err != nil {
			http.Error(w, "Render Error", http.StatusInternalServerError)
		}
		return
	}

	if err := web.RenderTemplate(
		w,
		"settings_password.html",
		pageCtx,
	); err != nil {
		http.Error(w, "Render Error", http.StatusInternalServerError)
	}
}

func (s *HTTPServer) handleSettingsPasswordPost(
	w http.ResponseWriter,
	r *http.Request,
	sess *auth.Session,
) {
	if s.authCmd == nil {
		http.Redirect(w, r, "/operator", http.StatusFound)
		return
	}

	curr := r.FormValue("current_password")
	newP := r.FormValue("new_password")
	confirm := r.FormValue("confirm_password")
	next := s.resolveNextPath(r, "/operator")

	minLen := s.cfg.PasswordPolicy.MinLength
	if minLen <= 0 {
		minLen = 8
	}

	if msg := validatePasswordInputs(curr, newP, confirm, minLen); msg != "" {
		s.redirectPasswordError(w, r, sess, next, msg)
		return
	}

	err := s.authCmd.ChangePassword(r.Context(), auth.ChangePasswordCommand{
		Username:        sess.Username,
		CurrentPassword: curr,
		NewPassword:     newP,
		MinLength:       minLen,
	})
	if err != nil {
		s.redirectPasswordError(
			w,
			r,
			sess,
			next,
			mapChangePasswordError(err, minLen),
		)
		return
	}

	dest := "/operator?msg=" + url.QueryEscape("Password updated successfully")
	if next != "" && next != "/settings/password" {
		dest = next
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

func (s *HTTPServer) redirectPasswordError(
	w http.ResponseWriter,
	r *http.Request,
	sess *auth.Session,
	next, msg string,
) {
	q := url.Values{}
	q.Set("error", msg)
	if sess.MustChangePassword || r.URL.Query().Get("enforce") == "true" {
		q.Set("enforce", "true")
	}
	if next != "/operator" {
		q.Set("next", next)
	}
	http.Redirect(w, r, "/settings/password?"+q.Encode(), http.StatusFound)
}

func validatePasswordInputs(
	curr, newP, confirm string,
	minLen int,
) string {
	if curr == "" || newP == "" || confirm == "" {
		return "All password fields are required"
	}
	if newP != confirm {
		return "New passwords do not match"
	}
	if len(newP) < minLen {
		return fmt.Sprintf("New password must be at least %d characters", minLen)
	}
	return ""
}

func mapChangePasswordError(err error, minLen int) string {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		return "Current password is incorrect"
	case errors.Is(err, auth.ErrPasswordUnchanged):
		return "New password must be different from current password"
	case errors.Is(err, auth.ErrPasswordTooShort):
		return fmt.Sprintf("New password must be at least %d characters", minLen)
	default:
		return err.Error()
	}
}

func (s *HTTPServer) requireAdmin(
	w http.ResponseWriter,
	r *http.Request,
) (*auth.Session, bool) {
	sess, err := s.extractSession(r)
	if err != nil || sess == nil {
		http.Redirect(w, r, "/login?next="+r.URL.Path, http.StatusFound)
		return nil, false
	}
	if sess.MustChangePassword {
		http.Redirect(
			w,
			r,
			"/settings/password?enforce=true",
			http.StatusFound,
		)
		return nil, false
	}
	if sess.Role != auth.RoleAdmin {
		http.Error(
			w,
			"Forbidden: Admin privileges required",
			http.StatusForbidden,
		)
		return nil, false
	}
	return sess, true
}

func (s *HTTPServer) handleSettingsUsers(
	w http.ResponseWriter,
	r *http.Request,
) {
	sess, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}

	var users []auth.User
	devicesByUser := make(map[string][]auth.TOTPDevice)
	if s.authQuery != nil {
		if uList, err := s.authQuery.ListUsers(
			r.Context(),
			auth.ListUsersQuery{},
		); err == nil {
			users = uList
			for _, u := range users {
				q := auth.ListDevicesQuery{
					Username: u.Username,
				}
				if devs, dErr := s.authQuery.ListDevices(
					r.Context(),
					q,
				); dErr == nil {
					devicesByUser[u.Username] = devs
				}
			}
		}
	}

	st := s.queryStorageStatus(r.Context())
	uptimeDuration := time.Since(s.startedAt).Truncate(time.Second)

	pageCtx := SettingsUsersPageContext{
		PageContext: PageContext{
			Title:        "User Management",
			Version:      s.cfg.Version,
			ActiveNav:    "settings_users",
			Breadcrumb:   "SETTINGS / USERS",
			HTTPBind:     s.cfg.HTTPBind,
			HTTPPort:     s.effectivePort(),
			SocketPath:   s.cfg.SocketPath,
			DaemonMode:   s.cfg.DaemonMode,
			OperatorUser: sess.Username,
			OperatorRole: string(sess.Role),
			AuthProvider: s.cfg.AuthProvider,
			AuthState:    "AUTHENTICATED",
			Uptime:       uptimeDuration.String(),
			StartedAt:    s.startedAt,
			Storage:      st,
			Message:      r.URL.Query().Get("msg"),
			Error:        r.URL.Query().Get("error"),
		},
		Users:       users,
		UserDevices: devicesByUser,
		InspectUser: r.URL.Query().Get("inspect"),
		Message:     r.URL.Query().Get("msg"),
		Error:       r.URL.Query().Get("error"),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.RenderTemplate(
		w,
		"settings_users.html",
		pageCtx,
	); err != nil {
		http.Error(w, "Render Error", http.StatusInternalServerError)
	}
}

func (s *HTTPServer) handleSettingsUsersCreate(
	w http.ResponseWriter,
	r *http.Request,
) {
	sess, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}
	if s.authCmd == nil {
		http.Redirect(w, r, "/settings/users", http.StatusFound)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	roleStr := strings.TrimSpace(r.FormValue("role"))
	mustChange := r.FormValue("must_change_password") == "true" ||
		r.FormValue("must_change_password") == "1" ||
		r.FormValue("must_change_password") == "on"

	role := auth.Role(roleStr)
	if !role.IsValid() {
		role = auth.RoleOperator
	}

	_, err := s.authCmd.AdminCreateUser(
		r.Context(),
		auth.AdminCreateUserCommand{
			AdminUsername:      sess.Username,
			Username:           username,
			Password:           password,
			Role:               role,
			MustChangePassword: mustChange,
		},
	)
	if err != nil {
		http.Redirect(
			w,
			r,
			"/settings/users?error="+url.QueryEscape(err.Error()),
			http.StatusFound,
		)
		return
	}

	http.Redirect(
		w,
		r,
		"/settings/users?msg="+url.QueryEscape(fmt.Sprintf(
			"Operator %s provisioned successfully",
			username,
		)),
		http.StatusFound,
	)
}

func (s *HTTPServer) handleSettingsUsersUpdate(
	w http.ResponseWriter,
	r *http.Request,
) {
	sess, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}
	if s.authCmd == nil {
		http.Redirect(w, r, "/settings/users", http.StatusFound)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	roleStr := strings.TrimSpace(r.FormValue("role"))
	active := r.FormValue("active") == "true" ||
		r.FormValue("active") == "1" ||
		r.FormValue("active") == "on"
	mustChange := r.FormValue("must_change_password") == "true" ||
		r.FormValue("must_change_password") == "1" ||
		r.FormValue("must_change_password") == "on"

	role := auth.Role(roleStr)
	if !role.IsValid() {
		role = auth.RoleOperator
	}

	err := s.authCmd.AdminUpdateUser(
		r.Context(),
		auth.AdminUpdateUserCommand{
			AdminUsername:      sess.Username,
			Username:           username,
			Role:               role,
			IsActive:           active,
			MustChangePassword: mustChange,
		},
	)
	if err != nil {
		http.Redirect(
			w,
			r,
			"/settings/users?inspect="+url.QueryEscape(username)+
				"&error="+url.QueryEscape(err.Error()),
			http.StatusFound,
		)
		return
	}

	http.Redirect(
		w,
		r,
		"/settings/users?inspect="+url.QueryEscape(username)+
			"&msg="+url.QueryEscape(
			"Operator account updated successfully",
		),
		http.StatusFound,
	)
}

func (s *HTTPServer) handleSettingsUsersResetPassword(
	w http.ResponseWriter,
	r *http.Request,
) {
	sess, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}
	if s.authCmd == nil {
		http.Redirect(w, r, "/settings/users", http.StatusFound)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	newPassword := r.FormValue("new_password")
	mustChange := r.FormValue("must_change_password") == "true" ||
		r.FormValue("must_change_password") == "1" ||
		r.FormValue("must_change_password") == "on"

	err := s.authCmd.AdminResetPassword(
		r.Context(),
		auth.AdminResetPasswordCommand{
			AdminUsername:      sess.Username,
			Username:           username,
			NewPassword:        newPassword,
			MustChangePassword: mustChange,
		},
	)
	if err != nil {
		http.Redirect(
			w,
			r,
			"/settings/users?error="+url.QueryEscape(err.Error()),
			http.StatusFound,
		)
		return
	}

	http.Redirect(
		w,
		r,
		"/settings/users?msg="+url.QueryEscape(fmt.Sprintf(
			"Password for %s reset successfully",
			username,
		)),
		http.StatusFound,
	)
}

func (s *HTTPServer) handleSettingsUsersStatus(
	w http.ResponseWriter,
	r *http.Request,
) {
	sess, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}
	if s.authCmd == nil {
		http.Redirect(w, r, "/settings/users", http.StatusFound)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	if username == sess.Username {
		http.Redirect(
			w,
			r,
			"/settings/users?error="+url.QueryEscape(
				"Cannot change status of own account",
			),
			http.StatusFound,
		)
		return
	}

	active := r.FormValue("active") == "true" || r.FormValue("active") == "1"

	var currentRole auth.Role = auth.RoleOperator
	mustChange := false
	if s.authQuery != nil {
		if u, err := s.authQuery.GetUser(
			r.Context(),
			auth.GetUserQuery{Username: username},
		); err == nil && u != nil {
			currentRole = u.Role
			mustChange = u.MustChangePassword
		}
	}

	err := s.authCmd.AdminUpdateUser(
		r.Context(),
		auth.AdminUpdateUserCommand{
			AdminUsername:      sess.Username,
			Username:           username,
			Role:               currentRole,
			IsActive:           active,
			MustChangePassword: mustChange,
		},
	)
	if err != nil {
		http.Redirect(
			w,
			r,
			"/settings/users?error="+url.QueryEscape(err.Error()),
			http.StatusFound,
		)
		return
	}

	stateMsg := "activated"
	if !active {
		stateMsg = "deactivated"
	}
	http.Redirect(
		w,
		r,
		"/settings/users?msg="+url.QueryEscape(fmt.Sprintf(
			"Operator %s %s successfully",
			username,
			stateMsg,
		)),
		http.StatusFound,
	)
}

func (s *HTTPServer) handleSettingsUsersRevokeDevice(
	w http.ResponseWriter,
	r *http.Request,
) {
	_, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}
	if s.authCmd == nil {
		http.Redirect(w, r, "/settings/users", http.StatusFound)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	deviceID := strings.TrimSpace(r.FormValue("device_id"))
	if username == "" || deviceID == "" {
		http.Redirect(
			w,
			r,
			"/settings/users?error="+url.QueryEscape(
				"Invalid user or device identifier",
			),
			http.StatusFound,
		)
		return
	}

	err := s.authCmd.RevokeDevice(
		r.Context(),
		auth.RevokeDeviceCommand{
			DeviceID: deviceID,
			Username: username,
		},
	)
	if err != nil {
		http.Redirect(
			w,
			r,
			"/settings/users?inspect="+url.QueryEscape(username)+
				"&error="+url.QueryEscape(err.Error()),
			http.StatusFound,
		)
		return
	}

	http.Redirect(
		w,
		r,
		"/settings/users?inspect="+url.QueryEscape(username)+
			"&msg="+url.QueryEscape(
			"Two-factor authentication device revoked",
		),
		http.StatusFound,
	)
}

func (s *HTTPServer) handleSettingsUsersDelete(
	w http.ResponseWriter,
	r *http.Request,
) {
	sess, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}
	if s.authCmd == nil {
		http.Redirect(w, r, "/settings/users", http.StatusFound)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	if username == sess.Username {
		http.Redirect(
			w,
			r,
			"/settings/users?error="+url.QueryEscape(
				"Cannot delete own account",
			),
			http.StatusFound,
		)
		return
	}

	err := s.authCmd.AdminDeleteUser(
		r.Context(),
		auth.AdminDeleteUserCommand{
			AdminUsername: sess.Username,
			Username:      username,
		},
	)
	if err != nil {
		http.Redirect(
			w,
			r,
			"/settings/users?error="+url.QueryEscape(err.Error()),
			http.StatusFound,
		)
		return
	}

	http.Redirect(
		w,
		r,
		"/settings/users?msg="+url.QueryEscape(fmt.Sprintf(
			"Operator %s deleted successfully",
			username,
		)),
		http.StatusFound,
	)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, "JSON Encoding Error", http.StatusInternalServerError)
	}
}

func (s *HTTPServer) handleAPILogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}
	if s.authCmd == nil {
		http.Error(w, "Auth unconfigured", http.StatusNotImplemented)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	res, err := s.authCmd.Login(r.Context(), auth.LoginCommand{
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		status := http.StatusUnauthorized
		errMsg := "invalid credentials"
		if errors.Is(err, auth.ErrAccessDenied) {
			status = http.StatusForbidden
			errMsg = "access denied: user is not in an authorized group"
		}
		writeJSON(w, status, map[string]string{
			"error": errMsg,
		})
		return
	}

	if res.Requires2FA {
		writeJSON(w, http.StatusOK, map[string]any{
			"requires_2fa":    true,
			"challenge_token": res.ChallengeToken,
		})
		return
	}

	if s.cfg.Require2FA && s.authQuery != nil {
		devices, dErr := s.authQuery.ListDevices(
			r.Context(),
			auth.ListDevicesQuery{Username: req.Username},
		)
		if dErr == nil && len(devices) == 0 {
			writeJSON(w, http.StatusPreconditionRequired, map[string]any{
				"error": "two-factor authentication is required; " +
					"please enroll via web console",
				"enrollment_required": true,
			})
			return
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    res.Session.ID,
		Path:     "/",
		Expires:  res.Session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"requires_2fa": false,
		"session":      res.Session,
		"user":         res.User,
	})
}

func (s *HTTPServer) handleAPIVerify2FA(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}
	if s.authCmd == nil {
		http.Error(w, "Auth unconfigured", http.StatusNotImplemented)
		return
	}

	var req struct {
		ChallengeToken string `json:"challenge_token"`
		Passcode       string `json:"passcode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	res, err := s.authCmd.Verify2FA(r.Context(), auth.Verify2FACommand{
		ChallengeToken: req.ChallengeToken,
		Passcode:       req.Passcode,
	})
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "invalid 2fa passcode or expired challenge",
		})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    res.Session.ID,
		Path:     "/",
		Expires:  res.Session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"session":   res.Session,
		"user":      res.User,
		"device_id": res.DeviceID,
	})
}

func (s *HTTPServer) handleAPIDevices(w http.ResponseWriter, r *http.Request) {
	sess, err := s.extractSession(r)
	if err != nil || sess == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		devices, err := s.authQuery.ListDevices(r.Context(), auth.ListDevicesQuery{
			Username: sess.Username,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, devices)
	case http.MethodPost:
		var req struct {
			DeviceName   string `json:"device_name"`
			SecretBase32 string `json:"secret_base32"`
			Passcode     string `json:"passcode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}
		dev, err := s.authCmd.EnrollDevice(r.Context(), auth.EnrollDeviceCommand{
			Username:     sess.Username,
			DeviceName:   req.DeviceName,
			SecretBase32: req.SecretBase32,
			Passcode:     req.Passcode,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, dev)
	case http.MethodDelete:
		deviceID := r.URL.Query().Get("id")
		if deviceID == "" {
			http.Error(w, "Missing device id", http.StatusBadRequest)
			return
		}
		if err := s.authCmd.RevokeDevice(r.Context(), auth.RevokeDeviceCommand{
			DeviceID: deviceID,
			Username: sess.Username,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
	}
}

func (s *HTTPServer) handleAPIInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}
	sess, err := s.extractSession(r)
	if err != nil || sess == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Username string `json:"username"`
		Label    string `json:"label"`
	}
	if decErr := json.NewDecoder(r.Body).Decode(&req); decErr != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}
	if req.Username == "" {
		req.Username = sess.Username
	}

	inv, err := s.authCmd.CreateInvite(r.Context(), auth.CreateInviteCommand{
		Username:  req.Username,
		Label:     req.Label,
		CreatedBy: sess.Username,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, inv)
}

func (s *HTTPServer) handleAPISession(w http.ResponseWriter, r *http.Request) {
	sess, err := s.extractSession(r)
	if err != nil || sess == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *HTTPServer) queryStorageStatus(ctx context.Context) *storage.Status {
	if s.querySvc == nil {
		if s.engine != nil {
			return &storage.Status{
				Driver:       s.engine.Driver(),
				DatabasePath: s.engine.Path(),
			}
		}
		return &storage.Status{Driver: "unconfigured"}
	}

	st, err := s.querySvc.GetDatabaseStatus(
		ctx,
		storage.GetDatabaseStatusQuery{},
	)
	if err != nil {
		if s.engine != nil {
			return &storage.Status{
				Driver:       s.engine.Driver(),
				DatabasePath: s.engine.Path(),
			}
		}
		return &storage.Status{Driver: "unknown"}
	}
	return st
}

func (s *HTTPServer) queryAppliedMigrations(
	ctx context.Context,
) []storage.MigrationRecord {
	if s.querySvc == nil {
		return nil
	}

	records, err := s.querySvc.GetAppliedMigrations(
		ctx,
		storage.GetAppliedMigrationsQuery{},
	)
	if err != nil {
		return nil
	}
	return records
}

func (s *HTTPServer) handleAPITokens(w http.ResponseWriter, r *http.Request) {
	if s.tokenSvc == nil {
		http.Error(
			w,
			"Token service unconfigured",
			http.StatusServiceUnavailable,
		)
		return
	}

	sess, err := s.extractSession(r)
	if err != nil || sess == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleAPITokensGet(w, r, sess)
	case http.MethodPost:
		s.handleAPITokensPost(w, r, sess)
	case http.MethodDelete:
		s.handleAPITokensDelete(w, r, sess)
	default:
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
	}
}

func (s *HTTPServer) handleAPITokensGet(
	w http.ResponseWriter,
	r *http.Request,
	sess *auth.Session,
) {
	targetUser := sess.Username
	if sess.Role == auth.RoleAdmin {
		if qUser := r.URL.Query().Get("username"); qUser != "" {
			targetUser = qUser
		} else if r.URL.Query().Get("all") == "true" {
			targetUser = ""
		}
	}
	toks, lErr := s.tokenSvc.ListTokens(r.Context(), targetUser)
	if lErr != nil {
		http.Error(w, lErr.Error(), http.StatusInternalServerError)
		return
	}
	if toks == nil {
		toks = []tokens.APIToken{}
	}
	writeJSON(w, http.StatusOK, toks)
}

func (s *HTTPServer) handleAPITokensPost(
	w http.ResponseWriter,
	r *http.Request,
	sess *auth.Session,
) {
	var req struct {
		Name      string   `json:"name"`
		Role      string   `json:"role"`
		Username  string   `json:"username"`
		Scopes    []string `json:"scopes"`
		ExpiresIn string   `json:"expires_in"`
	}
	if dErr := json.NewDecoder(r.Body).Decode(&req); dErr != nil {
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	targetUser := sess.Username
	if req.Username != "" && sess.Role == auth.RoleAdmin {
		targetUser = req.Username
	}

	role := auth.Role(req.Role)
	if role == "" {
		role = sess.Role
	}
	if sess.Role != auth.RoleAdmin && role == auth.RoleAdmin {
		http.Error(
			w,
			"Forbidden: cannot grant admin role",
			http.StatusForbidden,
		)
		return
	}

	var expiresIn time.Duration
	if req.ExpiresIn != "" {
		d, pErr := parseDurationCustom(req.ExpiresIn)
		if pErr != nil {
			http.Error(
				w,
				"Invalid expires_in format",
				http.StatusBadRequest,
			)
			return
		}
		expiresIn = d
	}

	res, cErr := s.tokenSvc.CreateToken(
		r.Context(),
		tokens.CreateTokenCommand{
			Name:      req.Name,
			Role:      role,
			Username:  targetUser,
			Scopes:    req.Scopes,
			ExpiresIn: expiresIn,
		},
	)
	if cErr != nil {
		http.Error(w, cErr.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusCreated, res)
}

func (s *HTTPServer) handleAPITokensDelete(
	w http.ResponseWriter,
	r *http.Request,
	sess *auth.Session,
) {
	tokenID := r.URL.Query().Get("id")
	if tokenID == "" {
		http.Error(w, "Missing token id", http.StatusBadRequest)
		return
	}

	var userScope string
	if sess.Role != auth.RoleAdmin {
		userScope = sess.Username
	}

	if rErr := s.tokenSvc.RevokeToken(
		r.Context(),
		tokens.RevokeTokenCommand{
			ID:       tokenID,
			Username: userScope,
		},
	); rErr != nil {
		if errors.Is(rErr, tokens.ErrTokenNotFound) {
			http.Error(w, "Token not found", http.StatusNotFound)
			return
		}
		http.Error(w, rErr.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *HTTPServer) handleSettingsTokens(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	sess, err := s.extractSession(r)
	if err != nil || sess == nil {
		http.Redirect(
			w,
			r,
			"/login?next=/settings/tokens",
			http.StatusFound,
		)
		return
	}
	if sess.MustChangePassword {
		http.Redirect(
			w,
			r,
			"/settings/password?enforce=true",
			http.StatusFound,
		)
		return
	}

	s.renderSettingsTokensView(w, r, sess, nil, "", "")
}

func (s *HTTPServer) handleSettingsTokensCreate(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	sess, err := s.extractSession(r)
	if err != nil || sess == nil {
		http.Redirect(
			w,
			r,
			"/login?next=/settings/tokens",
			http.StatusFound,
		)
		return
	}
	if sess.MustChangePassword {
		http.Redirect(
			w,
			r,
			"/settings/password?enforce=true",
			http.StatusFound,
		)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	roleStr := strings.TrimSpace(r.FormValue("role"))
	expiresInStr := strings.TrimSpace(r.FormValue("expires_in"))

	if name == "" {
		s.renderSettingsTokensView(
			w,
			r,
			sess,
			nil,
			"",
			"Token name is required",
		)
		return
	}

	role := auth.Role(roleStr)
	if role == "" {
		role = sess.Role
	}
	if sess.Role != auth.RoleAdmin && role == auth.RoleAdmin {
		s.renderSettingsTokensView(
			w,
			r,
			sess,
			nil,
			"",
			"Forbidden: Operator cannot grant admin role",
		)
		return
	}

	var expiresIn time.Duration
	if expiresInStr != "" {
		d, pErr := parseDurationCustom(expiresInStr)
		if pErr != nil {
			s.renderSettingsTokensView(
				w,
				r,
				sess,
				nil,
				"",
				"Invalid expiration duration",
			)
			return
		}
		expiresIn = d
	}

	if s.tokenSvc == nil {
		s.renderSettingsTokensView(
			w,
			r,
			sess,
			nil,
			"",
			"Token service unconfigured",
		)
		return
	}

	res, cErr := s.tokenSvc.CreateToken(
		r.Context(),
		tokens.CreateTokenCommand{
			Name:      name,
			Role:      role,
			Username:  sess.Username,
			ExpiresIn: expiresIn,
		},
	)
	if cErr != nil {
		s.renderSettingsTokensView(w, r, sess, nil, "", cErr.Error())
		return
	}

	s.renderSettingsTokensView(
		w,
		r,
		sess,
		res,
		"Personal access token generated successfully. Copy it now!",
		"",
	)
}

func (s *HTTPServer) handleSettingsTokensRevoke(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"Method Not Allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	sess, err := s.extractSession(r)
	if err != nil || sess == nil {
		http.Redirect(
			w,
			r,
			"/login?next=/settings/tokens",
			http.StatusFound,
		)
		return
	}
	if sess.MustChangePassword {
		http.Redirect(
			w,
			r,
			"/settings/password?enforce=true",
			http.StatusFound,
		)
		return
	}

	tokenID := strings.TrimSpace(r.FormValue("token_id"))
	if tokenID == "" {
		s.renderSettingsTokensView(
			w,
			r,
			sess,
			nil,
			"",
			"Token ID is required for revocation",
		)
		return
	}

	if s.tokenSvc == nil {
		s.renderSettingsTokensView(
			w,
			r,
			sess,
			nil,
			"",
			"Token service unconfigured",
		)
		return
	}

	var userScope string
	if sess.Role != auth.RoleAdmin {
		userScope = sess.Username
	}

	if rErr := s.tokenSvc.RevokeToken(
		r.Context(),
		tokens.RevokeTokenCommand{
			ID:       tokenID,
			Username: userScope,
		},
	); rErr != nil {
		s.renderSettingsTokensView(w, r, sess, nil, "", rErr.Error())
		return
	}

	s.renderSettingsTokensView(
		w,
		r,
		sess,
		nil,
		"Token revoked successfully",
		"",
	)
}

func (s *HTTPServer) renderSettingsTokensView(
	w http.ResponseWriter,
	r *http.Request,
	sess *auth.Session,
	created *tokens.CreateTokenResult,
	message string,
	errStr string,
) {
	var userTokens []tokens.APIToken
	if s.tokenSvc != nil {
		targetUser := sess.Username
		if sess.Role == auth.RoleAdmin {
			targetUser = ""
		}
		toks, tErr := s.tokenSvc.ListTokens(r.Context(), targetUser)
		if tErr == nil {
			userTokens = toks
		}
	}

	st := s.queryStorageStatus(r.Context())
	uptimeDuration := time.Since(s.startedAt).Truncate(time.Second)

	pageCtx := SettingsTokensPageContext{
		PageContext: PageContext{
			Title:        "Personal Access Tokens",
			Version:      s.cfg.Version,
			ActiveNav:    "settings_tokens",
			Breadcrumb:   "SETTINGS / TOKENS",
			HTTPBind:     s.cfg.HTTPBind,
			HTTPPort:     s.effectivePort(),
			DaemonMode:   s.cfg.DaemonMode,
			Storage:      st,
			OperatorUser: sess.Username,
			OperatorRole: string(sess.Role),
			AuthProvider: s.cfg.AuthProvider,
			Uptime:       uptimeDuration.String(),
			StartedAt:    s.startedAt,
		},
		Tokens:       userTokens,
		CreatedToken: created,
		Message:      message,
		Error:        errStr,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.RenderTemplate(
		w,
		"settings_tokens.html",
		pageCtx,
	); err != nil {
		http.Error(w, "Render Error", http.StatusInternalServerError)
	}
}

func parseDurationCustom(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "0" || s == "never" {
		return 0, nil
	}
	if strings.HasSuffix(s, "d") {
		daysStr := strings.TrimSuffix(s, "d")
		days, err := strconv.Atoi(daysStr)
		if err != nil || days <= 0 {
			return 0, fmt.Errorf("invalid day duration %q", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	if strings.HasSuffix(s, "y") {
		yearsStr := strings.TrimSuffix(s, "y")
		years, err := strconv.Atoi(yearsStr)
		if err != nil || years <= 0 {
			return 0, fmt.Errorf("invalid year duration %q", s)
		}
		return time.Duration(years) * 365 * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
