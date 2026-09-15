//go:build tinygo

// Package mcp is empty in TinyGo builds: the MCP SDK needs APIs TinyGo 0.42 lacks
// or gets wrong (https://github.com/tinygo-org/tinygo/issues/5684), so the proxy
// answers /mcp with 501. See .plan/tinygo.md.
package mcp

import (
	"net/http"

	"github.com/joeblew999/grok-oauth-proxy/internal/proxy"
)

// NewHandler returns nil, which the proxy serves as 501.
func NewHandler(*proxy.Upstream) http.Handler { return nil }
