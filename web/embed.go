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
	"reflect"
	"strings"
	"time"
)

// FS embeds all HTML templates and static assets for the Crucible web shell.
//
//go:embed templates/* static/*
var FS embed.FS

var funcMap = template.FuncMap{
	"upper": func(v any) string {
		if v == nil {
			return ""
		}
		return strings.ToUpper(fmt.Sprint(v))
	},
	"lower": func(v any) string {
		if v == nil {
			return ""
		}
		return strings.ToLower(fmt.Sprint(v))
	},
	"safeHTML": func(s string) template.HTML {
		// #nosec G203 -- used solely for verified SVG vector QR rendering
		return template.HTML(s)
	},
	"formatTime": func(v any) string {
		switch t := v.(type) {
		case time.Time:
			if t.IsZero() {
				return "none"
			}
			return t.UTC().Format(time.RFC3339)
		case *time.Time:
			if t == nil || t.IsZero() {
				return "never"
			}
			return t.UTC().Format(time.RFC3339)
		default:
			return "none"
		}
	},
	"lookup": func(m any, key string) any {
		if m == nil {
			return nil
		}
		val := reflect.ValueOf(m)
		if val.Kind() != reflect.Map {
			return nil
		}
		elem := val.MapIndex(reflect.ValueOf(key))
		if !elem.IsValid() {
			return nil
		}
		return elem.Interface()
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

// RenderStandalone executes a standalone template without the base.html
// layout (e.g. login.html or 2fa.html).
func RenderStandalone(w io.Writer, page string, data any) error {
	tmpl, err := template.New(page).Funcs(funcMap).ParseFS(
		FS,
		"templates/"+page,
	)
	if err != nil {
		return fmt.Errorf("parsing standalone template %s: %w", page, err)
	}

	if err := tmpl.Execute(w, data); err != nil {
		return fmt.Errorf("executing standalone template %s: %w", page, err)
	}

	return nil
}
