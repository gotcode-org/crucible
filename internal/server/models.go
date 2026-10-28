// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

// Package server implements standalone HTTP daemon listeners, UNIX domain
// socket bindings, health checks, and embedded web shell routing.
package server

import (
	"os"
	"time"

	"gotcode.org/crucible/internal/storage"
)

// Config defines network binding, socket, and runtime parameters for the HTTP
// server.
type Config struct {
	HTTPBind   string
	HTTPPort   int
	SocketPath string
	SocketMode os.FileMode
	DaemonMode string
	Version    string
}

// DatabaseHealth captures operational metrics reported by /healthz.
type DatabaseHealth struct {
	Status            string `json:"status"`
	Driver            string `json:"driver"`
	Path              string `json:"path"`
	IntegrityOK       bool   `json:"integrity_ok"`
	SchemaVersion     int    `json:"schema_version"`
	MigrationsApplied int    `json:"migrations_applied"`
	TablesCount       int    `json:"tables_count"`
	Error             string `json:"error,omitempty"`
}

// HealthEndpoints reports active listener addresses.
type HealthEndpoints struct {
	HTTP   string `json:"http"`
	Socket string `json:"socket"`
}

// HealthResponse represents the JSON payload returned by GET /healthz.
type HealthResponse struct {
	Status        string          `json:"status"`
	Version       string          `json:"version"`
	Uptime        string          `json:"uptime"`
	UptimeSeconds int64           `json:"uptime_seconds"`
	Timestamp     time.Time       `json:"timestamp"`
	Database      DatabaseHealth  `json:"database"`
	Endpoints     HealthEndpoints `json:"endpoints"`
}

// PageContext carries view model data injected into dashboard.html and
// base.html.
type PageContext struct {
	Title        string
	Version      string
	ActiveNav    string
	Breadcrumb   string
	HTTPBind     string
	HTTPPort     int
	SocketPath   string
	DaemonMode   string
	OperatorUser string
	AuthState    string
	Uptime       string
	StartedAt    time.Time
	Storage      *storage.Status
	Migrations   []storage.MigrationRecord
}
