// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package config

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ServerFlagOverrides captures command-line flag values overriding config and
// environment.
type ServerFlagOverrides struct {
	Mode           *string
	HTTPPort       *int
	HTTPBind       *string
	GRPCPort       *int
	GRPCBind       *string
	SocketPath     *string
	DatabasePath   *string
	ReportsDir     *string
	SecretsKeyPath *string
	LogLevel       *string
	LogFormat      *string
	GitRepoURL     *string
	GitBranch      *string
	Require2FA     *bool
}

// ClientFlagOverrides captures CLI flag overrides for client profiles.
type ClientFlagOverrides struct {
	ServerURL          *string
	Token              *string
	InsecureSkipVerify *bool
	TimeoutSec         *int
}

// GetServerConfigQuery carries parameters for resolving an effective
// ServerConfig.
type GetServerConfigQuery struct {
	ConfigFile     string
	Flags          ServerFlagOverrides
	IsProduction   bool
	SkipValidation bool
}

// GetClientConfigQuery carries parameters for resolving an effective
// ClientConfig.
type GetClientConfigQuery struct {
	ConfigFile     string
	Profile        string
	Flags          ClientFlagOverrides
	SkipValidation bool
}

// ValidateConfigQuery carries target configuration file path for validation.
type ValidateConfigQuery struct {
	ConfigFile string
}

// GenerateSystemdUnitQuery carries options to generate a systemd unit.
type GenerateSystemdUnitQuery struct {
	Options SystemdOptions
}

// QueryService implements the QueryHandler inbound port.
type QueryService struct {
	sourceReader    SourceReader
	envReader       EnvReader
	systemdRenderer SystemdUnitRenderer
}

// NewQueryService constructs a new QueryService backed by provided adapters.
func NewQueryService(
	sourceReader SourceReader,
	envReader EnvReader,
	systemdRenderer SystemdUnitRenderer,
) (*QueryService, error) {
	if sourceReader == nil {
		return nil, errors.New("source reader cannot be nil")
	}
	if envReader == nil {
		return nil, errors.New("env reader cannot be nil")
	}
	if systemdRenderer == nil {
		return nil, errors.New("systemd renderer cannot be nil")
	}
	return &QueryService{
		sourceReader:    sourceReader,
		envReader:       envReader,
		systemdRenderer: systemdRenderer,
	}, nil
}

// DefaultQueryService initializes a QueryService with standard OS adapters.
func DefaultQueryService() *QueryService {
	svc, err := NewQueryService(NewOSFileSystemAdapter(), NewOSEnvironmentAdapter(), NewSystemdAdapter())
	if err != nil {
		panic(err)
	}
	return svc
}

// GetServerConfig resolves effective ServerConfig using 4-tier precedence:
// Flags > Environment Variables > YAML File > Defaults.
func (s *QueryService) GetServerConfig(ctx context.Context, query GetServerConfigQuery) (*ServerConfig, error) {
	cfg := DefaultServerConfig(query.IsProduction)

	configFile, err := s.resolveServerConfigFile(ctx, query.ConfigFile)
	if err != nil {
		return nil, fmt.Errorf("resolving server config path: %w", err)
	}

	if configFile != "" {
		data, err := s.sourceReader.ReadConfigFile(ctx, configFile)
		if err != nil {
			return nil, fmt.Errorf("reading server config file: %w", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing server config YAML: %w", err)
		}
	}

	s.applyServerEnv(cfg)
	s.applyServerFlags(cfg, query.Flags)

	cfg.Server.SocketPath = filepath.Clean(cfg.Server.SocketPath)
	cfg.Storage.DatabasePath = filepath.Clean(cfg.Storage.DatabasePath)
	cfg.Reports.Dir = filepath.Clean(cfg.Reports.Dir)
	cfg.Secrets.KeyPath = filepath.Clean(cfg.Secrets.KeyPath)
	if cfg.Git.WorkDir != "" {
		cfg.Git.WorkDir = filepath.Clean(cfg.Git.WorkDir)
	}

	if !query.SkipValidation {
		if err := cfg.Validate(); err != nil {
			return nil, fmt.Errorf("server config validation failed: %w", err)
		}
	}

	return cfg, nil
}

// GetClientConfig resolves effective ClientConfig and active profile.
func (s *QueryService) GetClientConfig(ctx context.Context, query GetClientConfigQuery) (*ClientConfig, error) {
	cfg := DefaultClientConfig()

	if query.ConfigFile != "" {
		data, err := s.sourceReader.ReadConfigFile(ctx, filepath.Clean(query.ConfigFile))
		if err != nil {
			return nil, fmt.Errorf("reading client config file: %w", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing client config YAML: %w", err)
		}
	}

	if query.Profile != "" {
		cfg.CurrentProfile = query.Profile
	}

	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]ClientProfile)
	}

	active, exists := cfg.Profiles[cfg.CurrentProfile]
	if !exists {
		active = ClientProfile{
			ServerURL:  "https://127.0.0.1:8443",
			TimeoutSec: 30,
		}
	}

	s.applyClientEnv(&active)
	s.applyClientFlags(&active, query.Flags)

	cfg.Profiles[cfg.CurrentProfile] = active

	if !query.SkipValidation {
		if err := cfg.Validate(); err != nil {
			return nil, fmt.Errorf("client config validation failed: %w", err)
		}
	}

	return cfg, nil
}

