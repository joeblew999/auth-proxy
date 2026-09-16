// Package config loads providers.toml, the single description of which upstream
// providers the proxy serves, and resolves the secrets it names.
//
// It is the only package that reads the environment. Everything else receives a
// *Config, so behaviour never depends on variables read somewhere at call time.
package config

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// AdminKeyName is the secret that clients and admin tools present to the proxy.
const AdminKeyName = "ADMIN_API_KEY"

// Auth is how the proxy authenticates to a provider.
type Auth string

const (
	// AuthKey sends the provider's API key, read from the named secret.
	AuthKey Auth = "key"
	// AuthXAIOAuth sends the stored Grok subscription token. Only valid for api.x.ai.
	AuthXAIOAuth Auth = "xai-oauth"
	// AuthNone sends no credential, for local servers such as Ollama.
	AuthNone Auth = "none"
)

const xaiHost = "api.x.ai"

// Provider is one upstream OpenAI-compatible API.
type Provider struct {
	Name    string
	BaseURL string // no trailing slash; includes the provider's version path
	Auth    Auth
	KeyName string            // secret name, for AuthKey
	Key     string            // resolved secret value; empty when not set
	Headers map[string]string // extra headers sent on every request
}

// IsXAI reports whether the provider is xAI's API.
func (p *Provider) IsXAI() bool {
	parsed, err := url.Parse(p.BaseURL)
	return err == nil && strings.EqualFold(parsed.Hostname(), xaiHost)
}

// Problem describes why the provider cannot serve requests with its current
// secrets, and the command that fixes it. It is empty when the provider is ready
// as far as configuration can tell; xai-oauth readiness also depends on the
// token store and is reported by the proxy.
func (p *Provider) Problem() (problem, fix string) {
	if p.Auth == AuthKey && p.Key == "" {
		return fmt.Sprintf("key %s is not set", p.KeyName), "mise run keys:set " + p.Name
	}
	return "", ""
}

// Config is the validated provider configuration.
type Config struct {
	Source    string // where the configuration came from, for logs and status
	Default   string // provider used for models without a provider prefix
	Providers []*Provider
	Aliases   map[string]string // alias -> "provider/model"
	AdminKey  string            // resolved ADMIN_API_KEY; empty when not set
}

