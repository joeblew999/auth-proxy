package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// pinXAIOAuthUpstream clears UPSTREAM_* for tests of the default xAI OAuth path,
// so a value exported in the developer's shell cannot change which mode runs.
func pinXAIOAuthUpstream(t *testing.T) {
	t.Helper()
	t.Setenv("UPSTREAM_BASE_URL", "")
	t.Setenv("UPSTREAM_API_KEY", "")
}

// Stored OAuth tokens are a Grok subscription credential. Pointing the base URL
// at another provider without a static key must fail closed: no token is
// returned, nothing is loaded, and no refresh reaches auth.x.ai.
func TestOAuthCredentialsNeverSentToNonXAIUpstream(t *testing.T) {
	t.Setenv("UPSTREAM_API_KEY", "")
	t.Setenv("UPSTREAM_BASE_URL", "https://openrouter.ai/api/v1")

	httpCalls := 0
	store := &memoryTokenStore{tokens: &AuthTokens{
		AccessToken:  "xai-subscription-token",
		RefreshToken: "xai-refresh-token",
		ExpiresAt:    4102444800,
	}}
	configureRuntime(store, testHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		httpCalls++
		return jsonResponse(http.StatusInternalServerError, `{}`), nil
	}))

	if tokens, err := currentAccessToken(); !errors.Is(err, errOAuthUpstreamNotXAI) || tokens != nil {
		t.Errorf("currentAccessToken() = %v, %v; want nil, errOAuthUpstreamNotXAI", tokens, err)
	}
	if tokens, err := forceRefreshToken("xai-subscription-token"); !errors.Is(err, errOAuthUpstreamNotXAI) || tokens != nil {
		t.Errorf("forceRefreshToken() = %v, %v; want nil, errOAuthUpstreamNotXAI", tokens, err)
	}
	if httpCalls != 0 {
		t.Errorf("HTTP calls = %d, want 0 (no refresh may be attempted)", httpCalls)
	}

	w := httptest.NewRecorder()
	if _, ok := ensureAccessToken(w); ok {
		t.Fatal("ensureAccessToken() ok = true, want false")
	}
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if body := w.Body.String(); strings.Contains(body, "xai-subscription-token") || !strings.Contains(body, "UPSTREAM_API_KEY") {
		t.Errorf("body = %s, want a UPSTREAM_API_KEY hint and no token", body)
	}
}

// A static key is the supported way to use another provider, so it must keep
// working when the base URL is not xAI.
func TestStaticKeyWorksWithNonXAIUpstream(t *testing.T) {
	t.Setenv("UPSTREAM_API_KEY", "sk-openrouter")
	t.Setenv("UPSTREAM_BASE_URL", "https://openrouter.ai/api/v1")
	configureRuntime(nil, nil)

	tokens, err := currentAccessToken()
	if err != nil || tokens.AccessToken != "sk-openrouter" {
		t.Errorf("currentAccessToken() = %v, %v; want the static key", tokens, err)
	}
}

// Real providers put their version path under a prefix (OpenRouter /api/v1,
// Groq /openai/v1, Gemini /v1beta/openai). The incoming /v1 must be replaced by
// the base path, never appended to it.
func TestUpstreamRequestURLJoinsProviderBasePaths(t *testing.T) {
	cases := []struct {
		base, incoming, want string
	}{
		{"https://api.x.ai/v1", "/v1/chat/completions", "https://api.x.ai/v1/chat/completions"},
		{"https://openrouter.ai/api/v1", "/v1/chat/completions", "https://openrouter.ai/api/v1/chat/completions"},
		{"https://api.groq.com/openai/v1/", "/v1/models", "https://api.groq.com/openai/v1/models"},
		{"https://generativelanguage.googleapis.com/v1beta/openai", "/chat/completions", "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"},
	}
	for _, tc := range cases {
		t.Setenv("UPSTREAM_BASE_URL", tc.base)
		incoming, err := url.Parse(tc.incoming)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.incoming, err)
		}
		got, err := upstreamRequestURL(incoming)
		if err != nil || got != tc.want {
			t.Errorf("base %q, incoming %q: got %q, %v; want %q", tc.base, tc.incoming, got, err, tc.want)
		}
	}
}

func TestValidateUpstreamBaseURL(t *testing.T) {
	for base, wantErr := range map[string]bool{
		"":                             false, // default xAI
		"https://openrouter.ai/api/v1": false,
		"http://127.0.0.1:11434/v1":    false,
		"openrouter.ai/api/v1":         true,
		"ftp://example.test/v1":        true,
		"https://":                     true,
	} {
		t.Setenv("UPSTREAM_BASE_URL", base)
		if err := validateUpstreamBaseURL(); (err != nil) != wantErr {
			t.Errorf("validateUpstreamBaseURL(%q) error = %v, want error %v", base, err, wantErr)
		}
	}
}

