package main

import (
	"net/http"
	"net/url"
	"testing"
)

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

	got, err := workerUpstreamURL(incoming)
	if err != nil {
		t.Fatalf("workerUpstreamURL() error = %v", err)
	}
	want := "http://127.0.0.1:11434/v1/chat/completions?foo=bar"
	if got != want {
		t.Errorf("workerUpstreamURL() = %q, want %q", got, want)
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
	t.Setenv("UPSTREAM_API_KEY", "")
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
