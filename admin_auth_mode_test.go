package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// In static-key mode there is no OAuth flow, so the OAuth-only admin endpoints
// must refuse clearly rather than attempt a device flow or silently store
// credentials that nothing ever reads.
func TestOAuthOnlyAdminEndpointsRefusedInStaticKeyMode(t *testing.T) {
	t.Setenv("UPSTREAM_API_KEY", "sk-static")
	t.Setenv("ADMIN_API_KEY", testAdminKey)

	auth := newDeviceAuth(&memoryTokenStore{}, testHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		t.Error("device flow contacted upstream in static-key mode")
		return jsonResponse(http.StatusInternalServerError, `{}`), nil
	}))

	cases := []struct {
		name        string
		handler     http.HandlerFunc
		method      string
		body        string
		contentType string
	}{
		{name: "auth start", handler: deviceAuthStartHandler(auth), method: http.MethodPost, body: "{}"},
		{name: "auth status", handler: deviceAuthStatusHandler(auth), method: http.MethodGet},
		{name: "tokens", handler: tokensHandler, method: http.MethodPost, body: `{"accessToken":"x","expiresAt":9999999999999}`, contentType: "application/json"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var r *http.Request
			if tc.body == "" {
				r = httptest.NewRequest(tc.method, "/admin", nil)
			} else {
				r = httptest.NewRequest(tc.method, "/admin", strings.NewReader(tc.body))
			}
			r.Header.Set("Authorization", "Bearer "+testAdminKey)
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}

			w := httptest.NewRecorder()
			adminMiddleware(tc.handler)(w, r)

			if w.Code != http.StatusConflict {
				t.Errorf("status = %d, want %d; body = %s", w.Code, http.StatusConflict, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "static key") {
				t.Errorf("body = %s, want it to explain the static-key mode", w.Body.String())
			}
		})
	}
}

// /admin/status must report the mode in use. Judging "configured" by the OAuth
// store would report false for a static-key deployment that works fine.
func TestAdminStatusReportsStaticKeyMode(t *testing.T) {
	t.Setenv("UPSTREAM_API_KEY", "sk-static")
	t.Setenv("ADMIN_API_KEY", testAdminKey)

	r := httptest.NewRequest(http.MethodGet, "/admin/status", nil)
	r.Header.Set("Authorization", "Bearer "+testAdminKey)
	w := httptest.NewRecorder()
	adminMiddleware(tokenStatusHandler)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"configured":true`) {
		t.Errorf("body = %s, want configured true", body)
	}
	if !strings.Contains(body, "static-key") {
		t.Errorf("body = %s, want authMode static-key", body)
	}
}

// With no static key the endpoints behave as before and the mode is reported as
// oauth.
func TestOAuthModeIsUnchangedWithoutStaticKey(t *testing.T) {
	pinXAIOAuthUpstream(t)
	t.Setenv("ADMIN_API_KEY", testAdminKey)

	r := httptest.NewRequest(http.MethodGet, "/admin/status", nil)
	r.Header.Set("Authorization", "Bearer "+testAdminKey)
	w := httptest.NewRecorder()
	adminMiddleware(tokenStatusHandler)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "oauth") {
		t.Errorf("body = %s, want authMode oauth", body)
	}
	if !strings.Contains(body, `"configured":false`) {
		t.Errorf("body = %s, want configured false with an empty store", body)
	}
}
