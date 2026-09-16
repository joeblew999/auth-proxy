//go:build !tinygo

package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/joeblew999/grok-oauth-proxy/cmd/proxy/internal/config"
	"github.com/joeblew999/grok-oauth-proxy/cmd/proxy/internal/proxy"
)

const clientKey = "client-key"

// newProxy mounts MCP exactly as the entry points do: behind the proxy handler
// and its client key check, with a fake "groq" provider behind it.
func newProxy(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var seenModels []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = io.WriteString(w, `{"data":[{"id":"llama-3.3-70b"},{"id":"llama-3.1-8b"}]}`)
			return
		}
		var req struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		seenModels = append(seenModels, req.Model)
		fmt.Fprintf(w, `{"model":%q,"choices":[{"message":{"content":"Hello, "}},{"message":{"content":"world!"}}]}`, req.Model)
	}))
	t.Cleanup(provider.Close)

	cfg, err := config.Load([]byte(fmt.Sprintf(`default = "groq"
[providers.groq]
base_url = "%s/openai/v1"
key = "GROQ_API_KEY"
[providers.other]
base_url = "http://127.0.0.1:1/v1"
auth = "none"`, provider.URL)), "test", func(k string) string {
		return map[string]string{"ADMIN_API_KEY": clientKey, "GROQ_API_KEY": "gsk"}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	up := &proxy.Upstream{Config: cfg, Client: http.DefaultClient}
	server := httptest.NewServer(proxy.NewHandler(proxy.Options{Upstream: up, MCP: NewHandler(up)}))
	t.Cleanup(server.Close)
	return server, &seenModels
}

func call(t *testing.T, server *httptest.Server, key string, payload map[string]any) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	// A public hostname, as forwarded by a tunnel or the Worker. The SDK's DNS
	// rebinding check would reject it on a loopback listener.
	req.Host = "proxy.example.com"
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var decoded map[string]any
	if resp.StatusCode == http.StatusOK {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
	}
	return resp.StatusCode, decoded
}

func result(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	r, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", resp)
	}
	return r
}

func toolCall(name string, args map[string]any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}
}

func TestRequiresClientKey(t *testing.T) {
	server, _ := newProxy(t)
	if status, _ := call(t, server, "", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}); status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
}

func TestToolsList(t *testing.T) {
	server, _ := newProxy(t)
	status, resp := call(t, server, clientKey, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	var names []string
	for _, raw := range result(t, resp)["tools"].([]any) {
		tool := raw.(map[string]any)
		names = append(names, tool["name"].(string))
		if desc := tool["description"].(string); !strings.Contains(desc, "groq, other") {
			t.Errorf("%s description %q does not list the providers", tool["name"], desc)
		}
	}
	if strings.Join(names, ",") != "ask,list_models" {
		t.Errorf("tools = %v, want exactly ask and list_models", names)
	}
}

func TestAskRoutesThroughProvider(t *testing.T) {
	server, seen := newProxy(t)
	status, resp := call(t, server, clientKey, toolCall("ask", map[string]any{"model": "groq/llama-3.3-70b", "prompt": "hi"}))
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	structured := result(t, resp)["structuredContent"].(map[string]any)
	if structured["text"] != "Hello, world!" || structured["model"] != "groq/llama-3.3-70b" || structured["requested_model"] != "groq/llama-3.3-70b" {
		t.Errorf("structured = %v", structured)
	}
	if len(*seen) != 1 || (*seen)[0] != "llama-3.3-70b" {
		t.Errorf("provider saw models %v, want the prefix removed", *seen)
	}
}

func TestListModels(t *testing.T) {
	server, _ := newProxy(t)
	_, resp := call(t, server, clientKey, toolCall("list_models", map[string]any{}))
	models := result(t, resp)["structuredContent"].(map[string]any)["models"].([]any)
	if len(models) != 2 || models[0] != "groq/llama-3.1-8b" {
		t.Errorf("models = %v (the unreachable provider should be skipped, the rest prefixed)", models)
	}
}

func TestAskRejectsBlankInput(t *testing.T) {
	server, _ := newProxy(t)
	for args, want := range map[string]string{
		`{"model":"groq/x","prompt":"  "}`: "prompt is required",
		`{"model":" ","prompt":"hello"}`:   "model is required",
	} {
		var parsed map[string]any
		_ = json.Unmarshal([]byte(args), &parsed)
		_, resp := call(t, server, clientKey, toolCall("ask", parsed))
		r := result(t, resp)
		content, _ := json.Marshal(r["content"])
		if r["isError"] != true || !strings.Contains(string(content), want) {
			t.Errorf("args %s: result = %v, want isError with %q", args, r, want)
		}
	}
}

// With one provider, ask reports the served model unprefixed, matching
// list_models.
func TestAskSingleProviderModelIsUnprefixed(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"model":"grok-4.3","choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer provider.Close()
	cfg, err := config.Load([]byte(fmt.Sprintf("[providers.xai]\nbase_url = %q\nauth = \"none\"", provider.URL+"/v1")), "test",
		func(k string) string { return map[string]string{"ADMIN_API_KEY": clientKey}[k] })
	if err != nil {
		t.Fatal(err)
	}
	up := &proxy.Upstream{Config: cfg, Client: http.DefaultClient}
	server := httptest.NewServer(proxy.NewHandler(proxy.Options{Upstream: up, MCP: NewHandler(up)}))
	defer server.Close()

	_, resp := call(t, server, clientKey, toolCall("ask", map[string]any{"model": "grok-4.3", "prompt": "hi"}))
	if got := result(t, resp)["structuredContent"].(map[string]any)["model"]; got != "grok-4.3" {
		t.Errorf("model = %v, want grok-4.3", got)
	}
}
