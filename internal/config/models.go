// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

// Package config provides configuration models, hexagonal ports, CQRS
// services,
// adapters, and systemd unit generation for Crucible daemons and CLI tools.
package config

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Standard operational daemon modes.
const (
	ModeStandalone = "standalone"
	ModeController = "controller"
	ModeWorker     = "worker"
)

// Supported logging levels and output formats.
const (
	LogLevelDebug = "debug"
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"

	LogFormatConsole = "console"
	LogFormatJSON    = "json"
)

// Default paths and ports.
const (
	DefaultDevHTTPPort  = 8443
	DefaultProdHTTPPort = 443
	DefaultDevGRPCPort  = 9443
	DefaultProdGRPCPort = 8443

	DefaultDevBindHost  = "127.0.0.1"
	DefaultProdBindHost = "0.0.0.0"

	DefaultDevDBPath       = "./var/crucible.db"
	DefaultProdDBPath      = "/var/lib/crucible/crucible.db"
	DefaultDevSocketPath   = "./var/crucible.sock"
	DefaultProdSocketPath  = "/run/crucibled/crucible.sock"
	DefaultDevReportsDir   = "./var/reports"
	DefaultProdReportsDir  = "/var/lib/crucible/reports"
	DefaultDevKeyFilePath  = "./var/secrets.key"
	DefaultProdKeyFilePath = "/var/lib/crucible/secrets.key"
	DefaultDevGitWorkDir   = "./var/git"
	DefaultProdGitWorkDir  = "/var/lib/crucible/git"

	DefaultSystemdUser   = "crucible"
	DefaultSystemdGroup  = "crucible"
	DefaultSystemdExec   = "/usr/local/bin/crucibled"
	DefaultSystemdConfig = "/etc/crucible/crucible.yaml"
)

// ServerConfig encapsulates all runtime configuration parameters for
// crucibled.
type ServerConfig struct {
	Version string         `yaml:"version"`
	Mode    string         `yaml:"mode"`
	Server  ServerSettings `yaml:"server"`
	Storage StorageConfig  `yaml:"storage"`
	Reports ReportsConfig  `yaml:"reports"`
	Secrets SecretsConfig  `yaml:"secrets"`
	Git     GitConfig      `yaml:"git"`
	Cluster ClusterConfig  `yaml:"cluster"`
	Logging LoggingConfig  `yaml:"logging"`
	TLS     TLSConfig      `yaml:"tls"`
	Auth    AuthConfig     `yaml:"auth"`
}

// ServerSettings defines listener bindings and socket endpoints.
type ServerSettings struct {
	HTTPPort   int    `yaml:"http_port"`
	HTTPBind   string `yaml:"http_bind"`
	GRPCPort   int    `yaml:"grpc_port"`
	GRPCBind   string `yaml:"grpc_bind"`
	SocketPath string `yaml:"socket_path"`
}

// StorageConfig defines database path and SQLite engine options.
type StorageConfig struct {
	DatabasePath  string `yaml:"database_path"`
	WALMode       bool   `yaml:"wal_mode"`
	BusyTimeoutMs int    `yaml:"busy_timeout_ms"`
}

// ReportsConfig defines report directory and archival retention.
type ReportsConfig struct {
	Dir           string `yaml:"dir"`
	RetentionDays int    `yaml:"retention_days"`
}

// SecretsConfig defines master encryption key path and unseal behavior.
type SecretsConfig struct {
	KeyPath    string `yaml:"key_path"`
	AutoUnseal bool   `yaml:"auto_unseal"`
}

// GitConfig defines upstream repository sync settings.
type GitConfig struct {
	RepoURL         string `yaml:"repo_url"`
	Branch          string `yaml:"branch"`
	SyncIntervalSec int    `yaml:"sync_interval_sec"`
	WorkDir         string `yaml:"work_dir"`
}

// ClusterConfig defines multi-node clustering and heartbeat parameters.
type ClusterConfig struct {
	NodeID                    string   `yaml:"node_id"`
	Peers                     []string `yaml:"peers"`
	HeartbeatIntervalSec      int      `yaml:"heartbeat_interval_sec"`
	MissedHeartbeatsThreshold int      `yaml:"missed_heartbeats_threshold"`
}

// LoggingConfig defines log verbosity and serialization format.
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// TLSConfig defines TLS certificate sources and Let's Encrypt ACME parameters.
type TLSConfig struct {
	Enabled     bool   `yaml:"enabled"`
	CertFile    string `yaml:"cert_file"`
	KeyFile     string `yaml:"key_file"`
	ACMEEnabled bool   `yaml:"acme_enabled"`
	ACMEDomain  string `yaml:"acme_domain"`
	ACMEEmail   string `yaml:"acme_email"`
}