func (s *QueryService) applyClientEnv(active *ClientProfile) {
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_SERVER_URL"); ok && strings.TrimSpace(val) != "" {
		active.ServerURL = strings.TrimSpace(val)
	}
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_TOKEN"); ok && strings.TrimSpace(val) != "" {
		active.Token = strings.TrimSpace(val)
	}
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_INSECURE_SKIP_VERIFY"); ok {
		if b, err := strconv.ParseBool(val); err == nil {
			active.InsecureSkipVerify = b
		}
	}
}

func (s *QueryService) applyClientFlags(active *ClientProfile, flags ClientFlagOverrides) {
	if flags.ServerURL != nil && strings.TrimSpace(*flags.ServerURL) != "" {
		active.ServerURL = strings.TrimSpace(*flags.ServerURL)
	}
	if flags.Token != nil && strings.TrimSpace(*flags.Token) != "" {
		active.Token = strings.TrimSpace(*flags.Token)
	}
	if flags.InsecureSkipVerify != nil {
		active.InsecureSkipVerify = *flags.InsecureSkipVerify
	}
	if flags.TimeoutSec != nil && *flags.TimeoutSec > 0 {
		active.TimeoutSec = *flags.TimeoutSec
	}
}

// ValidateServerConfig reads and validates a server configuration file.
func (s *QueryService) ValidateServerConfig(ctx context.Context, query ValidateConfigQuery) error {
	if query.ConfigFile == "" {
		return errors.New("config file path cannot be empty")
	}

	data, err := s.sourceReader.ReadConfigFile(ctx, filepath.Clean(query.ConfigFile))
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}

	var cfg ServerConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parsing YAML syntax: %w", err)
	}

	return cfg.Validate()
}

// ValidateClientConfig reads and validates a client configuration file.
func (s *QueryService) ValidateClientConfig(ctx context.Context, query ValidateConfigQuery) error {
	if query.ConfigFile == "" {
		return errors.New("config file path cannot be empty")
	}

	data, err := s.sourceReader.ReadConfigFile(ctx, filepath.Clean(query.ConfigFile))
	if err != nil {
		return fmt.Errorf("reading config file: %w", err)
	}

	var cfg ClientConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parsing YAML syntax: %w", err)
	}

	return cfg.Validate()
}

// GenerateSystemdUnit generates a hardened production systemd unit service
// string.
func (s *QueryService) GenerateSystemdUnit(_ context.Context, query GenerateSystemdUnitQuery) (string, error) {
	if err := query.Options.Validate(); err != nil {
		return "", fmt.Errorf("systemd options invalid: %w", err)
	}

	return s.systemdRenderer.Render(query.Options)
}

func (s *QueryService) applyServerEnv(cfg *ServerConfig) {
	s.applyServerEnvNet(cfg)
	s.applyServerEnvStorage(cfg)
	s.applyServerEnvCluster(cfg)
	s.applyServerEnvTLS(cfg)
	s.applyServerEnvAuth(cfg)
}

func (s *QueryService) applyServerEnvNet(cfg *ServerConfig) {
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_MODE"); ok && val != "" {
		cfg.Mode = val
	}
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_HTTP_PORT"); ok {
		if p, err := strconv.Atoi(val); err == nil && p > 0 {
			cfg.Server.HTTPPort = p
		}
	}
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_HTTP_BIND"); ok && val != "" {
		cfg.Server.HTTPBind = val
	}
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_GRPC_PORT"); ok {
		if p, err := strconv.Atoi(val); err == nil && p > 0 {
			cfg.Server.GRPCPort = p
		}
	}
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_GRPC_BIND"); ok && val != "" {
		cfg.Server.GRPCBind = val
	}
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_SOCKET_PATH"); ok && val != "" {
		cfg.Server.SocketPath = val
	}
}

func (s *QueryService) applyServerEnvStorage(cfg *ServerConfig) {
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_DATABASE_PATH"); ok && val != "" {
		cfg.Storage.DatabasePath = val
	}
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_REPORTS_DIR"); ok && val != "" {
		cfg.Reports.Dir = val
	}
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_SECRETS_KEY_PATH"); ok && val != "" {
		cfg.Secrets.KeyPath = val
	}
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_LOG_LEVEL"); ok && val != "" {
		cfg.Logging.Level = val
	}
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_LOG_FORMAT"); ok && val != "" {
		cfg.Logging.Format = val
	}
}

