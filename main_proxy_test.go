//go:build !js || !wasm

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// upstreamRecorder is a fake provider that records what the local reverse proxy
// actually sent it.
type upstreamRecorder struct {
	mu       sync.Mutex
	requests []*http.Request
}

func (u *upstreamRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	u.requests = append(u.requests, r.Clone(r.Context()))
	u.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (u *upstreamRecorder) seen() []*http.Request {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]*http.Request(nil), u.requests...)
}

// The local reverse proxy used to join the base path onto an already-prefixed
// request path, so a base of /openai/v1 produced /openai/v1/v1/chat/completions.
func TestLocalProxyJoinsNonV1BasePath(t *testing.T) {
	recorder := &upstreamRecorder{}
	upstream := httptest.NewServer(recorder)
	defer upstream.Close()

	t.Setenv("UPSTREAM_BASE_URL", upstream.URL+"/openai/v1")
	t.Setenv("UPSTREAM_API_KEY", "sk-provider")

	handler := handleProxy(newUpstreamReverseProxy())
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?key=admin-secret&foo=bar", strings.NewReader(`{}`))
	r.Header.Set("Authorization", "Bearer admin-secret")
	w := httptest.NewRecorder()
	handler(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	seen := recorder.seen()
	if len(seen) != 1 {
		t.Fatalf("upstream saw %d requests, want 1", len(seen))
	}
	got := seen[0]
	if got.URL.Path != "/openai/v1/chat/completions" {
		t.Errorf("upstream path = %q, want /openai/v1/chat/completions", got.URL.Path)
	}
	if got.URL.RawQuery != "foo=bar" {
		t.Errorf("upstream query = %q, want foo=bar (key stripped)", got.URL.RawQuery)
	}
	if auth := got.Header.Get("Authorization"); auth != "Bearer sk-provider" {
		t.Errorf("upstream Authorization = %q, want the static key", auth)
	}
}

// End to end through the local proxy: stored xAI OAuth tokens must not reach a
// non-xAI upstream when no static key is configured.
func TestLocalProxyWithholdsOAuthTokenFromNonXAIUpstream(t *testing.T) {
	recorder := &upstreamRecorder{}
	upstream := httptest.NewServer(recorder)
	defer upstream.Close()

	t.Setenv("UPSTREAM_BASE_URL", upstream.URL+"/v1")
	t.Setenv("UPSTREAM_API_KEY", "")
	configureRuntime(&memoryTokenStore{tokens: &AuthTokens{
		AccessToken: "xai-subscription-token",
		ExpiresAt:   4102444800,
	}}, http.DefaultClient)

	handler := handleProxy(newUpstreamReverseProxy())
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	handler(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d; body = %s", w.Code, http.StatusInternalServerError, w.Body.String())
	}
	if n := len(recorder.seen()); n != 0 {
		t.Errorf("upstream saw %d requests, want 0", n)
	}
}

// The browser OAuth routes must refuse when OAuth cannot be used.
func TestOAuthRoutesRefusedWhenOAuthUnavailable(t *testing.T) {
	cases := map[string][2]string{
		"static key":       {"https://api.x.ai/v1", "sk-static"},
		"non-xAI upstream": {"https://openrouter.ai/api/v1", ""},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv("UPSTREAM_BASE_URL", env[0])
			t.Setenv("UPSTREAM_API_KEY", env[1])
			called := false
			handler := requireOAuthRoute(func(http.ResponseWriter, *http.Request) { called = true })

			w := httptest.NewRecorder()
			handler(w, httptest.NewRequest(http.MethodGet, "/login", nil))

			if called || w.Code != http.StatusConflict {
				t.Errorf("called = %v, status = %d; want the route refused with 409", called, w.Code)
			}
		})
	}

	pinXAIOAuthUpstream(t)
	called := false
	requireOAuthRoute(func(http.ResponseWriter, *http.Request) { called = true })(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/login", nil))
	if !called {
		t.Error("default xAI OAuth configuration refused the login route")
	}
}
