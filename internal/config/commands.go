// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// InitServerConfigCommand carries input parameters for creating a server
// configuration.
type InitServerConfigCommand struct {
	OutputPath   string
	IsProduction bool
	Overwrite    bool
}

// InitClientConfigCommand carries input parameters for creating a client
// configuration.
type InitClientConfigCommand struct {
	OutputPath string
	ServerURL  string
	Token      string
	Overwrite  bool
}

// SetClientProfileCommand carries input parameters for updating client
// profiles.
type SetClientProfileCommand struct {
	ConfigFile  string
	ProfileName string
	Profile     ClientProfile
}

// CommandService implements the CommandHandler inbound port.
type CommandService struct {
	sourceReader SourceReader
	fileWriter   FileWriter
}

// NewCommandService constructs a CommandService backed by provided adapters.
func NewCommandService(sourceReader SourceReader, fileWriter FileWriter) (*CommandService, error) {
	if sourceReader == nil {
		return nil, errors.New("source reader cannot be nil")
	}
	if fileWriter == nil {
		return nil, errors.New("file writer cannot be nil")
	}
	return &CommandService{
		sourceReader: sourceReader,
		fileWriter:   fileWriter,
	}, nil
}

// DefaultCommandService initializes a CommandService with standard OS
// adapters.
func DefaultCommandService() *CommandService {
	adapter := NewOSFileSystemAdapter()
	svc, err := NewCommandService(adapter, adapter)
	if err != nil {
		panic(err)
	}
	return svc
}

// InitServerConfig generates and writes a commented YAML configuration
// template.
func (s *CommandService) InitServerConfig(ctx context.Context, cmd InitServerConfigCommand) (string, error) {
	targetPath := cmd.OutputPath
	if targetPath == "" {
		if cmd.IsProduction {
			targetPath = DefaultSystemdConfig
		} else {
			targetPath = "crucible.yaml"
		}
	}
	targetPath = filepath.Clean(targetPath)

	exists, err := s.sourceReader.FileExists(ctx, targetPath)
	if err != nil {
		return "", fmt.Errorf("checking target file existence: %w", err)
	}
	if exists && !cmd.Overwrite {
		return "", fmt.Errorf("file %q already exists; use --force to overwrite", targetPath)
	}

	content := s.renderServerTemplate(cmd.IsProduction)
	if err := s.fileWriter.WriteConfigFile(ctx, targetPath, []byte(content), 0640); err != nil {
		return "", fmt.Errorf("writing server config: %w", err)
	}

	return targetPath, nil
}

// InitClientConfig generates and writes a client configuration template.
func (s *CommandService) InitClientConfig(ctx context.Context, cmd InitClientConfigCommand) (string, error) {
	targetPath := cmd.OutputPath
	if targetPath == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			targetPath = "cruciblectl.yaml"
		} else {
			targetPath = filepath.Join(home, ".config", "crucible", "cruciblectl.yaml")
		}
	}
	targetPath = filepath.Clean(targetPath)

	exists, err := s.sourceReader.FileExists(ctx, targetPath)
	if err != nil {
		return "", fmt.Errorf("checking target file existence: %w", err)
	}
	if exists && !cmd.Overwrite {
		return "", fmt.Errorf("file %q already exists; use --force to overwrite", targetPath)
	}

	serverURL := cmd.ServerURL
	if serverURL == "" {
		serverURL = "https://127.0.0.1:8443"
	}

	content := s.renderClientTemplate(serverURL, cmd.Token)
	// Mode 0600 strictly protects sensitive API tokens
	if err := s.fileWriter.WriteConfigFile(ctx, targetPath, []byte(content), 0600); err != nil {
		return "", fmt.Errorf("writing client config: %w", err)
	}

	return targetPath, nil
}