// Provider returns the named provider, or nil.
func (c *Config) Provider(name string) *Provider {
	for _, p := range c.Providers {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// Names returns the provider names in sorted order.
func (c *Config) Names() []string {
	names := make([]string, len(c.Providers))
	for i, p := range c.Providers {
		names[i] = p.Name
	}
	return names
}

// OAuthProvider returns the xai-oauth provider, or nil when none is configured.
func (c *Config) OAuthProvider() *Provider {
	for _, p := range c.Providers {
		if p.Auth == AuthXAIOAuth {
			return p
		}
	}
	return nil
}

// KeyNames returns every secret the configuration needs, admin key first.
func (c *Config) KeyNames() []string {
	names := []string{AdminKeyName}
	for _, p := range c.Providers {
		if p.Auth == AuthKey {
			names = append(names, p.KeyName)
		}
	}
	return names
}

type fileProvider struct {
	BaseURL string            `toml:"base_url"`
	Auth    string            `toml:"auth"`
	Key     string            `toml:"key"`
	Headers map[string]string `toml:"headers"`
}

type file struct {
	Default   string                  `toml:"default"`
	Providers map[string]fileProvider `toml:"providers"`
	Aliases   map[string]string       `toml:"aliases"`
}

var (
	providerNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	keyNamePattern      = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

// Load parses and validates a providers.toml document, then resolves the secrets
// it names through getenv. Errors name the offending provider or key and, where
// possible, how to fix it.
func Load(data []byte, source string, getenv func(string) string) (*Config, error) {
	var f file
	meta, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, len(undecoded))
		for i, key := range undecoded {
			keys[i] = key.String()
		}
		return nil, fmt.Errorf("%s: unknown setting %s (allowed: default, [aliases], and base_url, auth, key, headers per provider)",
			source, strings.Join(keys, ", "))
	}
	if len(f.Providers) == 0 {
		return nil, fmt.Errorf("%s: no providers; add a [providers.<name>] section with base_url", source)
	}

	cfg := &Config{Source: source, Aliases: map[string]string{}, AdminKey: strings.TrimSpace(getenv(AdminKeyName))}
	for name, fp := range f.Providers {
		p, err := buildProvider(name, fp, getenv)
		if err != nil {
			return nil, fmt.Errorf("%s: provider %q: %w", source, name, err)
		}
		cfg.Providers = append(cfg.Providers, p)
	}
	sort.Slice(cfg.Providers, func(i, j int) bool { return cfg.Providers[i].Name < cfg.Providers[j].Name })

	oauth := 0
	for _, p := range cfg.Providers {
		if p.Auth == AuthXAIOAuth {
			oauth++
		}
	}
	if oauth > 1 {
		return nil, fmt.Errorf("%s: only one provider may use auth = %q, because there is one stored Grok login", source, AuthXAIOAuth)
	}

	cfg.Default = strings.TrimSpace(f.Default)
	switch {
	case cfg.Default == "" && len(cfg.Providers) == 1:
		cfg.Default = cfg.Providers[0].Name
	case cfg.Default == "":
		return nil, fmt.Errorf("%s: set default = \"<provider>\" to choose where models without a prefix go (providers: %s)",
			source, strings.Join(cfg.Names(), ", "))
	case cfg.Provider(cfg.Default) == nil:
		return nil, fmt.Errorf("%s: default = %q is not a provider (providers: %s)", source, cfg.Default, strings.Join(cfg.Names(), ", "))
	}

	for alias, target := range f.Aliases {
		prefix, model, ok := strings.Cut(target, "/")
		if !ok || model == "" || cfg.Provider(prefix) == nil {
			return nil, fmt.Errorf("%s: alias %q = %q must be \"<provider>/<model>\" with a configured provider (providers: %s)",
				source, alias, target, strings.Join(cfg.Names(), ", "))
		}
		if strings.Contains(alias, "/") {
			return nil, fmt.Errorf("%s: alias %q must not contain \"/\"", source, alias)
		}
		cfg.Aliases[alias] = target
	}
	return cfg, nil
}

func buildProvider(name string, fp fileProvider, getenv func(string) string) (*Provider, error) {
	if !providerNamePattern.MatchString(name) {
		return nil, fmt.Errorf("name must be lowercase letters, digits and dashes")
	}
	base := strings.TrimRight(strings.TrimSpace(fp.BaseURL), "/")
	parsed, err := url.Parse(base)
	if base == "" || err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("base_url %q must be an absolute http or https URL, including the version path (e.g. https://api.groq.com/openai/v1)", fp.BaseURL)
	}

	p := &Provider{Name: name, BaseURL: base, Headers: fp.Headers}
	keyName := strings.TrimSpace(fp.Key)
	switch Auth(strings.TrimSpace(fp.Auth)) {
	case "":
		if keyName == "" {
			return nil, fmt.Errorf("set key = \"<SECRET_NAME>\" for an API key, auth = %q for the Grok login, or auth = %q for no credential", AuthXAIOAuth, AuthNone)
		}
		p.Auth = AuthKey
	case AuthKey:
		if keyName == "" {
			return nil, fmt.Errorf("auth = %q needs key = \"<SECRET_NAME>\"", AuthKey)
		}
		p.Auth = AuthKey
	case AuthXAIOAuth:
		p.Auth = AuthXAIOAuth
	case AuthNone:
		p.Auth = AuthNone
	default:
		return nil, fmt.Errorf("auth = %q is not one of %q, %q, %q", fp.Auth, AuthKey, AuthXAIOAuth, AuthNone)
	}

	if p.Auth != AuthKey && keyName != "" {
		return nil, fmt.Errorf("key is only used with auth = %q; remove it or the auth setting", AuthKey)
	}
	if p.Auth == AuthXAIOAuth && !p.IsXAI() {
		return nil, fmt.Errorf("auth = %q only works with base_url on %s; the Grok login is never sent to other hosts", AuthXAIOAuth, xaiHost)
	}
	if p.Auth == AuthKey {
		if !keyNamePattern.MatchString(keyName) {
			return nil, fmt.Errorf("key = %q must be a secret name such as GROQ_API_KEY, not the key itself", keyName)
		}
		if keyName == AdminKeyName {
			return nil, fmt.Errorf("key must not reuse %s, the proxy's own client key", AdminKeyName)
		}
		p.KeyName = keyName
		p.Key = strings.TrimSpace(getenv(keyName))
	}
	return p, nil
}