// PasswordPolicyConfig defines password rotation and complexity requirements.
type PasswordPolicyConfig struct {
	RequireChangeOnFirstLogin bool   `yaml:"require_change_on_first_login"`
	ExpiryInterval            string `yaml:"expiry_interval"`
	MinLength                 int    `yaml:"min_length"`
}

// ExpiryDuration parses ExpiryInterval into a time.Duration.
// Supports suffixes "d" (days), "m"/"mo" (months ~= 30d), "y" (years ~= 365d),
// standard Go durations, or "0"/"off"/"disabled" for zero duration.
func (p PasswordPolicyConfig) ExpiryDuration() (time.Duration, error) {
	val := strings.ToLower(strings.TrimSpace(p.ExpiryInterval))
	if val == "" || val == "0" || val == "off" || val == "disabled" ||
		val == "false" {
		return 0, nil
	}
	if dur, ok, err := parseCustomInterval(val); ok {
		return dur, err
	}
	dur, err := time.ParseDuration(val)
	if err != nil {
		return 0, fmt.Errorf(
			"invalid password expiry interval %q: %w",
			p.ExpiryInterval,
			err,
		)
	}
	if dur < 0 {
		return 0, fmt.Errorf(
			"password expiry interval %q cannot be negative",
			p.ExpiryInterval,
		)
	}
	return dur, nil
}

func parseCustomInterval(val string) (time.Duration, bool, error) {
	if strings.HasSuffix(val, "d") {
		d, err := strconv.Atoi(strings.TrimSuffix(val, "d"))
		if err != nil || d <= 0 {
			return 0, true, fmt.Errorf("invalid day duration %q: %w", val, err)
		}
		return time.Duration(d) * 24 * time.Hour, true, nil
	}
	if strings.HasSuffix(val, "mo") ||
		(strings.HasSuffix(val, "m") && !strings.Contains(val, "ms")) {
		numStr := strings.TrimSuffix(strings.TrimSuffix(val, "mo"), "m")
		m, err := strconv.Atoi(strings.TrimSpace(numStr))
		if err == nil && m > 0 {
			return time.Duration(m) * 30 * 24 * time.Hour, true, nil
		}
	}
	if strings.HasSuffix(val, "y") {
		y, err := strconv.Atoi(strings.TrimSuffix(val, "y"))
		if err != nil || y <= 0 {
			return 0, true, fmt.Errorf("invalid year duration %q: %w", val, err)
		}
		return time.Duration(y) * 365 * 24 * time.Hour, true, nil
	}
	return 0, false, nil
}

// AuthConfig defines operator authentication and group mapping policies.
type AuthConfig struct {
	Provider         string               `yaml:"provider"`
	Require2FA       bool                 `yaml:"require_2fa"`
	PasswordPolicy   PasswordPolicyConfig `yaml:"password_policy"`
	AdminGroups      []string             `yaml:"admin_groups"`
	OperatorGroups   []string             `yaml:"operator_groups"`
	RestrictToGroups bool                 `yaml:"restrict_to_groups"`
}

// ClientConfig encapsulates client connection profiles for cruciblectl.
type ClientConfig struct {
	Version        string                   `yaml:"version"`
	CurrentProfile string                   `yaml:"current_profile"`
	Profiles       map[string]ClientProfile `yaml:"profiles"`
}

