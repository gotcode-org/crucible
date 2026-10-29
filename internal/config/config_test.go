// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package config_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gotcode.org/crucible/internal/config"
)

type mockSourceReader struct {
	files map[string][]byte
}

func (m *mockSourceReader) ReadConfigFile(_ context.Context, path string) ([]byte, error) {
	data, ok := m.files[filepath.Clean(path)]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (m *mockSourceReader) FileExists(_ context.Context, path string) (bool, error) {
	_, ok := m.files[filepath.Clean(path)]
	return ok, nil
}

type mockEnvReader struct {
	vars map[string]string
}

func (m *mockEnvReader) LookupEnv(key string) (string, bool) {
	val, ok := m.vars[key]
	return val, ok
}

type mockFileWriter struct {
	written map[string][]byte
	perms   map[string]fs.FileMode
}

func (m *mockFileWriter) WriteConfigFile(_ context.Context, path string, data []byte, perm fs.FileMode) error {
	if m.written == nil {
		m.written = make(map[string][]byte)
		m.perms = make(map[string]fs.FileMode)
	}
	clean := filepath.Clean(path)
	m.written[clean] = data
	m.perms[clean] = perm
	return nil
}

func TestServerConfig_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*config.ServerConfig)
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid dev default config",
			mutate:  func(_ *config.ServerConfig) {},
			wantErr: false,
		},
		{
			name: "valid prod default config",
			mutate: func(c *config.ServerConfig) {
				*c = *config.DefaultServerConfig(true)
				c.TLS.CertFile = "/etc/crucible/cert.pem"
				c.TLS.KeyFile = "/etc/crucible/key.pem"
			},
			wantErr: false,
		},
		{
			name: "invalid mode",
			mutate: func(c *config.ServerConfig) {
				c.Mode = "invalid_mode"
			},
			wantErr: true,
			errMsg:  "invalid daemon mode",
		},
		{
			name: "invalid http port zero",
			mutate: func(c *config.ServerConfig) {
				c.Server.HTTPPort = 0
			},
			wantErr: true,
			errMsg:  "invalid http_port",
		},
		{
			name: "invalid http port overflow",
			mutate: func(c *config.ServerConfig) {
				c.Server.HTTPPort = 70000
			},
			wantErr: true,
			errMsg:  "invalid http_port",
		},
		{
			name: "conflicting http and grpc ports",
			mutate: func(c *config.ServerConfig) {
				c.Server.HTTPPort = 8443
				c.Server.GRPCPort = 8443
			},
			wantErr: true,
			errMsg:  "cannot be identical",
		},
		{
			name: "empty database path",
			mutate: func(c *config.ServerConfig) {
				c.Storage.DatabasePath = "   "
			},
			wantErr: true,
			errMsg:  "database_path cannot be empty",
		},
		{
			name: "empty reports dir",
			mutate: func(c *config.ServerConfig) {
				c.Reports.Dir = ""
			},
			wantErr: true,
			errMsg:  "reports dir cannot be empty",
		},
		{
			name: "negative reports retention",
			mutate: func(c *config.ServerConfig) {
				c.Reports.RetentionDays = -1
			},
			wantErr: true,
			errMsg:  "retention_days cannot be negative",
		},
		{
			name: "invalid log level",
			mutate: func(c *config.ServerConfig) {
				c.Logging.Level = "verbose"
			},
			wantErr: true,
			errMsg:  "invalid logging level",
		},
		{
			name: "invalid log format",
			mutate: func(c *config.ServerConfig) {
				c.Logging.Format = "xml"
			},
			wantErr: true,
			errMsg:  "invalid logging format",
		},
		{
			name: "tls enabled without cert/key and without acme",
			mutate: func(c *config.ServerConfig) {
				c.TLS.Enabled = true
				c.TLS.CertFile = ""
				c.TLS.KeyFile = ""
				c.TLS.ACMEEnabled = false
			},
			wantErr: true,
			errMsg:  "tls is enabled but cert_file or key_file is missing",
		},
		{
			name: "acme enabled without domain",
			mutate: func(c *config.ServerConfig) {
				c.TLS.Enabled = true
				c.TLS.ACMEEnabled = true
				c.TLS.ACMEDomain = ""
			},
			wantErr: true,
			errMsg:  "acme_domain is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := config.DefaultServerConfig(false)
			tt.mutate(cfg)

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("cfg.Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("expected error containing %q, got: %v", tt.errMsg, err)
			}
		})
	}
}

