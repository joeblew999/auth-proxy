package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joeblew999/grok-oauth-proxy/internal/config"
	"github.com/joeblew999/grok-oauth-proxy/internal/xaiauth"
)

const clientKey = "client-key"

// fakeProvider is an OpenAI-compatible upstream that records what it received.
type fakeProvider struct {
	*httptest.Server
	mu       sync.Mutex
	requests []seenRequest
	models   string
	handle   func(w http.ResponseWriter, r *http.Request, body []byte) // optional override
}

type seenRequest struct {
	Path, Auth, Model, Header string
}

func newFakeProvider(t *testing.T, models string) *fakeProvider {
	t.Helper()
	p := &fakeProvider{models: models}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var probe struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &probe)
		p.mu.Lock()
		p.requests = append(p.requests, seenRequest{Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Model: probe.Model, Header: r.Header.Get("X-Title")})
		p.mu.Unlock()
		if p.handle != nil {
			p.handle(w, r, body)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = io.WriteString(w, p.models)
			return
		}
		fmt.Fprintf(w, `{"model":%q,"choices":[{"message":{"content":"ok"}}]}`, probe.Model)
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *fakeProvider) seen() []seenRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]seenRequest(nil), p.requests...)
}

func loadConfig(t *testing.T, doc string, secrets map[string]string) *config.Config {
	t.Helper()
	cfg, err := config.Load([]byte(doc), "test.toml", func(k string) string { return secrets[k] })
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

func do(t *testing.T, h http.Handler, method, path, body string, key string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func twoProviders(t *testing.T) (http.Handler, *fakeProvider, *fakeProvider) {
	t.Helper()
	main := newFakeProvider(t, `{"data":[{"id":"big-model"}]}`)
	groq := newFakeProvider(t, `{"data":[{"id":"llama-3.3-70b"}]}`)
	cfg := loadConfig(t, fmt.Sprintf(`
default = "main"
[providers.main]
base_url = "%s/v1"
key = "MAIN_KEY"
[providers.groq]
base_url = "%s/openai/v1"
key = "GROQ_API_KEY"
headers = { "X-Title" = "proxy" }
[aliases]
fast = "groq/llama-3.1-8b-instant"
`, main.URL, groq.URL), map[string]string{"ADMIN_API_KEY": clientKey, "MAIN_KEY": "sk-main", "GROQ_API_KEY": "gsk-groq"})
	h := NewHandler(Options{Upstream: &Upstream{Config: cfg, Client: http.DefaultClient, LoginFix: "mise run login"}})
	return h, main, groq
}

func TestRoutesByModelPrefixAliasAndDefault(t *testing.T) {
	h, main, groq := twoProviders(t)

	cases := []struct {
		model        string
		provider     *fakeProvider
		wantModel    string
		wantAuth     string
		wantPath     string
		wantXTitle   string
		providerName string
	}{
		{"groq/llama-3.3-70b", groq, "llama-3.3-70b", "Bearer gsk-groq", "/openai/v1/chat/completions", "proxy", "groq"},
		{"fast", groq, "llama-3.1-8b-instant", "Bearer gsk-groq", "/openai/v1/chat/completions", "proxy", "groq"},
		{"big-model", main, "big-model", "Bearer sk-main", "/v1/chat/completions", "", "main"},
	}
	for _, tc := range cases {
		before := len(tc.provider.seen())
		w := do(t, h, http.MethodPost, "/v1/chat/completions?key=ignored", `{"model":"`+tc.model+`","messages":[]}`, clientKey)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body = %s", tc.model, w.Code, w.Body.String())
		}
		seen := tc.provider.seen()
		if len(seen) != before+1 {
			t.Fatalf("%s: %s saw %d requests, want 1 more", tc.model, tc.providerName, len(seen)-before)
		}
		got := seen[len(seen)-1]
		if got.Model != tc.wantModel || got.Auth != tc.wantAuth || got.Path != tc.wantPath || got.Header != tc.wantXTitle {
			t.Errorf("%s: provider saw %+v", tc.model, got)
		}
	}
}

func TestMissingProviderKeyNamesTheFix(t *testing.T) {
	upstream := newFakeProvider(t, `{"data":[]}`)
	cfg := loadConfig(t, fmt.Sprintf(`[providers.groq]
base_url = "%s/v1"
key = "GROQ_API_KEY"`, upstream.URL), map[string]string{"ADMIN_API_KEY": clientKey})
	h := NewHandler(Options{Upstream: &Upstream{Config: cfg, Client: http.DefaultClient}})

	w := do(t, h, http.MethodPost, "/v1/chat/completions", `{"model":"x"}`, clientKey)
	var body struct {
		Error map[string]string `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusServiceUnavailable || body.Error["fix"] != "mise run keys:set groq" || !strings.Contains(body.Error["message"], "GROQ_API_KEY") {
		t.Errorf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if n := len(upstream.seen()); n != 0 {
		t.Errorf("provider saw %d requests without a key, want 0", n)
	}
}

func TestModelsListMergesProvidersWithPrefixes(t *testing.T) {
	h, _, _ := twoProviders(t)
	w := do(t, h, http.MethodGet, "/v1/models", "", clientKey)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"id":"groq/llama-3.3-70b"`) || !strings.Contains(body, `"id":"main/big-model"`) {
		t.Errorf("models = %s", body)
	}
}

func TestStreamingIsDeliveredIncrementally(t *testing.T) {
	release := make(chan struct{})
	upstream := newFakeProvider(t, "")
	upstream.handle = func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		<-release
		fmt.Fprint(w, "data: [DONE]\n\n")
	}
	cfg := loadConfig(t, fmt.Sprintf(`[providers.local]
base_url = "%s/v1"
auth = "none"`, upstream.URL), map[string]string{"ADMIN_API_KEY": clientKey})
	server := httptest.NewServer(NewHandler(Options{Upstream: &Upstream{Config: cfg, Client: http.DefaultClient}}))
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"m","stream":true}`))
	req.Header.Set("Authorization", "Bearer "+clientKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	firstLine := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(resp.Body).ReadString('\n')
		firstLine <- line
	}()
	select {
	case line := <-firstLine:
		if line != "data: first\n" {
			t.Errorf("first line = %q", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first event not delivered before the stream finished; response is buffered")
	}
	close(release)
	if seen := upstream.seen(); len(seen) != 1 || seen[0].Auth != "" {
		t.Errorf("auth = none provider saw %+v, want no Authorization", seen)
	}
}

func TestClientKeyRequired(t *testing.T) {
	h, main, _ := twoProviders(t)
	if w := do(t, h, http.MethodGet, "/health", "", ""); w.Code != http.StatusOK {
		t.Errorf("/health status = %d, want public 200", w.Code)
	}
	for _, key := range []string{"", "wrong"} {
		if w := do(t, h, http.MethodPost, "/v1/chat/completions", `{"model":"x"}`, key); w.Code != http.StatusUnauthorized {
			t.Errorf("key %q: status = %d, want 401", key, w.Code)
		}
	}
	if n := len(main.seen()); n != 0 {
		t.Errorf("provider saw %d unauthorised requests", n)
	}

	r := httptest.NewRequest(http.MethodGet, "/admin/status", nil)
	r.Header.Set("X-API-Key", clientKey)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("X-API-Key status = %d", w.Code)
	}

	cross := httptest.NewRequest(http.MethodGet, "/admin/status?key="+clientKey, nil)
	cross.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, cross)
	if w.Code != http.StatusForbidden {
		t.Errorf("cross-origin admin status = %d, want 403", w.Code)
	}

	cfg := loadConfig(t, "[providers.a]\nbase_url = \"http://127.0.0.1:1/v1\"\nauth = \"none\"", nil)
	noKey := NewHandler(Options{Upstream: &Upstream{Config: cfg, Client: http.DefaultClient}})
	w = do(t, noKey, http.MethodGet, "/v1/models", "", "anything")
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "mise run keys:set admin") {
		t.Errorf("unset admin key: status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestAdminStatusReportsProblemsAndFixes(t *testing.T) {
	cfg := loadConfig(t, `default = "xai"
