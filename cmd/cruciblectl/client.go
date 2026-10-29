// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"gotcode.org/crucible/internal/config"
)

// ClientTransport wraps an HTTP client configured for either UNIX domain
// socket or TCP HTTP/HTTPS transport.
type ClientTransport struct {
	Client     *http.Client
	BaseURL    string
	Token      string
	IsUnixSock bool
}

// ClientOptions specifies connection and authentication overrides.
type ClientOptions struct {
	SocketPath string
	ServerURL  string
	Token      string
	ConfigFile string
}

var standardSocketPaths = []string{
	"/run/crucibled/crucible.sock",
	"./var/crucible.sock",
	"crucible.sock",
}

// ResolveClientTransport detects the target transport (UNIX domain socket or
// TCP) based on explicit flags, auto-discovered sockets, or config profile.
func ResolveClientTransport(
	ctx context.Context,
	opts ClientOptions,
) (*ClientTransport, error) {
	token := strings.TrimSpace(opts.Token)

	// 1. Explicit TCP server URL override
	if opts.ServerURL != "" && opts.ServerURL != "http://127.0.0.1:8080" {
		return newTCPTransport(opts.ServerURL, token), nil
	}

	// 2. Explicit or auto-discovered UNIX socket
	sock, err := discoverSocketPath(opts.SocketPath)
	if err != nil {
		return nil, err
	}
	if sock != "" {
		return newUnixTransport(sock, token), nil
	}

	// 3. Fallback to client configuration profile or default TCP
	profServer, profToken := resolveProfileCredentials(
		ctx,
		opts.ConfigFile,
	)
	targetServer := opts.ServerURL
	if targetServer == "" || targetServer == "http://127.0.0.1:8080" {
		if profServer != "" {
			targetServer = profServer
		} else {
			targetServer = "http://127.0.0.1:8080"
		}
	}
	if token == "" && profToken != "" {
		token = profToken
	}

	return newTCPTransport(targetServer, token), nil
}

func discoverSocketPath(socketPath string) (string, error) {
	if socketPath != "" {
		if !isSocketFile(socketPath) {
			return "", fmt.Errorf(
				"socket file %s does not exist",
				socketPath,
			)
		}
		return socketPath, nil
	}
	for _, p := range standardSocketPaths {
		if isSocketFile(p) {
			return p, nil
		}
	}
	return "", nil
}

func resolveProfileCredentials(
	ctx context.Context,
	cfgPath string,
) (string, string) {
	if cfgPath == "" {
		cfgPath = defaultClientConfigPath()
	}
	if _, err := os.Stat(cfgPath); err != nil {
		return "", ""
	}
	qSvc := config.DefaultQueryService()
	cfg, err := qSvc.GetClientConfig(
		ctx,
		config.GetClientConfigQuery{ConfigFile: cfgPath},
	)
	if err != nil || cfg == nil {
		return "", ""
	}
	prof, ok := cfg.Profiles[cfg.CurrentProfile]
	if !ok {
		return "", ""
	}
	return prof.ServerURL, prof.Token
}

func isSocketFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSocket != 0
}

func newUnixTransport(socketPath string, token string) *ClientTransport {
	return &ClientTransport{
		Client: &http.Client{
			Transport: &http.Transport{
				DialContext: func(
					c context.Context,
					_, _ string,
				) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(
						c,
						"unix",
						socketPath,
					)
				},
			},
			Timeout: 15 * time.Second,
		},
		BaseURL:    "http://unix",
		Token:      token,
		IsUnixSock: true,
	}
}

func newTCPTransport(serverURL string, token string) *ClientTransport {
	return &ClientTransport{
		Client: &http.Client{
			Timeout: 15 * time.Second,
		},
		BaseURL:    strings.TrimRight(serverURL, "/"),
		Token:      token,
		IsUnixSock: false,
	}
}

// Request executes an HTTP request with authentication headers attached.
func (c *ClientTransport) Request(
	ctx context.Context,
	method string,
	endpoint string,
	body io.Reader,
) (*http.Response, error) {
	fullURL := c.BaseURL + "/" + strings.TrimLeft(endpoint, "/")
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	return c.Client.Do(req)
}