func TestClientConfig_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*config.ClientConfig)
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid default client config",
			mutate:  func(_ *config.ClientConfig) {},
			wantErr: false,
		},
		{
			name: "empty profiles map",
			mutate: func(c *config.ClientConfig) {
				c.Profiles = nil
			},
			wantErr: true,
			errMsg:  "must contain at least one profile",
		},
		{
			name: "missing current profile key",
			mutate: func(c *config.ClientConfig) {
				c.CurrentProfile = "nonexistent"
			},
			wantErr: true,
			errMsg:  "does not exist in profiles",
		},
		{
			name: "invalid server url",
			mutate: func(c *config.ClientConfig) {
				prof := c.Profiles[c.CurrentProfile]
				prof.ServerURL = "ftp://invalid-scheme"
				c.Profiles[c.CurrentProfile] = prof
			},
			wantErr: true,
			errMsg:  "must start with http:// or https://",
		},
		{
			name: "negative timeout",
			mutate: func(c *config.ClientConfig) {
				prof := c.Profiles[c.CurrentProfile]
				prof.TimeoutSec = -5
				c.Profiles[c.CurrentProfile] = prof
			},
			wantErr: true,
			errMsg:  "timeout_sec cannot be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := config.DefaultClientConfig()
			tt.mutate(cfg)

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("cfg.Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("expected error containing %q, got: %v", tt.errMsg, err)
			}
		})
	}
}

func TestSystemdOptions_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*config.SystemdOptions)
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid default options",
			mutate:  func(_ *config.SystemdOptions) {},
			wantErr: false,
		},
		{
			name: "empty user",
			mutate: func(o *config.SystemdOptions) {
				o.User = ""
			},
			wantErr: true,
			errMsg:  "user cannot be empty",
		},
		{
			name: "empty exec_path",
			mutate: func(o *config.SystemdOptions) {
				o.ExecPath = ""
			},
			wantErr: true,
			errMsg:  "exec_path cannot be empty",
		},
		{
			name: "invalid port",
			mutate: func(o *config.SystemdOptions) {
				o.Port = 0
			},
			wantErr: true,
			errMsg:  "invalid systemd port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := config.DefaultSystemdOptions()
			tt.mutate(opts)

			err := opts.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("opts.Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("expected error containing %q, got: %v", tt.errMsg, err)
			}
		})
	}
}

