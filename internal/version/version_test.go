// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package version

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type mockProvider struct {
	metadataFn func(ctx context.Context) (BuildMetadata, error)
	depsFn     func(ctx context.Context) ([]DependencyInfo, error)
}

func (m *mockProvider) GetBuildMetadata(ctx context.Context) (BuildMetadata, error) {
	if m.metadataFn != nil {
		return m.metadataFn(ctx)
	}
	return BuildMetadata{
		Version:      "v1.0.0",
		GitBranch:    "main",
		GitCommit:    "abc1234",
		GitTreeState: "clean",
		BuildDate:    "2026-10-27T16:00:00Z",
		GoVersion:    "go1.24.4",
		Compiler:     "gc",
		Platform:     "linux/amd64",
	}, nil
}

func (m *mockProvider) GetDependencies(ctx context.Context) ([]DependencyInfo, error) {
	if m.depsFn != nil {
		return m.depsFn(ctx)
	}
	return []DependencyInfo{
		{Path: "github.com/spf13/cobra", Version: "v1.8.0"},
	}, nil
}

func mustNewQueryService(t *testing.T, provider MetadataProvider) *QueryService {
	t.Helper()
	svc, err := NewQueryService(provider)
	if err != nil {
		t.Fatalf("unexpected error constructing QueryService: %v", err)
	}
	return svc
}

