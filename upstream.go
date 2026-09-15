package main

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// errOAuthUpstreamNotXAI is returned instead of OAuth credentials when the
// upstream is not xAI. The stored tokens are a Grok subscription credential, so
// sending them anywhere else would hand that account to a third party.
var errOAuthUpstreamNotXAI = errors.New("refusing to send Grok OAuth credentials to a non-xAI upstream; set UPSTREAM_API_KEY for that provider")

// defaultUpstreamBaseURL is xAI. Keeping it as the default means a deployment
// with no UPSTREAM_* configuration behaves exactly as it did before upstreams
// were configurable.
const defaultUpstreamBaseURL = "https://api.x.ai/v1"

// upstreamBaseURL returns the base URL of the upstream OpenAI-compatible API.
//
// Override with UPSTREAM_BASE_URL to point the proxy at another provider, for
// example a local Ollama at http://127.0.0.1:11434/v1 or any hosted
// OpenAI-compatible endpoint. A trailing slash is tolerated.
func upstreamBaseURL() string {
	configured := strings.TrimSpace(getenv("UPSTREAM_BASE_URL"))
	if configured == "" {
		return defaultUpstreamBaseURL
	}
	return strings.TrimRight(configured, "/")
}

// staticUpstreamKey returns the bearer token to send upstream when a static key
// is configured, or "" when xAI's OAuth device flow should be used instead.
//
// Set UPSTREAM_API_KEY for providers that authenticate with a fixed API key.
// Leaving it unset preserves the OAuth behaviour, which is the default.
func staticUpstreamKey() string {
	return strings.TrimSpace(getenv("UPSTREAM_API_KEY"))
}

// validateUpstreamBaseURL reports a configuration error when the upstream base
// URL is not an absolute http or https URL, so a typo fails at startup rather
// than on every request.
func validateUpstreamBaseURL() error {
	base := upstreamBaseURL()
	parsed, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("invalid UPSTREAM_BASE_URL %q: %w", base, err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("invalid UPSTREAM_BASE_URL %q: want an absolute http or https URL", base)
	}
	return nil
}

// isXAIUpstream reports whether the configured upstream is xAI.
//
// It gates everything xAI-specific: OAuth credentials are only ever sent to
// xAI, and xAI-only model aliases are only merged into /v1/models for xAI.
func isXAIUpstream() bool {
	parsed, err := url.Parse(upstreamBaseURL())
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), "api.x.ai")
}

// oauthUnavailableReason explains why the xAI OAuth flow cannot be used with the
// current configuration, or returns "" when it can. OAuth needs both no static
// key and an xAI upstream.
func oauthUnavailableReason() string {
	switch {
	case staticUpstreamKey() != "":
		return "OAuth is unavailable: UPSTREAM_API_KEY is set, so the upstream credential is a static key"
	case !isXAIUpstream():
		return "OAuth is unavailable: UPSTREAM_BASE_URL is not xAI, and Grok OAuth credentials are only sent to api.x.ai; set UPSTREAM_API_KEY for that provider"
	}
	return ""
}
