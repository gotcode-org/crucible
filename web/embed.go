// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

// Package web embeds HTML templates and static assets for the Crucible Web UI.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"
)

// FS embeds all HTML templates and static assets for the Crucible web shell.
//
//go:embed templates/* static/*
var FS embed.FS

var funcMap = template.FuncMap{
	"upper": strings.ToUpper,
	"lower": strings.ToLower,
	"formatTime": func(t time.Time) string {
		if t.IsZero() {
			return "none"
		}
		return t.UTC().Format(time.RFC3339)
	},
}

// RenderTemplate parses and executes the requested page template merged with
// base.html layout.
func RenderTemplate(w io.Writer, page string, data any) error {
	tmpl, err := template.New("base.html").Funcs(funcMap).ParseFS(
		FS,
		"templates/base.html",
		"templates/"+page,
	)
	if err != nil {
		return fmt.Errorf("parsing template %s: %w", page, err)
	}

	if err := tmpl.ExecuteTemplate(w, "base.html", data); err != nil {
		return fmt.Errorf("executing template %s: %w", page, err)
	}

	return nil
}
