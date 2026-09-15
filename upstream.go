package main

import (
	"net/url"
	"strings"
)

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

// isXAIUpstream reports whether the configured upstream is xAI.
//
// Used to decide whether xAI-only model aliases should be merged into
// /v1/models responses. Advertising them against another provider would claim
// models it does not serve.
func isXAIUpstream() bool {
	parsed, err := url.Parse(upstreamBaseURL())
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Hostname(), "api.x.ai")
}
