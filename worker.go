//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"syscall/js"

	workers "github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare"
	"github.com/syumai/workers-go/cloudflare/fetch"
	"github.com/syumai/workers-go/cloudflare/kv"

	"github.com/joeblew999/grok-oauth-proxy/internal/mcp"
	"github.com/joeblew999/grok-oauth-proxy/internal/proxy"
	"github.com/joeblew999/grok-oauth-proxy/internal/xaiauth"
)

const (
	tokensKey  = "oauth-credentials"
	sessionKey = "device-auth"
)

func main() {
	cfg, err := loadConfig("")
	if err != nil {
		// Deploy validates providers.toml first, so this means a bad
		// PROVIDERS_TOML variable. Details go to the logs only.
		log.Printf("config: %v", err)
		workers.Serve(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{
				"message": "proxy configuration is invalid; see the Worker logs",
				"fix":     "mise run logs",
			}})
		}))
		return
	}

	client := newWorkersHTTPClient()
	up := &proxy.Upstream{Config: cfg, Client: client, LoginFix: "mise run login --worker"}
	if cfg.OAuthProvider() != nil {
		store, err := newKVStore()
		if err != nil {
			log.Printf("Grok login unavailable: %v", err)
		} else {
			up.XAI = xaiauth.New(store, client)
		}
	}
	workers.Serve(proxy.NewHandler(proxy.Options{Upstream: up, MCP: mcp.NewHandler(up)}))
}

type workersHTTPClient struct {
	client *fetch.Client
}

// newWorkersHTTPClient uses the Worker's own fetch, or the optional GROK_EGRESS
// Workers VPC binding when one is configured in wrangler.toml.
func newWorkersHTTPClient() *workersHTTPClient {
	binding := cloudflare.GetBinding("GROK_EGRESS")
	if binding.IsUndefined() || binding.IsNull() {
		return &workersHTTPClient{client: fetch.NewClient()}
	}
	namespace := js.Global().Get("Object").New()
	namespace.Set("fetch", binding.Get("fetch").Call("bind", binding))
	return &workersHTTPClient{client: fetch.NewClient(fetch.WithBinding(namespace))}
}

func (c *workersHTTPClient) Do(req *http.Request) (*http.Response, error) {
	// fetch throws on a GET or HEAD request with any body, and http.NoBody counts.
	body := req.Body
	if body == http.NoBody {
		body = nil
	}
	fetchReq, err := fetch.NewRequest(req.Context(), req.Method, req.URL.String(), body)
	if err != nil {
		return nil, err
	}
	for key, values := range req.Header {
		for _, value := range values {
			fetchReq.Header.Set(key, value)
		}
	}
	return c.client.Do(fetchReq, &fetch.RequestInit{Redirect: fetch.RedirectModeManual})
}

// kvStore keeps the Grok login in the GROK_AUTH KV namespace.
type kvStore struct {
	kv *kv.Namespace
}

func newKVStore() (*kvStore, error) {
	namespace, err := kv.NewNamespace("GROK_AUTH")
	if err != nil {
		return nil, fmt.Errorf("KV binding GROK_AUTH: %w", err)
	}
	return &kvStore{kv: namespace}, nil
}

func (s *kvStore) get(key string) ([]byte, error) {
	value, err := s.kv.GetString(key, nil)
	if err != nil {
		return nil, fmt.Errorf("read %s from KV: %w", key, err)
	}
	if value == "" || value == "<null>" {
		return nil, nil
	}
	return []byte(value), nil
}

func (s *kvStore) LoadTokens() (*xaiauth.Tokens, error) {
	raw, err := s.get(tokensKey)
	if err != nil || raw == nil {
		return nil, err
	}
	var tokens xaiauth.Tokens
	if err := json.Unmarshal(raw, &tokens); err != nil {
		return nil, fmt.Errorf("decode stored Grok tokens: %w", err)
	}
	return &tokens, nil
}

func (s *kvStore) SaveTokens(tokens xaiauth.Tokens) error {
	raw, err := json.Marshal(tokens)
	if err != nil {
		return err
	}
	return s.kv.PutString(tokensKey, string(raw), nil)
}

func (s *kvStore) LoadDeviceSession() ([]byte, error) { return s.get(sessionKey) }

func (s *kvStore) SaveDeviceSession(session []byte) error {
	return s.kv.PutString(sessionKey, string(session), nil)
}
