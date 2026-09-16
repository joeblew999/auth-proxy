package bootstrap

import "testing"

// The built-in providers.toml ships in every binary and Worker, so a mistake in
// it must fail the tests rather than a deploy.
func TestBuiltinAndMockProvidersLoad(t *testing.T) {
	t.Setenv(ProvidersEnv, "")
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("built-in providers.toml: %v", err)
	}
	if cfg.Provider(cfg.Default) == nil {
		t.Errorf("default %q is not a provider", cfg.Default)
	}

	mock, err := LoadConfig("../../tools/mock-upstream/providers.toml")
	if err != nil {
		t.Fatalf("mock providers.toml: %v", err)
	}
	if len(mock.Providers) != 2 {
		t.Errorf("mock providers = %v, want two for routing checks", mock.Names())
	}
}

func TestProvidersEnvReplacesBuiltin(t *testing.T) {
	t.Setenv(ProvidersEnv, "[providers.only]\nbase_url = \"http://127.0.0.1:9/v1\"\nauth = \"none\"\n")
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source != ProvidersEnv || cfg.Default != "only" {
		t.Errorf("Source = %q, Default = %q", cfg.Source, cfg.Default)
	}
}