// ClientProfile defines target daemon connection credentials.
type ClientProfile struct {
	ServerURL          string `yaml:"server_url"`
	Token              string `yaml:"token"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
	TimeoutSec         int    `yaml:"timeout_sec"`
}

// SystemdOptions defines parameters for generating systemd service units.
type SystemdOptions struct {
	User                string
	Group               string
	ExecPath            string
	ConfigPath          string
	Port                int
	AmbientCapabilities bool
}

// DefaultServerConfig generates a complete ServerConfig populated with safe
// defaults.
func DefaultServerConfig(prod bool) *ServerConfig {
	cfg := &ServerConfig{
		Version: "1",
		Mode:    ModeStandalone,
		Server: ServerSettings{
			HTTPPort:   DefaultDevHTTPPort,
			HTTPBind:   DefaultDevBindHost,
			GRPCPort:   DefaultDevGRPCPort,
			GRPCBind:   DefaultDevBindHost,
			SocketPath: filepath.Clean(DefaultDevSocketPath),
		},
		Storage: StorageConfig{
			DatabasePath:  filepath.Clean(DefaultDevDBPath),
			WALMode:       true,
			BusyTimeoutMs: 5000,
		},
		Reports: ReportsConfig{
			Dir:           filepath.Clean(DefaultDevReportsDir),
			RetentionDays: 90,
		},
		Secrets: SecretsConfig{
			KeyPath:    filepath.Clean(DefaultDevKeyFilePath),
			AutoUnseal: false,
		},
		Git: GitConfig{
			RepoURL:         "",
			Branch:          "main",
			SyncIntervalSec: 300,
			WorkDir:         filepath.Clean(DefaultDevGitWorkDir),
		},
		Cluster: ClusterConfig{
			NodeID:                    "",
			Peers:                     []string{},
			HeartbeatIntervalSec:      3,
			MissedHeartbeatsThreshold: 3,
		},
		Logging: LoggingConfig{
			Level:  LogLevelInfo,
			Format: LogFormatConsole,
		},
		TLS: TLSConfig{
			Enabled:     false,
			CertFile:    "",
			KeyFile:     "",
			ACMEEnabled: false,
			ACMEDomain:  "",
			ACMEEmail:   "",
		},
		Auth: AuthConfig{
			Provider:   "sqlite",
			Require2FA: true,
			PasswordPolicy: PasswordPolicyConfig{
				RequireChangeOnFirstLogin: true,
				ExpiryInterval:            "0",
				MinLength:                 12,
			},
			AdminGroups: []string{
				"root",
				"wheel",
				"sudo",
				"adm",
				"crucible-admin",
			},
			OperatorGroups: []string{
				"operator",
				"crucible-operator",
			},
			RestrictToGroups: true,
		},
	}

	if prod {
		cfg.Server.HTTPPort = DefaultProdHTTPPort
		cfg.Server.HTTPBind = DefaultProdBindHost
		cfg.Server.GRPCPort = DefaultProdGRPCPort
		cfg.Server.GRPCBind = DefaultProdBindHost
		cfg.Server.SocketPath = filepath.Clean(DefaultProdSocketPath)

		cfg.Storage.DatabasePath = filepath.Clean(DefaultProdDBPath)
		cfg.Reports.Dir = filepath.Clean(DefaultProdReportsDir)
		cfg.Secrets.KeyPath = filepath.Clean(DefaultProdKeyFilePath)
		cfg.Git.WorkDir = filepath.Clean(DefaultProdGitWorkDir)
		cfg.Logging.Format = LogFormatJSON
		cfg.TLS.Enabled = true
	}

	return cfg
}

// DefaultClientConfig generates a default ClientConfig with an initial local
// profile.
func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		Version:        "1",
		CurrentProfile: "default",
		Profiles: map[string]ClientProfile{
			"default": {
				ServerURL:          "https://127.0.0.1:8443",
				Token:              "",
				InsecureSkipVerify: false,
				TimeoutSec:         30,
			},
		},
	}
}

// DefaultSystemdOptions generates standard production systemd service options.
func DefaultSystemdOptions() *SystemdOptions {
	return &SystemdOptions{
		User:                DefaultSystemdUser,
		Group:               DefaultSystemdGroup,
		ExecPath:            DefaultSystemdExec,
		ConfigPath:          DefaultSystemdConfig,
		Port:                DefaultProdHTTPPort,
		AmbientCapabilities: true,
	}
}

// Validate verifies domain invariants for ServerConfig.
func (c *ServerConfig) Validate() error {
	if c == nil {
		return errors.New("server configuration is nil")
	}

	if err := c.validateModeAndNetwork(); err != nil {
		return err
	}

	if err := c.validateStorageAndReports(); err != nil {
		return err
	}

	if err := c.validateLogging(); err != nil {
		return err
	}

	if err := c.validateTLS(); err != nil {
		return err
	}

	return c.validateAuth()
}

func (c *ServerConfig) validateModeAndNetwork() error {
	switch c.Mode {
	case ModeStandalone, ModeController, ModeWorker:
	default:
		return fmt.Errorf("invalid daemon mode %q: must be standalone, controller, or worker", c.Mode)
	}

	if c.Server.HTTPPort <= 0 || c.Server.HTTPPort > 65535 {
		return fmt.Errorf("invalid http_port %d: must be between 1 and 65535", c.Server.HTTPPort)
	}

	if c.Server.GRPCPort <= 0 || c.Server.GRPCPort > 65535 {
		return fmt.Errorf("invalid grpc_port %d: must be between 1 and 65535", c.Server.GRPCPort)
	}

	if c.Server.HTTPPort == c.Server.GRPCPort {
		return errors.New("http_port and grpc_port cannot be identical")
	}

	return nil
}

func (c *ServerConfig) validateStorageAndReports() error {
	if strings.TrimSpace(c.Storage.DatabasePath) == "" {
		return errors.New("storage database_path cannot be empty")
	}

	if strings.TrimSpace(c.Reports.Dir) == "" {
		return errors.New("reports dir cannot be empty")
	}

	if c.Reports.RetentionDays < 0 {
		return errors.New("reports retention_days cannot be negative")
	}

	return nil
}

func (c *ServerConfig) validateLogging() error {
	switch c.Logging.Level {
	case LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
	default:
		return fmt.Errorf("invalid logging level %q: must be debug, info, warn, or error", c.Logging.Level)
	}

	switch c.Logging.Format {
	case LogFormatConsole, LogFormatJSON:
	default:
		return fmt.Errorf("invalid logging format %q: must be console or json", c.Logging.Format)
	}

	return nil
}

func (c *ServerConfig) validateTLS() error {
	if c.TLS.Enabled && !c.TLS.ACMEEnabled {
		if c.TLS.CertFile == "" || c.TLS.KeyFile == "" {
			return errors.New("tls is enabled but cert_file or key_file is missing (and acme_enabled is false)")
		}
	}

	if c.TLS.ACMEEnabled && strings.TrimSpace(c.TLS.ACMEDomain) == "" {
		return errors.New("acme_enabled is true but acme_domain is empty")
	}

	return nil
}

func (c *ServerConfig) validateAuth() error {
	p := strings.ToLower(strings.TrimSpace(c.Auth.Provider))
	if p != "" && p != "sqlite" && p != "pam" {
		return fmt.Errorf(
			"invalid auth provider %q: must be sqlite or pam",
			c.Auth.Provider,
		)
	}
	if c.Auth.PasswordPolicy.MinLength < 0 {
		return errors.New("password policy min_length cannot be negative")
	}
	if _, err := c.Auth.PasswordPolicy.ExpiryDuration(); err != nil {
		return err
	}
	return nil
}

// Validate verifies domain invariants for ClientConfig.
func (c *ClientConfig) Validate() error {
	if c == nil {
		return errors.New("client configuration is nil")
	}

	if len(c.Profiles) == 0 {
		return errors.New("client configuration must contain at least one profile")
	}

	if c.CurrentProfile == "" {
		return errors.New("current_profile cannot be empty")
	}

	profile, ok := c.Profiles[c.CurrentProfile]
	if !ok {
		return fmt.Errorf("current_profile %q does not exist in profiles", c.CurrentProfile)
	}

	for name, prof := range c.Profiles {
		if err := prof.Validate(); err != nil {
			return fmt.Errorf("profile %q is invalid: %w", name, err)
		}
	}

	return profile.Validate()
}

// Validate verifies domain invariants for ClientProfile.
func (p *ClientProfile) Validate() error {
	raw := strings.TrimSpace(p.ServerURL)
	if raw == "" {
		return errors.New("server_url cannot be empty")
	}

	var hostPart string
	switch {
	case strings.HasPrefix(raw, "http://"):
		hostPart = strings.TrimPrefix(raw, "http://")
	case strings.HasPrefix(raw, "https://"):
		hostPart = strings.TrimPrefix(raw, "https://")
	default:
		return fmt.Errorf("invalid server_url %q: must start with http:// or https://", raw)
	}

	hostPart = strings.TrimSpace(hostPart)
	if hostPart == "" || hostPart == "/" {
		return fmt.Errorf("invalid server_url %q: host address cannot be empty", raw)
	}

	if p.TimeoutSec < 0 {
		return errors.New("timeout_sec cannot be negative")
	}

	return nil
}

// Validate verifies domain invariants for SystemdOptions.
func (o *SystemdOptions) Validate() error {
	if o == nil {
		return errors.New("systemd options are nil")
	}

	if strings.TrimSpace(o.User) == "" {
		return errors.New("systemd user cannot be empty")
	}

	if strings.TrimSpace(o.Group) == "" {
		return errors.New("systemd group cannot be empty")
	}

	if strings.TrimSpace(o.ExecPath) == "" {
		return errors.New("systemd exec_path cannot be empty")
	}

	if strings.TrimSpace(o.ConfigPath) == "" {
		return errors.New("systemd config_path cannot be empty")
	}

	if o.Port <= 0 || o.Port > 65535 {
		return fmt.Errorf("invalid systemd port %d: must be between 1 and 65535", o.Port)
	}

	return nil
}
