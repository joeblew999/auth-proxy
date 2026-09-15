package main

import (
	_ "embed"
	"os"
	"strings"

	"github.com/joeblew999/grok-oauth-proxy/internal/config"
)

//go:embed providers.toml
var builtinProviders []byte

// providersEnv holds a whole providers.toml document. It replaces the built-in
// file, which is how local Worker runs point at the mock upstream without a
// rebuild. `status` always shows which source is in use.
const providersEnv = "PROVIDERS_TOML"

// loadConfig reads providers from, in order: an explicit file path, the
// PROVIDERS_TOML variable, or the providers.toml built into the binary.
func loadConfig(path string) (*config.Config, error) {
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return config.Load(data, path, getenv)
	}
	if doc := getenv(providersEnv); strings.TrimSpace(doc) != "" {
		return config.Load([]byte(doc), providersEnv, getenv)
	}
	return config.Load(builtinProviders, "providers.toml (built in)", getenv)
}