// SetClientProfile adds or updates a named profile in the client config file.
func (s *CommandService) SetClientProfile(ctx context.Context, cmd SetClientProfileCommand) error {
	if cmd.ConfigFile == "" {
		return errors.New("config file path cannot be empty")
	}
	if cmd.ProfileName == "" {
		return errors.New("profile name cannot be empty")
	}
	if err := cmd.Profile.Validate(); err != nil {
		return fmt.Errorf("invalid profile: %w", err)
	}

	targetPath := filepath.Clean(cmd.ConfigFile)
	cfg := DefaultClientConfig()

	exists, err := s.sourceReader.FileExists(ctx, targetPath)
	if err != nil {
		return fmt.Errorf("checking config existence: %w", err)
	}

	if exists {
		data, readErr := s.sourceReader.ReadConfigFile(ctx, targetPath)
		if readErr != nil {
			return fmt.Errorf("reading client config: %w", readErr)
		}
		if unmarshalErr := yaml.Unmarshal(data, cfg); unmarshalErr != nil {
			return fmt.Errorf("parsing client config YAML: %w", unmarshalErr)
		}
	}

	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]ClientProfile)
	}
	cfg.Profiles[cmd.ProfileName] = cmd.Profile
	cfg.CurrentProfile = cmd.ProfileName

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("serializing client config: %w", err)
	}

	header := "# SPDX-License-Identifier: AGPL-3.0-or-later\n" +
		"# Copyright (c) 2026 The GotCode Collective\n" +
		"# Crucible Operator CLI Configuration\n\n"
	fullData := append([]byte(header), out...)

	return s.fileWriter.WriteConfigFile(ctx, targetPath, fullData, 0600)
}

