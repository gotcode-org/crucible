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
	"os"
	"path/filepath"
	"sync"
	"time"

	"gotcode.org/crucible/internal/storage"
	"gotcode.org/crucible/web"
)

// HTTPServer implements the Server interface hosting the embedded web shell,
// metrics, and JSON health endpoints over dual TCP and UNIX domain sockets.
type HTTPServer struct {
	cfg          Config
	engine       storage.Engine
	querySvc     storage.QueryHandler
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

// NewServer initializes a new HTTPServer with embedded web routes and static
// asset handlers.
func NewServer(
	cfg Config,
	engine storage.Engine,
	querySvc storage.QueryHandler,
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

	if querySvc == nil && engine != nil {
		querySvc = storage.NewQueryService(engine)
	}

	s := &HTTPServer{
		cfg:      cfg,
		engine:   engine,
		querySvc: querySvc,
		mux:      http.NewServeMux(),
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
	s.mux.HandleFunc("/", s.handleDashboard)

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

func (s *HTTPServer) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	st := s.queryStorageStatus(r.Context())
	migrations := s.queryAppliedMigrations(r.Context())

	user := os.Getenv("USER")
	if user == "" {
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
		AuthState:    "LOCAL_SESSION",
		Uptime:       uptimeDuration.String(),
		StartedAt:    s.startedAt,
		Storage:      st,
		Migrations:   migrations,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := web.RenderTemplate(w, "dashboard.html", pageCtx); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
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