// OAuth-only admin endpoints must refuse when the upstream is not xAI, since
// any credentials they produced could never be used.
func TestOAuthAdminEndpointsRefusedForNonXAIUpstream(t *testing.T) {
	t.Setenv("UPSTREAM_API_KEY", "")
	t.Setenv("UPSTREAM_BASE_URL", "https://openrouter.ai/api/v1")
	t.Setenv("ADMIN_API_KEY", testAdminKey)

	r := httptest.NewRequest(http.MethodPost, "/admin/tokens", strings.NewReader(`{"accessToken":"x","expiresAt":9999999999999}`))
	r.Header.Set("Authorization", "Bearer "+testAdminKey)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	adminMiddleware(tokensHandler)(w, r)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "UPSTREAM_API_KEY") {
		t.Errorf("tokens: status = %d, body = %s; want 409 naming UPSTREAM_API_KEY", w.Code, w.Body.String())
	}

	r = httptest.NewRequest(http.MethodGet, "/admin/status", nil)
	r.Header.Set("Authorization", "Bearer "+testAdminKey)
	w = httptest.NewRecorder()
	adminMiddleware(tokenStatusHandler)(w, r)
	if body := w.Body.String(); !strings.Contains(body, `"configured":false`) || !strings.Contains(body, "misconfigured") {
		t.Errorf("status body = %s, want configured false and authMode misconfigured", body)
	}
}

func TestUpstreamBaseURLDefaultsToXAI(t *testing.T) {
	t.Setenv("UPSTREAM_BASE_URL", "")

	if got := upstreamBaseURL(); got != "https://api.x.ai/v1" {
		t.Errorf("upstreamBaseURL() = %q, want the xAI default", got)
	}
	if !isXAIUpstream() {
		t.Error("isXAIUpstream() = false for the default upstream")
	}
}

func TestUpstreamBaseURLOverrideTrimsTrailingSlash(t *testing.T) {
	t.Setenv("UPSTREAM_BASE_URL", "http://127.0.0.1:11434/v1/")

	if got := upstreamBaseURL(); got != "http://127.0.0.1:11434/v1" {
		t.Errorf("upstreamBaseURL() = %q, want the trailing slash trimmed", got)
	}
	if isXAIUpstream() {
		t.Error("isXAIUpstream() = true for a non-xAI upstream")
	}
}

func TestUpstreamBaseURLTrimsWhitespace(t *testing.T) {
	t.Setenv("UPSTREAM_BASE_URL", "  https://example.test/v1  ")

	if got := upstreamBaseURL(); got != "https://example.test/v1" {
		t.Errorf("upstreamBaseURL() = %q, want it trimmed", got)
	}
}

// Routing must follow UPSTREAM_BASE_URL rather than a hardcoded xAI host.
func TestWorkerUpstreamURLUsesConfiguredBase(t *testing.T) {
	t.Setenv("UPSTREAM_BASE_URL", "http://127.0.0.1:11434/v1")

	incoming, err := url.Parse("https://proxy.example/v1/chat/completions?key=admin-secret&foo=bar")
	if err != nil {
		t.Fatalf("parse incoming URL: %v", err)
	}

	got, err := upstreamRequestURL(incoming)
	if err != nil {
		t.Fatalf("upstreamRequestURL() error = %v", err)
	}
	want := "http://127.0.0.1:11434/v1/chat/completions?foo=bar"
	if got != want {
		t.Errorf("upstreamRequestURL() = %q, want %q", got, want)
	}
}

// A static key must be used verbatim, with no stored OAuth credentials needed
// and no refresh attempted.
func TestStaticUpstreamKeyBypassesOAuth(t *testing.T) {
	t.Setenv("UPSTREAM_API_KEY", "sk-static-key")

	httpCalls := 0
	client := testHTTPClientFunc(func(*http.Request) (*http.Response, error) {
		httpCalls++
		return jsonResponse(http.StatusInternalServerError, `{}`), nil
	})
	// No token store on purpose: a static key must not need one.
	configureRuntime(nil, client)

	tokens, err := currentAccessToken()
	if err != nil {
		t.Fatalf("currentAccessToken() error = %v", err)
	}
	if tokens.AccessToken != "sk-static-key" {
		t.Errorf("AccessToken = %q, want the static key", tokens.AccessToken)
	}

	if _, err := forceRefreshToken("some-failed-token"); err != nil {
		t.Errorf("forceRefreshToken() error = %v, want no error in static-key mode", err)
	}
	if httpCalls != 0 {
		t.Errorf("HTTP calls = %d, want 0 (a static key must never trigger a refresh)", httpCalls)
	}
}

func TestStaticUpstreamKeyIsTrimmed(t *testing.T) {
	t.Setenv("UPSTREAM_API_KEY", "  sk-padded  ")

	tokens, err := currentAccessToken()
	if err != nil {
		t.Fatalf("currentAccessToken() error = %v", err)
	}
	if tokens.AccessToken != "sk-padded" {
		t.Errorf("AccessToken = %q, want it trimmed", tokens.AccessToken)
	}
}

// Without a static key the OAuth path must still fail closed when no
// credentials have been stored.
func TestNoStaticKeyStillRequiresCredentials(t *testing.T) {
	pinXAIOAuthUpstream(t)
	configureRuntime(nil, nil)

	if _, err := currentAccessToken(); err == nil {
		t.Error("currentAccessToken() error = nil, want an error when no credentials exist")
	}
}

// xAI-only model aliases must not leak into another provider's model list.
func TestProviderExtraModelsGatedOnXAI(t *testing.T) {
	t.Setenv("UPSTREAM_BASE_URL", "https://api.x.ai/v1")
	if got := len(providerExtraModels()); got != len(extraModels) {
		t.Errorf("providerExtraModels() len = %d, want %d for the xAI upstream", got, len(extraModels))
	}

	t.Setenv("UPSTREAM_BASE_URL", "http://127.0.0.1:11434/v1")
	if got := len(providerExtraModels()); got != 0 {
		t.Errorf("providerExtraModels() len = %d, want 0 for a non-xAI upstream", got)
	}
}