func TestBuildMetadataValidate(t *testing.T) {
	tests := []struct {
		name    string
		meta    BuildMetadata
		wantErr bool
	}{
		{
			name: "valid metadata",
			meta: BuildMetadata{
				Version:  "v1.0.0",
				Platform: "linux/amd64",
			},
			wantErr: false,
		},
		{
			name: "empty version",
			meta: BuildMetadata{
				Version:  "  ",
				Platform: "linux/amd64",
			},
			wantErr: true,
		},
		{
			name: "empty platform",
			meta: BuildMetadata{
				Version:  "v1.0.0",
				Platform: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.meta.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestInfoFormatters(t *testing.T) {
	info := Info{
		AppName: "crucibled",
		Build: BuildMetadata{
			Version:      "v0.1.0",
			GitBranch:    "main",
			GitCommit:    "deadbeef",
			GitTreeState: "clean",
			BuildDate:    "2026-10-27T00:00:00Z",
			GoVersion:    "go1.24.4",
			Compiler:     "gc",
			Platform:     "linux/amd64",
		},
	}

	t.Run("String output", func(t *testing.T) {
		str := info.String()
		if !strings.Contains(str, "crucibled v0.1.0") {
			t.Errorf("expected app name and version, got:\n%s", str)
		}
		if !strings.Contains(str, "Git Commit:     deadbeef") {
			t.Errorf("expected commit in string output, got:\n%s", str)
		}
	})

	t.Run("Short output", func(t *testing.T) {
		short := info.Short()
		expected := "crucibled v0.1.0 (deadbeef, linux/amd64)"
		if short != expected {
			t.Errorf("Short() = %q, want %q", short, expected)
		}
	})

	t.Run("JSON output", func(t *testing.T) {
		jsonStr, err := info.JSON()
		if err != nil {
			t.Fatalf("unexpected JSON error: %v", err)
		}
		if !strings.Contains(jsonStr, `"app_name": "crucibled"`) {
			t.Errorf("expected app_name in json output, got:\n%s", jsonStr)
		}
	})
}

func TestQueryServiceConstructor(t *testing.T) {
	t.Run("nil provider returns error", func(t *testing.T) {
		svc, err := NewQueryService(nil)
		if !errors.Is(err, ErrNilProvider) {
			t.Errorf("expected ErrNilProvider, got %v", err)
		}
		if svc != nil {
			t.Errorf("expected nil service, got %v", svc)
		}
	})

	t.Run("valid provider succeeds", func(t *testing.T) {
		mock := &mockProvider{}
		svc, err := NewQueryService(mock)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if svc == nil {
			t.Fatal("expected non-nil service")
		}
	})
}

func TestQueryServiceExecution(t *testing.T) {
	ctx := context.Background()

	t.Run("query without dependencies", func(t *testing.T) {
		mock := &mockProvider{}
		svc := mustNewQueryService(t, mock)

		info, err := svc.Handle(ctx, GetVersionQuery{AppName: "cruciblectl"})
		if err != nil {
			t.Fatalf("unexpected error executing query: %v", err)
		}
		if info.AppName != "cruciblectl" {
			t.Errorf("expected AppName cruciblectl, got %q", info.AppName)
		}
		if info.Build.Version != "v1.0.0" {
			t.Errorf("expected version v1.0.0, got %q", info.Build.Version)
		}
		if len(info.Dependencies) != 0 {
			t.Errorf("expected 0 dependencies when flag is false, got %d", len(info.Dependencies))
		}
	})

	t.Run("default app name fallback", func(t *testing.T) {
		mock := &mockProvider{}
		svc := mustNewQueryService(t, mock)

		info, err := svc.Handle(ctx, GetVersionQuery{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info.AppName != "Crucible" {
			t.Errorf("expected default app name Crucible, got %q", info.AppName)
		}
	})

	t.Run("include dependencies", func(t *testing.T) {
		mock := &mockProvider{}
		svc := mustNewQueryService(t, mock)

		info, err := svc.Handle(ctx, GetVersionQuery{IncludeDependencies: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(info.Dependencies) != 1 {
			t.Fatalf("expected 1 dependency, got %d", len(info.Dependencies))
		}
		if info.Dependencies[0].Path != "github.com/spf13/cobra" {
			t.Errorf("unexpected dependency path: %s", info.Dependencies[0].Path)
		}
	})
}

func TestQueryServiceErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("provider metadata failure", func(t *testing.T) {
		mockErr := errors.New("read failure")
		mock := &mockProvider{
			metadataFn: func(_ context.Context) (BuildMetadata, error) {
				return BuildMetadata{}, mockErr
			},
		}
		svc := mustNewQueryService(t, mock)

		_, err := svc.Handle(ctx, GetVersionQuery{})
		if !errors.Is(err, mockErr) {
			t.Errorf("expected mockErr, got %v", err)
		}
	})

	t.Run("validation failure", func(t *testing.T) {
		mock := &mockProvider{
			metadataFn: func(_ context.Context) (BuildMetadata, error) {
				return BuildMetadata{Version: "", Platform: "linux/amd64"}, nil
			},
		}
		svc := mustNewQueryService(t, mock)

		_, err := svc.Handle(ctx, GetVersionQuery{})
		if !errors.Is(err, ErrInvalidVersion) {
			t.Errorf("expected ErrInvalidVersion, got %v", err)
		}
	})

	t.Run("provider dependencies failure", func(t *testing.T) {
		mockErr := errors.New("dep read failure")
		mock := &mockProvider{
			depsFn: func(_ context.Context) ([]DependencyInfo, error) {
				return nil, mockErr
			},
		}
		svc := mustNewQueryService(t, mock)

		_, err := svc.Handle(ctx, GetVersionQuery{IncludeDependencies: true})
		if !errors.Is(err, mockErr) {
			t.Errorf("expected mockErr, got %v", err)
		}
	})
}

func TestRuntimeMetadataProvider(t *testing.T) {
	ctx := context.Background()
	provider := NewRuntimeMetadataProvider()

	meta, err := provider.GetBuildMetadata(ctx)
	if err != nil {
		t.Fatalf("unexpected error from GetBuildMetadata: %v", err)
	}
	valErr := meta.Validate()
	if valErr != nil {
		t.Errorf("runtime metadata failed validation: %v", valErr)
	}

	deps, err := provider.GetDependencies(ctx)
	if err != nil {
		t.Errorf("unexpected error from GetDependencies: %v", err)
	}
	_ = deps
}
