package session

import (
	"strings"
	"testing"
)

func TestUnusableFindsEveryServerAFreshCloneCannotUse(t *testing.T) {
	report := `Checking MCP server health…

hk: mise exec -- hk mcp --root . - ✔ Connected
gmail: https://gmail.mcp.example (HTTP) - ⚠ Needs authentication
drive: https://drive.example (HTTP) - ✗ Failed to connect
`
	got := Unusable(report)
	if len(got) != 2 || !strings.HasPrefix(got[0], "gmail:") || !strings.HasPrefix(got[1], "drive:") {
		t.Fatalf("got %q", got)
	}
	if bad := Unusable("hk: ... - ✔ Connected\n"); len(bad) != 0 {
		t.Fatalf("clean report flagged %q", bad)
	}
}
