// Package bootstrap decides which providers file the proxy uses: an explicit
// --config path, the PROVIDERS_TOML variable, or the providers.toml built
// into the binary. Both runtimes (the local CLI and the Worker) load through
// here so they never drift apart.
package bootstrap

import (
	_ "embed"
	"os"
	"strings"

	"github.com/joeblew999/grok-oauth-proxy/cmd/proxy/internal/config"
)

//go:embed providers.toml
var builtinProviders []byte

// ProvidersEnv holds a whole providers.toml document. It replaces the built-in
// file, which is how local Worker runs point at the mock upstream without a
// rebuild. `status` always shows which source is in use.
const ProvidersEnv = "PROVIDERS_TOML"

// LoadConfig reads providers from, in order: an explicit file path, the
// PROVIDERS_TOML variable, or the providers.toml built into the binary.
func LoadConfig(path string) (*config.Config, error) {
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return config.Load(data, path, getenv)
	}
	if doc := getenv(ProvidersEnv); strings.TrimSpace(doc) != "" {
		return config.Load([]byte(doc), ProvidersEnv, getenv)
	}
	return config.Load(builtinProviders, "providers.toml (built in)", getenv)
}
