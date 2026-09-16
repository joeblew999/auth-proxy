package config

import (
	"strings"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadResolvesProvidersKeysAndAliases(t *testing.T) {
	cfg, err := Load([]byte(`
default = "xai"

[providers.xai]
base_url = "https://api.x.ai/v1/"
auth = "xai-oauth"

[providers.groq]
base_url = "https://api.groq.com/openai/v1"
key = "GROQ_API_KEY"
headers = { "X-Title" = "proxy" }

[providers.local]
base_url = "http://127.0.0.1:11434/v1"
auth = "none"

[aliases]
fast = "groq/llama-3.1-8b-instant"
`), "test.toml", env(map[string]string{"GROQ_API_KEY": " gsk-123 ", "ADMIN_API_KEY": "admin"}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := strings.Join(cfg.Names(), ","); got != "groq,local,xai" {
		t.Errorf("Names() = %q, want sorted providers", got)
	}
	if cfg.Default != "xai" || cfg.AdminKey != "admin" {
		t.Errorf("Default = %q, AdminKey = %q", cfg.Default, cfg.AdminKey)
	}
	xai := cfg.Provider("xai")
	if xai.BaseURL != "https://api.x.ai/v1" || xai.Auth != AuthXAIOAuth || !xai.IsXAI() {
		t.Errorf("xai = %+v", xai)
	}
	groq := cfg.Provider("groq")
	if groq.Auth != AuthKey || groq.KeyName != "GROQ_API_KEY" || groq.Key != "gsk-123" || groq.Headers["X-Title"] != "proxy" {
		t.Errorf("groq = %+v", groq)
	}
	if cfg.Provider("local").Auth != AuthNone {
		t.Errorf("local auth = %q", cfg.Provider("local").Auth)
	}
	if cfg.Aliases["fast"] != "groq/llama-3.1-8b-instant" {
		t.Errorf("Aliases = %v", cfg.Aliases)
	}
	if cfg.OAuthProvider() != xai {
		t.Error("OAuthProvider() did not return the xai-oauth provider")
	}
	if got := strings.Join(cfg.KeyNames(), ","); got != "ADMIN_API_KEY,GROQ_API_KEY" {
		t.Errorf("KeyNames() = %q", got)
	}
}

func TestSingleProviderIsDefault(t *testing.T) {
	cfg, err := Load([]byte("[providers.groq]\nbase_url = \"https://api.groq.com/openai/v1\"\nkey = \"GROQ_API_KEY\"\n"), "t", env(nil))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Default != "groq" {
		t.Errorf("Default = %q, want the only provider", cfg.Default)
	}
}

func TestProblemNamesTheFix(t *testing.T) {
	cfg, err := Load([]byte("[providers.groq]\nbase_url = \"https://api.groq.com/openai/v1\"\nkey = \"GROQ_API_KEY\"\n"), "t", env(nil))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	problem, fix := cfg.Provider("groq").Problem()
	if !strings.Contains(problem, "GROQ_API_KEY") || fix != "mise run secrets:set groq" {
		t.Errorf("Problem() = %q, %q", problem, fix)
	}
}

// Every rejection must say what is wrong in terms the user can act on.
func TestLoadRejectsInvalidConfig(t *testing.T) {
	cases := map[string]struct {
		toml string
		want string
	}{
		"empty": {``, "no providers"},
		"typo in setting": {`[providers.groq]
base-url = "https://api.groq.com/openai/v1"
key = "GROQ_API_KEY"`, "unknown setting providers.groq.base-url"},
		"missing auth": {`[providers.groq]
base_url = "https://api.groq.com/openai/v1"`, "set key ="},
		"relative url": {`[providers.groq]
base_url = "api.groq.com/openai/v1"
key = "GROQ_API_KEY"`, "absolute http or https URL"},
		"key value instead of name": {`[providers.groq]
base_url = "https://api.groq.com/openai/v1"
key = "gsk_live_123abc"`, "must be a secret name"},
		"oauth to other host": {`[providers.groq]
base_url = "https://api.groq.com/openai/v1"
auth = "xai-oauth"`, "only works with base_url on api.x.ai"},
		"key with none": {`[providers.local]
base_url = "http://127.0.0.1:11434/v1"
auth = "none"
key = "LOCAL_KEY"`, "key is only used with auth"},
		"unknown auth": {`[providers.x]
base_url = "https://api.x.ai/v1"
auth = "oauth"`, "is not one of"},
		"no default with two": {`[providers.a]
base_url = "https://a.test/v1"
auth = "none"
[providers.b]
base_url = "https://b.test/v1"
auth = "none"`, "set default"},
		"default unknown": {`default = "c"
[providers.a]
base_url = "https://a.test/v1"
auth = "none"`, "default = \"c\" is not a provider"},
		"alias unknown provider": {`[providers.a]
base_url = "https://a.test/v1"
auth = "none"
[aliases]
fast = "groq/llama"`, "alias \"fast\""},
		"bad provider name": {`[providers.Groq]
base_url = "https://a.test/v1"
auth = "none"`, "lowercase"},
		"admin key reused": {`[providers.a]
base_url = "https://a.test/v1"
key = "ADMIN_API_KEY"`, "must not reuse ADMIN_API_KEY"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load([]byte(tc.toml), "providers.toml", env(nil))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Load() error = %v, want it to contain %q", err, tc.want)
			}
			if err != nil && !strings.HasPrefix(err.Error(), "providers.toml") {
				t.Errorf("error %q does not name the config source", err)
			}
		})
	}
}

func TestOnlyOneOAuthProvider(t *testing.T) {
	_, err := Load([]byte(`default = "a"
[providers.a]
base_url = "https://api.x.ai/v1"
auth = "xai-oauth"
[providers.b]
base_url = "https://api.x.ai/v1"
auth = "xai-oauth"`), "t", env(nil))
	if err == nil || !strings.Contains(err.Error(), "only one provider") {
		t.Errorf("Load() error = %v", err)
	}
}
