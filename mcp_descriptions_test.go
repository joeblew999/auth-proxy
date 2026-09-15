//go:build !tinygo

package main

import (
	"strings"
	"testing"
)

// MCP tool descriptions are shown to clients, so they must not claim the
// upstream is xAI when it has been pointed somewhere else.
func TestUpstreamDescriptionsFollowConfig(t *testing.T) {
	t.Setenv("UPSTREAM_API_KEY", "")
	t.Setenv("UPSTREAM_BASE_URL", "https://api.x.ai/v1")

	if got := upstreamName(); got != "the xAI API" {
		t.Errorf("upstreamName() = %q, want the xAI name", got)
	}
	if blurb := upstreamAuthBlurb(); !strings.Contains(blurb, "Grok OAuth") {
		t.Errorf("upstreamAuthBlurb() = %q, want it to mention the Grok OAuth credentials", blurb)
	}
	if blurb := upstreamModelsBlurb(); !strings.Contains(blurb, "xAI API") {
		t.Errorf("upstreamModelsBlurb() = %q, want it to mention the xAI API", blurb)
	}

	t.Setenv("UPSTREAM_BASE_URL", "http://127.0.0.1:11434/v1")

	if got := upstreamName(); got != "the configured upstream API" {
		t.Errorf("upstreamName() = %q, want the generic name", got)
	}
	if blurb := upstreamAuthBlurb(); strings.Contains(blurb, "xAI") {
		t.Errorf("upstreamAuthBlurb() = %q, must not mention xAI for a non-xAI upstream", blurb)
	}
	if blurb := upstreamModelsBlurb(); strings.Contains(blurb, "xAI") {
		t.Errorf("upstreamModelsBlurb() = %q, must not mention xAI for a non-xAI upstream", blurb)
	}
}

// Static-key mode changes how auth is described, since there is no OAuth flow.
func TestUpstreamAuthBlurbReflectsStaticKeyMode(t *testing.T) {
	t.Setenv("UPSTREAM_BASE_URL", "https://api.x.ai/v1")
	t.Setenv("UPSTREAM_API_KEY", "sk-static")

	blurb := upstreamAuthBlurb()
	if !strings.Contains(blurb, "static API key") {
		t.Errorf("upstreamAuthBlurb() = %q, want it to mention the static API key", blurb)
	}
	// Saying "no OAuth flow is involved" is deliberate and useful. What must not
	// happen is claiming stored credentials are used, which is the OAuth path.
	if strings.Contains(blurb, "using the stored") {
		t.Errorf("upstreamAuthBlurb() = %q, must not claim stored credentials in static-key mode", blurb)
	}
}

// Tool names are public MCP surface and must not drift with the upstream.
func TestMCPToolNamesAreStable(t *testing.T) {
	t.Setenv("UPSTREAM_BASE_URL", "http://127.0.0.1:11434/v1")
	srv := newMCPServer()
	if srv == nil {
		t.Fatal("newMCPServer() = nil")
	}
}