[providers.xai]
base_url = "https://api.x.ai/v1"
auth = "xai-oauth"
[providers.groq]
base_url = "https://api.groq.com/openai/v1"
key = "GROQ_API_KEY"`, map[string]string{"ADMIN_API_KEY": clientKey})
	h := NewHandler(Options{Upstream: &Upstream{Config: cfg, Client: http.DefaultClient,
		XAI: xaiauth.New(&memStore{}, http.DefaultClient), LoginFix: "mise run login --worker"}})

	w := do(t, h, http.MethodGet, "/admin/status", "", clientKey)
	var status struct {
		Default   string           `json:"default"`
		Providers []ProviderStatus `json:"providers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body.String())
	}
	fixes := map[string]string{}
	for _, p := range status.Providers {
		if p.Ready {
			t.Errorf("%s ready, want a problem", p.Name)
		}
		fixes[p.Name] = p.Fix
	}
	if status.Default != "xai" || fixes["groq"] != "mise run keys:set groq" || fixes["xai"] != "mise run login --worker" {
		t.Errorf("status = %s", w.Body.String())
	}
}

func TestOAuthAdminRoutesNeedAnOAuthProvider(t *testing.T) {
	h, _, _ := twoProviders(t)
	for _, path := range []string{"/admin/auth/start", "/admin/tokens"} {
		w := do(t, h, http.MethodPost, path, `{}`, clientKey)
		if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "xai-oauth") {
			t.Errorf("%s: status = %d, body = %s", path, w.Code, w.Body.String())
		}
	}
}

