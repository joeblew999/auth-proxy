package worker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	wranglerFile = "wrangler.toml"
	localFile    = "mise.local.toml"
	subdomainKey = "CLOUDFLARE_WORKERS_SUBDOMAIN"
)

var subdomainEndpoint = "https://api.cloudflare.com/client/v4/accounts/%s/workers/subdomain"

// wranglerConfig is the little of wrangler.toml a URL needs.
type wranglerConfig struct {
	Name string `toml:"name"`
	Env  map[string]struct {
		Name string `toml:"name"`
	} `toml:"env"`
}

func readWrangler(path string) (wranglerConfig, error) {
	var cfg wranglerConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	if cfg.Name == "" {
		return cfg, fmt.Errorf("%s has no name; add one", path)
	}
	return cfg, nil
}

// workerName is the Worker wrangler deploys env to: the environment's own
// name when it sets one, otherwise <name>-<env>, and plain <name> for the
// top-level environment.
func workerName(cfg wranglerConfig, env string) string {
	if env == "" {
		return cfg.Name
	}
	if e, ok := cfg.Env[env]; ok && e.Name != "" {
		return e.Name
	}
	return cfg.Name + "-" + env
}

// URL is the address to talk to: the workers.dev URL of the Worker in dir when
// worker is set, otherwise local as given.
func URL(dir, env string, worker bool, local string, refresh bool) (string, error) {
	if !worker {
		return local, nil
	}
	cfg, err := readWrangler(filepath.Join(dir, wranglerFile))
	if err != nil {
		return "", err
	}
	sub, err := subdomain(refresh)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://%s.%s.workers.dev", workerName(cfg, env), sub), nil
}

// subdomain is the account's workers.dev subdomain: the one fact a URL needs
// that no committed file may hold. It is read from the API once and kept in
// mise.local.toml.
func subdomain(refresh bool) (string, error) {
	if !refresh {
		env, err := readLocal(localFile)
		if err != nil {
			return "", err
		}
		if env[subdomainKey] != "" {
			return env[subdomainKey], nil
		}
	}
	token, err := credential("CLOUDFLARE_API_TOKEN")
	if err != nil {
		return "", err
	}
	account, err := credential("CLOUDFLARE_ACCOUNT_ID")
	if err != nil {
		return "", err
	}
	sub, err := fetchSubdomain(token, account)
	if err != nil {
		return "", err
	}
	if err := writeLocal(localFile, subdomainKey, sub); err != nil {
		return "", err
	}
	return sub, nil
}

func credential(name string) (string, error) {
	v, err := fnoxGet(name)
	if err != nil || v == "" {
		return "", fmt.Errorf("%s is not in fnox; store it with: fnox set -g %s", name, name)
	}
	return v, nil
}

func fetchSubdomain(token, account string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf(subdomainEndpoint, account), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("asking Cloudflare for the workers.dev subdomain: %w", err)
	}
	defer resp.Body.Close()
	var body struct {
		Success bool `json:"success"`
		Result  struct {
			Subdomain string `json:"subdomain"`
		} `json:"result"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != http.StatusOK || !body.Success || body.Result.Subdomain == "" {
		var msgs []string
		for _, e := range body.Errors {
			msgs = append(msgs, e.Message)
		}
		detail := ""
		if len(msgs) > 0 {
			detail = ": " + strings.Join(msgs, "; ")
		}
		return "", fmt.Errorf("could not read the account's workers.dev subdomain (HTTP %d%s); check that CLOUDFLARE_API_TOKEN in fnox can read Workers and CLOUDFLARE_ACCOUNT_ID is the account that owns them", resp.StatusCode, detail)
	}
	return body.Result.Subdomain, nil
}

// mise.local.toml is this clone's, gitignored, and mise reads its [env] like
// any other config file, so what dev learns about the account is also in the
// shell environment.

type localConfig struct {
	Env map[string]string `toml:"env"`
}

func readLocal(path string) (map[string]string, error) {
	var c localConfig
	if _, err := toml.DecodeFile(path, &c); err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("%s: %w (it is this clone's file; fix or delete it)", path, err)
	}
	if c.Env == nil {
		c.Env = map[string]string{}
	}
	return c.Env, nil
}

func writeLocal(path, key, value string) error {
	env, err := readLocal(path)
	if err != nil {
		return err
	}
	env[key] = value
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# Written by `dev url` from the Cloudflare account in fnox. Gitignored: it is\n")
	b.WriteString("# this clone's. After switching accounts: mise run url --refresh\n")
	b.WriteString("[env]\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s = %s\n", k, strconv.Quote(env[k]))
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