func TestPrecedence_FourTiers(t *testing.T) {
	t.Parallel()

	// Tier 4: Built-in Defaults -> HTTPPort = 8443
	// Tier 3: YAML File         -> HTTPPort = 8080, Mode = "worker"
	// Tier 2: Environment       -> HTTPPort = 9090, LogLevel = "debug"
	// Tier 1: CLI Flags         -> HTTPPort = 7777

	mockSource := &mockSourceReader{
		files: map[string][]byte{
			"/etc/crucible/crucible.yaml": []byte(`
server:
  http_port: 8080
mode: "worker"
`),
		},
	}

	mockEnv := &mockEnvReader{
		vars: map[string]string{
			"CRUCIBLE_HTTP_PORT": "9090",
			"CRUCIBLE_LOG_LEVEL": "debug",
		},
	}

	svc, err := config.NewQueryService(mockSource, mockEnv, config.NewSystemdAdapter())
	if err != nil {
		t.Fatalf("NewQueryService error: %v", err)
	}

	ctx := context.Background()

	// Step 1: Base + YAML only (no env override, no flags)
	baseSvc, err := config.NewQueryService(mockSource, &mockEnvReader{vars: map[string]string{}}, config.NewSystemdAdapter())
	if err != nil {
		t.Fatalf("NewQueryService base error: %v", err)
	}
	cfgYAML, err := baseSvc.GetServerConfig(ctx, config.GetServerConfigQuery{
		ConfigFile: "/etc/crucible/crucible.yaml",
	})
	if err != nil {
		t.Fatalf("GetServerConfig error: %v", err)
	}
	if cfgYAML.Server.HTTPPort != 8080 {
		t.Errorf("Tier 3 failed: expected YAML port 8080, got %d", cfgYAML.Server.HTTPPort)
	}
	if cfgYAML.Mode != config.ModeWorker {
		t.Errorf("Tier 3 failed: expected YAML mode %q, got %q", config.ModeWorker, cfgYAML.Mode)
	}

	// Step 2: Env overrides YAML port
	cfgEnv, err := svc.GetServerConfig(ctx, config.GetServerConfigQuery{
		ConfigFile: "/etc/crucible/crucible.yaml",
	})
	if err != nil {
		t.Fatalf("GetServerConfig error: %v", err)
	}
	if cfgEnv.Server.HTTPPort != 9090 {
		t.Errorf("Tier 2 failed: expected Env port 9090, got %d", cfgEnv.Server.HTTPPort)
	}
	if cfgEnv.Logging.Level != "debug" {
		t.Errorf("Tier 2 failed: expected Env log level debug, got %s", cfgEnv.Logging.Level)
	}
	// Verify YAML mode was preserved through env layer
	if cfgEnv.Mode != config.ModeWorker {
		t.Errorf("Tier 2 failed: expected Mode worker preserved, got %s", cfgEnv.Mode)
	}

	// Step 3: Flag overrides Env port
	flagPort := 7777
	cfgFlag, err := svc.GetServerConfig(ctx, config.GetServerConfigQuery{
		ConfigFile: "/etc/crucible/crucible.yaml",
		Flags: config.ServerFlagOverrides{
			HTTPPort: &flagPort,
		},
	})
	if err != nil {
		t.Fatalf("GetServerConfig error: %v", err)
	}
	if cfgFlag.Server.HTTPPort != 7777 {
		t.Errorf("Tier 1 failed: expected Flag port 7777, got %d", cfgFlag.Server.HTTPPort)
	}
}

func TestQueryService_CustomConfigPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	customSource := &mockSourceReader{
		files: map[string][]byte{
			"/home/crucible/etc/crucible.yaml": []byte(`
server:
  http_port: 8888
mode: "controller"
`),
		},
	}
	customEnv := &mockEnvReader{
		vars: map[string]string{
			"CRUCIBLE_CONFIG": "/home/crucible/etc/crucible.yaml",
		},
	}
	customSvc, err := config.NewQueryService(customSource, customEnv, config.NewSystemdAdapter())
	if err != nil {
		t.Fatalf("customSvc error: %v", err)
	}
	cfgCustom, err := customSvc.GetServerConfig(ctx, config.GetServerConfigQuery{})
	if err != nil {
		t.Fatalf("GetServerConfig with CRUCIBLE_CONFIG error: %v", err)
	}
	if cfgCustom.Server.HTTPPort != 8888 {
		t.Errorf("expected port 8888 from custom path, got %d", cfgCustom.Server.HTTPPort)
	}
	if cfgCustom.Mode != config.ModeController {
		t.Errorf("expected mode controller from custom path, got %s", cfgCustom.Mode)
	}
}