func (s *CommandService) renderServerTemplate(prod bool) string {
	httpPort := DefaultDevHTTPPort
	httpBind := DefaultDevBindHost
	grpcPort := DefaultDevGRPCPort
	grpcBind := DefaultDevBindHost
	sockPath := DefaultDevSocketPath
	dbPath := DefaultDevDBPath
	reportsDir := DefaultDevReportsDir
	secretsKey := DefaultDevKeyFilePath
	gitWorkDir := DefaultDevGitWorkDir
	logFormat := LogFormatConsole
	tlsEnabled := false

	if prod {
		httpPort = DefaultProdHTTPPort
		httpBind = DefaultProdBindHost
		grpcPort = DefaultProdGRPCPort
		grpcBind = DefaultProdBindHost
		sockPath = DefaultProdSocketPath
		dbPath = DefaultProdDBPath
		reportsDir = DefaultProdReportsDir
		secretsKey = DefaultProdKeyFilePath
		gitWorkDir = DefaultProdGitWorkDir
		logFormat = LogFormatJSON
		tlsEnabled = true
	}

	var sb strings.Builder
	sb.WriteString("# SPDX-License-Identifier: AGPL-3.0-or-later\n")
	sb.WriteString("# Copyright (c) 2026 The GotCode Collective\n")
	sb.WriteString("# See the LICENSE file in the project root for full license details.\n")
	sb.WriteString("#\n")
	sb.WriteString("# Crucible Automation Server Daemon Configuration\n")
	sb.WriteString("version: \"1\"\n")
	sb.WriteString("mode: \"standalone\" # standalone | controller | worker\n\n")

	sb.WriteString("# Server network bindings and local UNIX socket\n")
	sb.WriteString("server:\n")
	sb.WriteString(fmt.Sprintf("  http_port: %d\n", httpPort))
	sb.WriteString(fmt.Sprintf("  http_bind: \"%s\"\n", httpBind))
	sb.WriteString(fmt.Sprintf("  grpc_port: %d\n", grpcPort))
	sb.WriteString(fmt.Sprintf("  grpc_bind: \"%s\"\n", grpcBind))
	sb.WriteString(fmt.Sprintf("  socket_path: \"%s\"\n\n", sockPath))

	sb.WriteString("# SQLite storage settings (pure Go modernc.org/sqlite, CGO_ENABLED=0)\n")
	sb.WriteString("storage:\n")
	sb.WriteString(fmt.Sprintf("  database_path: \"%s\"\n", dbPath))
	sb.WriteString("  wal_mode: true\n")
	sb.WriteString("  busy_timeout_ms: 5000\n\n")

	sb.WriteString("# Execution logs and published HTML reports\n")
	sb.WriteString("reports:\n")
	sb.WriteString(fmt.Sprintf("  dir: \"%s\"\n", reportsDir))
	sb.WriteString("  retention_days: 90\n\n")

	sb.WriteString("# Cryptographic envelope secrets storage\n")
	sb.WriteString("secrets:\n")
	sb.WriteString(fmt.Sprintf("  key_path: \"%s\"\n", secretsKey))
	sb.WriteString("  auto_unseal: false\n\n")

	sb.WriteString("# Upstream Git repository synchronization\n")
	sb.WriteString("git:\n")
	sb.WriteString("  repo_url: \"\"\n")
	sb.WriteString("  branch: \"main\"\n")
	sb.WriteString("  sync_interval_sec: 300\n")
	sb.WriteString(fmt.Sprintf("  work_dir: \"%s\"\n\n", gitWorkDir))

	sb.WriteString("# Multi-node cluster configuration\n")
	sb.WriteString("cluster:\n")
	sb.WriteString("  node_id: \"\"\n")
	sb.WriteString("  peers: []\n")
	sb.WriteString("  heartbeat_interval_sec: 3\n")
	sb.WriteString("  missed_heartbeats_threshold: 3\n\n")

	sb.WriteString("# Structured logging\n")
	sb.WriteString("logging:\n")
	sb.WriteString("  level: \"info\" # debug | info | warn | error\n")
	sb.WriteString(fmt.Sprintf("  format: \"%s\" # console | json\n\n", logFormat))

	sb.WriteString("# TLS & Let's Encrypt automated certificate configuration\n")
	sb.WriteString("tls:\n")
	sb.WriteString(fmt.Sprintf("  enabled: %t\n", tlsEnabled))
	sb.WriteString("  cert_file: \"\"\n")
	sb.WriteString("  key_file: \"\"\n")
	sb.WriteString("  acme_enabled: false\n")
	sb.WriteString("  acme_domain: \"\"\n")
	sb.WriteString("  acme_email: \"\"\n\n")

	sb.WriteString("# Authentication and RBAC mapping\n")
	sb.WriteString("auth:\n")
	sb.WriteString("  provider: \"sqlite\" # sqlite | pam\n")
	sb.WriteString("  require_2fa: true # enforce 2FA enrollment on login\n")
	sb.WriteString("  admin_groups:\n")
	sb.WriteString("    - \"root\"\n")
	sb.WriteString("    - \"wheel\"\n")
	sb.WriteString("    - \"sudo\"\n")
	sb.WriteString("    - \"adm\"\n")
	sb.WriteString("    - \"crucible-admin\"\n")
	sb.WriteString("  operator_groups:\n")
	sb.WriteString("    - \"operator\"\n")
	sb.WriteString("    - \"crucible-operator\"\n")
	sb.WriteString("  restrict_to_groups: true\n")

	return sb.String()
}

func (s *CommandService) renderClientTemplate(serverURL, token string) string {
	var sb strings.Builder
	sb.WriteString("# SPDX-License-Identifier: AGPL-3.0-or-later\n")
	sb.WriteString("# Copyright (c) 2026 The GotCode Collective\n")
	sb.WriteString("# See the LICENSE file in the project root for full license details.\n")
	sb.WriteString("#\n")
	sb.WriteString("# Crucible Operator CLI Configuration\n")
	sb.WriteString("version: \"1\"\n")
	sb.WriteString("current_profile: \"default\"\n\n")
	sb.WriteString("profiles:\n")
	sb.WriteString("  default:\n")
	sb.WriteString(fmt.Sprintf("    server_url: \"%s\"\n", serverURL))
	sb.WriteString(fmt.Sprintf("    token: \"%s\"\n", token))
	sb.WriteString("    insecure_skip_verify: false\n")
	sb.WriteString("    timeout_sec: 30\n")

	return sb.String()
}
