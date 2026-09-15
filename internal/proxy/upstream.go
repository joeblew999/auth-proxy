// Package proxy is the HTTP proxy shared by the local server and the Cloudflare
// Worker. Only the HTTP client and the token store differ between the two.
package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/joeblew999/grok-oauth-proxy/internal/config"
	"github.com/joeblew999/grok-oauth-proxy/internal/router"
	"github.com/joeblew999/grok-oauth-proxy/internal/xaiauth"
)

// Doer sends HTTP requests; *http.Client and the Worker's fetch client both fit.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Error is a failure the client should see, with the command that fixes it.
type Error struct {
	Status   int
	Provider string
	Message  string
	Fix      string
}

func (e *Error) Error() string {
	msg := e.Message
	if e.Provider != "" {
		msg = "provider " + e.Provider + ": " + msg
	}
	if e.Fix != "" {
		msg += " (fix: " + e.Fix + ")"
	}
	return msg
}

// Upstream sends requests to configured providers with the right credentials.
type Upstream struct {
	Config *config.Config
	Client Doer
	// XAI is the Grok login; nil when no provider uses auth = "xai-oauth".
	XAI *xaiauth.Manager
	// LoginFix is the command that logs in to Grok for this runtime.
	LoginFix string
}

// Send forwards one request to a provider. The body is buffered by the caller so
// that an xai-oauth request can be retried once after a token refresh.
func (u *Upstream) Send(ctx context.Context, p *config.Provider, method string, target *url.URL, body []byte, clientHeader http.Header) (*http.Response, error) {
	credential, err := u.credential(ctx, p)
	if err != nil {
		return nil, err
	}
	resp, err := u.send(ctx, p, method, target, body, clientHeader, credential)
	if err != nil || resp.StatusCode != http.StatusUnauthorized || p.Auth != config.AuthXAIOAuth {
		return resp, err
	}

	resp.Body.Close()
	refreshed, err := u.XAI.Refresh(ctx, credential)
	if err != nil {
		return nil, u.loginError(p, err)
	}
	return u.send(ctx, p, method, target, body, clientHeader, refreshed)
}

func (u *Upstream) send(ctx context.Context, p *config.Provider, method string, target *url.URL, body []byte, clientHeader http.Header, credential string) (*http.Response, error) {
	// A nil reader, not an empty one: the Worker's fetch rejects any GET or HEAD
	// request that carries a body, even a zero-length one.
	var reader io.Reader
	if len(body) > 0 {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), reader)
	if err != nil {
		return nil, &Error{Status: http.StatusBadRequest, Provider: p.Name, Message: "invalid upstream request: " + err.Error()}
	}
	copyRequestHeaders(req.Header, clientHeader)
	for key, value := range p.Headers {
		req.Header.Set(key, value)
	}
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	resp, err := u.Client.Do(req)
	if err != nil {
		log.Printf("provider %s: request failed: %v", p.Name, err)
		return nil, &Error{Status: http.StatusBadGateway, Provider: p.Name, Message: "upstream request failed: " + err.Error()}
	}
	return resp, nil
}

// credential returns the bearer token for a provider, or "" for auth = "none".
func (u *Upstream) credential(ctx context.Context, p *config.Provider) (string, error) {
	switch p.Auth {
	case config.AuthKey:
		if problem, fix := p.Problem(); problem != "" {
			return "", &Error{Status: http.StatusServiceUnavailable, Provider: p.Name, Message: problem, Fix: fix}
		}
		return p.Key, nil
	case config.AuthXAIOAuth:
		if u.XAI == nil {
			return "", &Error{Status: http.StatusInternalServerError, Provider: p.Name, Message: "Grok login is not available in this runtime"}
		}
		token, err := u.XAI.Token(ctx)
		if err != nil {
			return "", u.loginError(p, err)
		}
		return token, nil
	default:
		return "", nil
	}
}

func (u *Upstream) loginError(p *config.Provider, err error) error {
	log.Printf("provider %s: %v", p.Name, err)
	message := xaiauth.ErrNotLoggedIn.Error()
	if errors.Is(err, xaiauth.ErrRefreshFailed) {
		message = xaiauth.ErrRefreshFailed.Error()
	}
	return &Error{Status: http.StatusUnauthorized, Provider: p.Name, Message: message, Fix: u.LoginFix}
}

// ListModels fetches /models from every provider concurrently and merges them.
// Providers that fail are left out and returned as errors.
func (u *Upstream) ListModels(ctx context.Context) ([]byte, []error) {
	results := make([]router.ProviderModels, len(u.Config.Providers))
	errs := make([]error, len(u.Config.Providers))
	var wg sync.WaitGroup
	for i, p := range u.Config.Providers {
		wg.Add(1)
		go func(i int, p *config.Provider) {
			defer wg.Done()
			body, err := u.providerModels(ctx, p)
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = router.ProviderModels{Provider: p.Name, IsXAI: p.IsXAI(), Body: body}
		}(i, p)
	}
	wg.Wait()

	var ok []router.ProviderModels
	var failures []error
	for i := range results {
		if errs[i] != nil {
			failures = append(failures, errs[i])
		} else {
			ok = append(ok, results[i])
		}
	}
	merged, mergeErrs := router.MergeModels(ok, len(u.Config.Providers))
	return merged, append(failures, mergeErrs...)
}

func (u *Upstream) providerModels(ctx context.Context, p *config.Provider) ([]byte, error) {
	target, err := url.Parse(p.BaseURL + "/models")
	if err != nil {
		return nil, err
	}
	resp, err := u.Send(ctx, p, http.MethodGet, target, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, &Error{Status: http.StatusBadGateway, Provider: p.Name, Message: "read /models: " + err.Error()}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &Error{Status: resp.StatusCode, Provider: p.Name, Message: fmt.Sprintf("/models returned %d: %s", resp.StatusCode, snippet(body))}
	}
	return body, nil
}

// ProviderStatus is one provider's readiness, as shown by /admin/status.
type ProviderStatus struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	Auth    string `json:"auth"`
	Ready   bool   `json:"ready"`
	Problem string `json:"problem,omitempty"`
	Fix     string `json:"fix,omitempty"`
}

// Status reports every provider's readiness without contacting any provider.
func (u *Upstream) Status() []ProviderStatus {
	statuses := make([]ProviderStatus, 0, len(u.Config.Providers))
	for _, p := range u.Config.Providers {
		s := ProviderStatus{Name: p.Name, BaseURL: p.BaseURL, Auth: string(p.Auth)}
		s.Problem, s.Fix = p.Problem()
		if p.Auth == config.AuthXAIOAuth && (u.XAI == nil || !u.XAI.LoggedIn()) {
			s.Problem, s.Fix = xaiauth.ErrNotLoggedIn.Error(), u.LoginFix
		}
		s.Ready = s.Problem == ""
		statuses = append(statuses, s)
	}
	return statuses
}

func snippet(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 300 {
		text = text[:300] + "..."
	}
	return text
}