func (s *QueryService) applyServerEnvCluster(cfg *ServerConfig) {
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_GIT_REPO_URL"); ok && val != "" {
		cfg.Git.RepoURL = val
	}
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_GIT_BRANCH"); ok && val != "" {
		cfg.Git.Branch = val
	}
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_CLUSTER_NODE_ID"); ok && val != "" {
		cfg.Cluster.NodeID = val
	}
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_CLUSTER_PEERS"); ok && val != "" {
		cfg.Cluster.Peers = strings.Split(val, ",")
	}
}

func (s *QueryService) applyServerEnvTLS(cfg *ServerConfig) {
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_TLS_ENABLED"); ok {
		if b, err := strconv.ParseBool(val); err == nil {
			cfg.TLS.Enabled = b
		}
	}
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_TLS_CERT"); ok && val != "" {
		cfg.TLS.CertFile = val
	}
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_TLS_KEY"); ok && val != "" {
		cfg.TLS.KeyFile = val
	}
}

func (s *QueryService) applyServerEnvAuth(cfg *ServerConfig) {
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_AUTH_REQUIRE_2FA"); ok {
		if b, err := strconv.ParseBool(val); err == nil {
			cfg.Auth.Require2FA = b
		}
	}
	if val, ok := s.envReader.LookupEnv("CRUCIBLE_AUTH_PROVIDER"); ok &&
		val != "" {
		cfg.Auth.Provider = val
	}
}

func (s *QueryService) applyServerFlags(cfg *ServerConfig, flags ServerFlagOverrides) {
	s.applyServerFlagsNet(cfg, flags)
	s.applyServerFlagsStorage(cfg, flags)
	s.applyServerFlagsMeta(cfg, flags)
}

func (s *QueryService) applyServerFlagsNet(cfg *ServerConfig, flags ServerFlagOverrides) {
	if flags.Mode != nil && *flags.Mode != "" {
		cfg.Mode = *flags.Mode
	}
	if flags.HTTPPort != nil && *flags.HTTPPort > 0 {
		cfg.Server.HTTPPort = *flags.HTTPPort
	}
	if flags.HTTPBind != nil && *flags.HTTPBind != "" {
		cfg.Server.HTTPBind = *flags.HTTPBind
	}
	if flags.GRPCPort != nil && *flags.GRPCPort > 0 {
		cfg.Server.GRPCPort = *flags.GRPCPort
	}
	if flags.GRPCBind != nil && *flags.GRPCBind != "" {
		cfg.Server.GRPCBind = *flags.GRPCBind
	}
	if flags.SocketPath != nil && *flags.SocketPath != "" {
		cfg.Server.SocketPath = *flags.SocketPath
	}
}

func (s *QueryService) applyServerFlagsStorage(cfg *ServerConfig, flags ServerFlagOverrides) {
	if flags.DatabasePath != nil && *flags.DatabasePath != "" {
		cfg.Storage.DatabasePath = *flags.DatabasePath
	}
	if flags.ReportsDir != nil && *flags.ReportsDir != "" {
		cfg.Reports.Dir = *flags.ReportsDir
	}
	if flags.SecretsKeyPath != nil && *flags.SecretsKeyPath != "" {
		cfg.Secrets.KeyPath = *flags.SecretsKeyPath
	}
}

func (s *QueryService) applyServerFlagsMeta(cfg *ServerConfig, flags ServerFlagOverrides) {
	if flags.LogLevel != nil && *flags.LogLevel != "" {
		cfg.Logging.Level = *flags.LogLevel
	}
	if flags.LogFormat != nil && *flags.LogFormat != "" {
		cfg.Logging.Format = *flags.LogFormat
	}
	if flags.GitRepoURL != nil && *flags.GitRepoURL != "" {
		cfg.Git.RepoURL = *flags.GitRepoURL
	}
	if flags.GitBranch != nil && *flags.GitBranch != "" {
		cfg.Git.Branch = *flags.GitBranch
	}
	if flags.Require2FA != nil {
		cfg.Auth.Require2FA = *flags.Require2FA
	}
}

func (s *QueryService) resolveServerConfigFile(ctx context.Context, explicit string) (string, error) {
	if explicit != "" {
		return filepath.Clean(explicit), nil
	}

	if envPath, ok := s.envReader.LookupEnv("CRUCIBLE_CONFIG"); ok {
		clean := strings.TrimSpace(envPath)
		if clean != "" {
			return filepath.Clean(clean), nil
		}
	}

	if exists, err := s.sourceReader.FileExists(ctx, "crucible.yaml"); err == nil && exists {
		return "crucible.yaml", nil
	}

	if exists, err := s.sourceReader.FileExists(ctx, DefaultSystemdConfig); err == nil && exists {
		return DefaultSystemdConfig, nil
	}

	return "", nil
}
