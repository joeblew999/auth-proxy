package router

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/joeblew999/grok-oauth-proxy/internal/config"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load([]byte(`
default = "xai"
[providers.xai]
base_url = "https://api.x.ai/v1"
auth = "xai-oauth"
[providers.groq]
base_url = "https://api.groq.com/openai/v1"
key = "GROQ_API_KEY"
[providers.cloudflare]
base_url = "https://api.cloudflare.com/client/v4/accounts/abc/ai/v1"
key = "CLOUDFLARE_AI_TOKEN"
[aliases]
fast = "groq/llama-3.1-8b-instant"
`), "test", func(string) string { return "" })
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

func TestResolve(t *testing.T) {
	cfg := testConfig(t)
	cases := []struct{ model, provider, upstreamModel string }{
		{"groq/llama-3.3-70b-versatile", "groq", "llama-3.3-70b-versatile"},
		{"xai/grok-4.3", "xai", "grok-4.3"},
		{"grok-4.3", "xai", "grok-4.3"},                               // no prefix: default
		{"cloudflare/openai/gpt-5.5", "cloudflare", "openai/gpt-5.5"}, // only the first segment is a prefix
		{"meta-llama/llama-3-8b", "xai", "meta-llama/llama-3-8b"},     // slash but no provider: default, unchanged
		{"fast", "groq", "llama-3.1-8b-instant"},                      // alias
		{"", "xai", ""},                                               // no model at all
		{"groq/", "xai", "groq/"},                                     // empty model after prefix is not a route
	}
	for _, tc := range cases {
		route := Resolve(cfg, tc.model)
		if route.Provider.Name != tc.provider || route.Model != tc.upstreamModel {
			t.Errorf("Resolve(%q) = %s, %q; want %s, %q", tc.model, route.Provider.Name, route.Model, tc.provider, tc.upstreamModel)
		}
	}

	if got := Resolve(cfg, "meta-llama/llama-3-8b").UnknownPrefix; got != "meta-llama" {
		t.Errorf("UnknownPrefix = %q, want meta-llama", got)
	}
	for _, model := range []string{"groq/llama", "grok-4.3", "fast"} {
		if got := Resolve(cfg, model).UnknownPrefix; got != "" {
			t.Errorf("Resolve(%q).UnknownPrefix = %q, want none", model, got)
		}
	}
}

// Real providers put their version path under a prefix. The incoming /v1 must be
// replaced by the base path, never appended to it.
func TestUpstreamURL(t *testing.T) {
	cfg := testConfig(t)
	cases := []struct{ provider, incoming, want string }{
		{"xai", "/v1/chat/completions?key=admin&foo=bar", "https://api.x.ai/v1/chat/completions?foo=bar"},
		{"groq", "/v1/chat/completions", "https://api.groq.com/openai/v1/chat/completions"},
		{"groq", "/chat/completions", "https://api.groq.com/openai/v1/chat/completions"},
		{"cloudflare", "/v1/models", "https://api.cloudflare.com/client/v4/accounts/abc/ai/v1/models"},
		{"groq", "/v1beta/thing", "https://api.groq.com/openai/v1/v1beta/thing"}, // only an exact /v1 segment is dropped
	}
	for _, tc := range cases {
		incoming, err := url.Parse(tc.incoming)
		if err != nil {
			t.Fatal(err)
		}
		got, err := UpstreamURL(cfg.Provider(tc.provider), incoming)
		if err != nil || got.String() != tc.want {
			t.Errorf("UpstreamURL(%s, %q) = %v, %v; want %q", tc.provider, tc.incoming, got, err, tc.want)
		}
	}
}

func TestRewriteModelKeepsOtherFields(t *testing.T) {
	body := []byte(`{"model":"groq/llama","stream":true,"messages":[{"role":"user","content":"hi"}],"temperature":0.2}`)
	if got := ModelFromBody(body); got != "groq/llama" {
		t.Fatalf("ModelFromBody() = %q", got)
	}
	rewritten, err := RewriteModel(body, "llama")
	if err != nil {
		t.Fatalf("RewriteModel() error = %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(rewritten, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["model"] != "llama" || fields["stream"] != true || fields["temperature"] != 0.2 || len(fields["messages"].([]any)) != 1 {
		t.Errorf("rewritten = %s", rewritten)
	}
	if _, err := RewriteModel([]byte(`[1,2]`), "x"); err == nil {
		t.Error("RewriteModel accepted a non-object body")
	}
}

func TestMergeModels(t *testing.T) {
	results := []ProviderModels{
		{Provider: "xai", IsXAI: true, Body: []byte(`{"object":"list","data":[{"id":"grok-4.3","object":"model"}]}`)},
		{Provider: "groq", Body: []byte(`{"data":[{"id":"llama-3.3-70b"}]}`)},
		{Provider: "broken", Body: []byte(`<html>`)},
	}
	body, errs := MergeModels(results, 3)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "broken") {
		t.Errorf("errs = %v, want one error naming the broken provider", errs)
	}
	var list struct {
		Object string  `json:"object"`
		Data   []Model `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range list.Data {
		ids = append(ids, m.ID)
		if m.Object != "model" {
			t.Errorf("model %s object = %q", m.ID, m.Object)
		}
	}
	if got := strings.Join(ids, ","); got != "groq/llama-3.3-70b,xai/grok-4.3,xai/grok-composer-2.5-fast" {
		t.Errorf("ids = %s", got)
	}

	single, _ := MergeModels(results[1:2], 1)
	if !strings.Contains(string(single), `"id":"llama-3.3-70b"`) {
		t.Errorf("single provider list = %s, want unprefixed IDs", single)
	}
	empty, _ := MergeModels(nil, 2)
	if string(empty) != `{"object":"list","data":[]}` {
		t.Errorf("empty list = %s", empty)
	}
}