// An xai-oauth provider sends the stored Grok token, and refreshes it once when
// xAI rejects it.
func TestXAIOAuthRefreshesOnUnauthorized(t *testing.T) {
	cfg := loadConfig(t, `[providers.xai]
base_url = "https://api.x.ai/v1"
auth = "xai-oauth"`, map[string]string{"ADMIN_API_KEY": clientKey})

	var seenAuth []string
	client := doerFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Host {
		case "auth.x.ai":
			return jsonResponse(http.StatusOK, `{"access_token":"fresh","refresh_token":"r2","expires_in":3600}`), nil
		case "api.x.ai":
			seenAuth = append(seenAuth, r.Header.Get("Authorization"))
			if r.Header.Get("Authorization") == "Bearer stale" {
				return jsonResponse(http.StatusUnauthorized, `{}`), nil
			}
			return jsonResponse(http.StatusOK, `{"choices":[]}`), nil
		}
		t.Errorf("unexpected host %s", r.URL.Host)
		return nil, fmt.Errorf("unexpected host")
	})
	store := &memStore{tokens: &xaiauth.Tokens{AccessToken: "stale", RefreshToken: "r1", ExpiresAt: time.Now().Add(time.Hour).Unix()}}
	h := NewHandler(Options{Upstream: &Upstream{Config: cfg, Client: client, XAI: xaiauth.New(store, client)}})

	w := do(t, h, http.MethodPost, "/v1/chat/completions", `{"model":"grok-4.3"}`, clientKey)
	if w.Code != http.StatusOK || strings.Join(seenAuth, ",") != "Bearer stale,Bearer fresh" {
		t.Errorf("status = %d, xAI saw %v", w.Code, seenAuth)
	}

	empty := NewHandler(Options{Upstream: &Upstream{Config: cfg, Client: client, XAI: xaiauth.New(&memStore{}, client), LoginFix: "mise run login"}})
	w = do(t, empty, http.MethodPost, "/v1/chat/completions", `{"model":"grok-4.3"}`, clientKey)
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "mise run login") {
		t.Errorf("not logged in: status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestTokensHandlerStoresAndRejectsUnknownFields(t *testing.T) {
	cfg := loadConfig(t, `[providers.xai]
base_url = "https://api.x.ai/v1"
auth = "xai-oauth"`, map[string]string{"ADMIN_API_KEY": clientKey})
	store := &memStore{}
	h := NewHandler(Options{Upstream: &Upstream{Config: cfg, Client: http.DefaultClient, XAI: xaiauth.New(store, http.DefaultClient)}})

	expires := time.Now().Add(time.Hour).UnixMilli()
	w := do(t, h, http.MethodPost, "/admin/tokens", fmt.Sprintf(`{"accessToken":"a","refreshToken":"r","expiresAt":%d}`, expires), clientKey)
	if w.Code != http.StatusOK || store.tokens == nil || store.tokens.AccessToken != "a" || store.tokens.ExpiresAt != expires/1000 {
		t.Errorf("status = %d, stored = %+v", w.Code, store.tokens)
	}
	w = do(t, h, http.MethodPost, "/admin/tokens", `{"accessToken":"a","refreshToken":"r","expiresAt":1,"unknown":true}`, clientKey)
	if w.Code != http.StatusBadRequest {
		t.Errorf("unknown field status = %d, want 400", w.Code)
	}
}

// The Worker's fetch throws on a GET or HEAD request with any body, including an
// empty one, which Go's own client tolerates. Every request without a body must
// therefore reach the client with a nil Body.
func TestRequestsWithoutBodyHaveNilBody(t *testing.T) {
	cfg := loadConfig(t, `[providers.a]
base_url = "https://a.test/v1"
auth = "none"`, map[string]string{"ADMIN_API_KEY": clientKey})
	client := doerFunc(func(r *http.Request) (*http.Response, error) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.Body != nil {
			t.Errorf("%s %s has a body; the Worker's fetch rejects this", r.Method, r.URL.Path)
		}
		return jsonResponse(http.StatusOK, `{"data":[{"id":"m"}]}`), nil
	})
	h := NewHandler(Options{Upstream: &Upstream{Config: cfg, Client: client}})
	for _, path := range []string{"/v1/models", "/v1/models/m"} {
		if w := do(t, h, http.MethodGet, path, "", clientKey); w.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, body = %s", path, w.Code, w.Body.String())
		}
	}
}

