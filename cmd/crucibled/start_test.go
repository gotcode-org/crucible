// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func TestCrucibledStart_Help(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"start", "--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cmd.Execute() failed: %v", err)
	}

	out := buf.String()
	expectedSubstrings := []string{
		"Start the crucibled server daemon",
		"--http-port",
		"--http-bind",
		"--socket-path",
		"--database-path",
		"--config",
		"--require-2fa",
		"--no-require-2fa",
	}

	for _, substr := range expectedSubstrings {
		if !strings.Contains(out, substr) {
			t.Errorf("start help output missing %q:\n%s", substr, out)
		}
	}
}

func TestCrucibledStart_Lifecycle(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "start_test.db")
	sockFile := filepath.Join(tmpDir, "start_test.sock")
	keyFile := filepath.Join(tmpDir, "secrets.key")
	cfgFile := filepath.Join(tmpDir, "start_test_cfg.yaml")

	cfgContent := fmt.Sprintf(
		"version: \"1.0\"\n"+
			"mode: standalone\n"+
			"server:\n"+
			"  http_port: 0\n"+
			"  socket_path: %s\n"+
			"storage:\n"+
			"  database_path: %s\n"+
			"secrets:\n"+
			"  key_path: %s\n",
		sockFile,
		dbFile,
		keyFile,
	)
	if err := os.WriteFile(cfgFile, []byte(cfgContent), 0600); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := newRootCmd()
	var buf syncBuffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{
		"start",
		"--config", cfgFile,
	})

	errChan := make(chan error, 1)
	go func() {
		errChan <- cmd.ExecuteContext(ctx)
	}()

	// Wait briefly for server startup banner
	deadline := time.Now().Add(3 * time.Second)
	started := false
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "CRUCIBLE SERVER DAEMON") {
			started = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if !started {
		t.Fatalf(
			"server did not print startup banner in time:\n%s",
			buf.String(),
		)
	}

	// Cancel context to initiate graceful shutdown
	cancel()

	select {
	case err := <-errChan:
		if err != nil {
			t.Fatalf("cmd.ExecuteContext failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server to shut down cleanly")
	}

	out := buf.String()
	expectedTokens := []string{
		"CRUCIBLE SERVER DAEMON",
		"ONLINE",
		"TCP Listener",
		"UNIX Socket",
		"stopped cleanly",
	}

	for _, token := range expectedTokens {
		if !strings.Contains(out, token) {
			t.Errorf("expected server output to contain %q:\n%s", token, out)
		}
	}
}
