// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 The GotCode Collective
// See the LICENSE file in the project root for full license details.

package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "valid at limit", body: `{"value":"` +
			strings.Repeat("a", maxJSONRequestBodyBytes-len(`{"value":""}`)) +
			`"}`, wantStatus: http.StatusOK},
		{name: "oversized", body: `{"value":"ok"}` +
			strings.Repeat(" ", maxJSONRequestBodyBytes),
			wantStatus: http.StatusRequestEntityTooLarge},
		{name: "malformed", body: `{"value":`,
			wantStatus: http.StatusBadRequest},
		{name: "multiple values", body: `{} {}`,
			wantStatus: http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodPost,
				"/",
				bytes.NewBufferString(test.body),
			)
			var payload struct {
				Value string `json:"value"`
			}
			ok := decodeJSONRequest(recorder, request, &payload)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			if (test.wantStatus == http.StatusOK) != ok {
				t.Fatalf("decode success = %t for status %d", ok, test.wantStatus)
			}
		})
	}
}