func TestCopyRequestHeadersStripsCredentialsAndMetadata(t *testing.T) {
	destination := http.Header{}
	copyRequestHeaders(destination, http.Header{
		"Content-Type": {"application/json"}, "X-Stainless-Lang": {"go"},
		"Authorization": {"Bearer client"}, "X-Api-Key": {"client"}, "Cf-Ray": {"r"},
		"X-Forwarded-For": {"1.2.3.4"}, "Transfer-Encoding": {"chunked"},
	})
	if destination.Get("Content-Type") != "application/json" || destination.Get("X-Stainless-Lang") != "go" {
		t.Errorf("client headers dropped: %v", destination)
	}
	for _, key := range []string{"Authorization", "X-Api-Key", "Cf-Ray", "X-Forwarded-For", "Transfer-Encoding"} {
		if destination.Get(key) != "" {
			t.Errorf("%s was forwarded", key)
		}
	}
}

type memStore struct {
	tokens  *xaiauth.Tokens
	session []byte
}

func (s *memStore) LoadTokens() (*xaiauth.Tokens, error) {
	if s.tokens == nil {
		return nil, nil
	}
	copy := *s.tokens
	return &copy, nil
}
func (s *memStore) SaveTokens(t xaiauth.Tokens) error  { s.tokens = &t; return nil }
func (s *memStore) LoadDeviceSession() ([]byte, error) { return s.session, nil }
func (s *memStore) SaveDeviceSession(b []byte) error   { s.session = b; return nil }

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}