func TestCommandService_Lifecycle(t *testing.T) {
	t.Parallel()

	mockSource := &mockSourceReader{
		files: make(map[string][]byte),
	}
	mockWriter := &mockFileWriter{}

	cmdSvc, err := config.NewCommandService(mockSource, mockWriter)
	if err != nil {
		t.Fatalf("NewCommandService error: %v", err)
	}

	ctx := context.Background()

	// 1. Init server config (dev)
	path, err := cmdSvc.InitServerConfig(ctx, config.InitServerConfigCommand{
		OutputPath:   "/tmp/test-crucible.yaml",
		IsProduction: false,
		Overwrite:    false,
	})
	if err != nil {
		t.Fatalf("InitServerConfig error: %v", err)
	}
	if path != "/tmp/test-crucible.yaml" {
		t.Errorf("unexpected output path: %s", path)
	}
	if _, ok := mockWriter.written["/tmp/test-crucible.yaml"]; !ok {
		t.Error("file was not written to mock writer")
	}

	// 2. Overwrite check without force
	mockSource.files["/tmp/test-crucible.yaml"] = mockWriter.written["/tmp/test-crucible.yaml"]
	_, err = cmdSvc.InitServerConfig(ctx, config.InitServerConfigCommand{
		OutputPath:   "/tmp/test-crucible.yaml",
		IsProduction: false,
		Overwrite:    false,
	})
	if err == nil {
		t.Error("expected error overwriting existing config without force, got nil")
	}

	// 3. Init client config
	clientPath, err := cmdSvc.InitClientConfig(ctx, config.InitClientConfigCommand{
		OutputPath: "/tmp/cruciblectl.yaml",
		ServerURL:  "https://remote.crucible.internal:8443",
		Token:      "pat_01JBCDEF...",
		Overwrite:  false,
	})
	if err != nil {
		t.Fatalf("InitClientConfig error: %v", err)
	}
	if clientPath != "/tmp/cruciblectl.yaml" {
		t.Errorf("unexpected client path: %s", clientPath)
	}
	if perm := mockWriter.perms["/tmp/cruciblectl.yaml"]; perm != 0600 {
		t.Errorf("expected client config permissions 0600, got: %v", perm)
	}

	// 4. Set client profile
	mockSource.files["/tmp/cruciblectl.yaml"] = mockWriter.written["/tmp/cruciblectl.yaml"]
	err = cmdSvc.SetClientProfile(ctx, config.SetClientProfileCommand{
		ConfigFile:  "/tmp/cruciblectl.yaml",
		ProfileName: "staging",
		Profile: config.ClientProfile{
			ServerURL:          "https://staging.internal:8443",
			Token:              "pat_staging_0123...",
			InsecureSkipVerify: true,
			TimeoutSec:         15,
		},
	})
	if err != nil {
		t.Fatalf("SetClientProfile error: %v", err)
	}
}

func TestSystemdAdapter_Render(t *testing.T) {
	t.Parallel()

	adapter := config.NewSystemdAdapter()
	opts := *config.DefaultSystemdOptions()
	opts.User = "crucible-service"
	opts.Group = "crucible-group"
	opts.ExecPath = "/usr/bin/crucibled"
	opts.ConfigPath = "/etc/crucible/crucible.yaml"

	unit, err := adapter.Render(opts)
	if err != nil {
		t.Fatalf("adapter.Render() error: %v", err)
	}

	expectedSubstrings := []string{
		"User=crucible-service",
		"Group=crucible-group",
		"ExecStart=/usr/bin/crucibled --config /etc/crucible/crucible.yaml",
		"AmbientCapabilities=CAP_NET_BIND_SERVICE",
		"CapabilityBoundingSet=CAP_NET_BIND_SERVICE",
		"StateDirectory=crucible",
		"RuntimeDirectory=crucibled",
		"ConfigurationDirectory=crucible",
		"LogsDirectory=crucible",
		"NoNewPrivileges=yes",
		"ProtectSystem=strict",
		"ProtectHome=yes",
		"PrivateTmp=yes",
	}

	for _, sub := range expectedSubstrings {
		if !strings.Contains(unit, sub) {
			t.Errorf("systemd unit missing expected directive %q:\n%s", sub, unit)
		}
	}
}

func TestPasswordPolicyConfig_ExpiryDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		interval string
		want     time.Duration
		wantErr  bool
	}{
		{name: "empty_disabled", interval: "", want: 0},
		{name: "zero_disabled", interval: "0", want: 0},
		{name: "off_disabled", interval: "off", want: 0},
		{name: "disabled_string", interval: "disabled", want: 0},
		{name: "90_days", interval: "90d", want: 90 * 24 * time.Hour},
		{name: "6_months", interval: "6m", want: 6 * 30 * 24 * time.Hour},
		{name: "1_year", interval: "1y", want: 365 * 24 * time.Hour},
		{name: "standard_hours", interval: "720h", want: 720 * time.Hour},
		{name: "invalid_format", interval: "invalid", wantErr: true},
		{name: "negative_days", interval: "-5d", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := config.PasswordPolicyConfig{ExpiryInterval: tt.interval}
			got, err := p.ExpiryDuration()
			if (err != nil) != tt.wantErr {
				t.Fatalf(
					"ExpiryDuration() error = %v, wantErr %v",
					err,
					tt.wantErr,
				)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ExpiryDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}
