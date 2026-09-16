package release

import (
	"strings"
	"testing"
)

func TestGoreleaserConfigNamesTheCommandAndBinary(t *testing.T) {
	cfg := goreleaserConfig("grok-oauth-proxy", "cmd/proxy")
	for _, want := range []string{"project_name: grok-oauth-proxy", "dir: cmd/proxy", "binary: grok-oauth-proxy", "goos: [linux, darwin, windows]", "CGO_ENABLED=0"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config lacks %q:\n%s", want, cfg)
		}
	}
}
