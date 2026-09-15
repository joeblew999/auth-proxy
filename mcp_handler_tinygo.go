//go:build tinygo

package main

import "net/http"

// mcpHandler reports /mcp as unavailable in TinyGo builds. The MCP SDK needs eight
// APIs that TinyGo 0.42 lacks or gets wrong, starting with hash/maphash on Go
// 1.27. See .plan/tinygo.md and https://github.com/tinygo-org/tinygo/issues/5684.
func mcpHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "MCP is not available in the TinyGo build", http.StatusNotImplemented)
	}
}
