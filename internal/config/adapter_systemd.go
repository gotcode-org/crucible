// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package config

import (
	"bytes"
	"fmt"
	"text/template"
)

const systemdUnitTemplate = `[Unit]
Description=Crucible Automation Server Daemon
Documentation=https://gotcode.org/crucible
After=network.target network-online.target
Wants=network-online.target

[Service]
Type=simple
User={{ .User }}
Group={{ .Group }}
ExecStart={{ .ExecPath }} --config {{ .ConfigPath }}
Restart=always
RestartSec=5s

# Ambient port binding capabilities (allows binding ports <1024 without root)
{{- if .AmbientCapabilities }}
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
{{- end }}

# Systemd directory management: auto-creates and sets ownership
StateDirectory=crucible
RuntimeDirectory=crucibled
ConfigurationDirectory=crucible
LogsDirectory=crucible

# Sandboxing & Security hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
ProtectKernelTunables=yes
ProtectControlGroups=yes
ProtectKernelModules=yes
PrivateTmp=yes
PrivateDevices=yes
MemoryDenyWriteExecute=yes
RestrictRealtime=yes
RestrictSUIDSGID=yes
LockPersonality=yes
LimitNOFILE=65536

# Logging
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
`

// SystemdAdapter implements SystemdUnitRenderer producing hardened unit files.
type SystemdAdapter struct {
	tmpl *template.Template
}

// NewSystemdAdapter constructs a new SystemdAdapter.
func NewSystemdAdapter() *SystemdAdapter {
	t := template.Must(template.New("crucibled.service").Parse(systemdUnitTemplate))
	return &SystemdAdapter{tmpl: t}
}

// Render compiles the systemd service unit template using the provided
// options.
func (a *SystemdAdapter) Render(opts SystemdOptions) (string, error) {
	var buf bytes.Buffer
	if err := a.tmpl.Execute(&buf, opts); err != nil {
		return "", fmt.Errorf("rendering systemd unit template: %w", err)
	}
	return buf.String(), nil
}
