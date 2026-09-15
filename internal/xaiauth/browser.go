package xaiauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"sync"
)

// BrowserLogin is the PKCE authorization-code login used by the local proxy:
// the user opens Start in a browser, xAI redirects to Callback, and Done closes
// once tokens are saved.
type BrowserLogin struct {
	manager     *Manager
	redirectURI string
	done        chan struct{}
	once        sync.Once

	mu        sync.Mutex
	verifiers map[string]string // state -> PKCE verifier
}

// NewBrowserLogin prepares a login whose callback is served at redirectURI.
func (m *Manager) NewBrowserLogin(redirectURI string) *BrowserLogin {
	return &BrowserLogin{manager: m, redirectURI: redirectURI, done: make(chan struct{}), verifiers: map[string]string{}}
}

// Done is closed after a successful login.
func (b *BrowserLogin) Done() <-chan struct{} { return b.done }

// Start redirects the browser to xAI's authorization page.
func (b *BrowserLogin) Start(w http.ResponseWriter, r *http.Request) {
	state, err1 := randomString(16)
	nonce, err2 := randomString(16)
	verifier, err3 := randomString(32)
	if err1 != nil || err2 != nil || err3 != nil {
		http.Error(w, "failed to start Grok login", http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256([]byte(verifier))

	b.mu.Lock()
	b.verifiers[state] = verifier
	b.mu.Unlock()

	u, _ := url.Parse(authorizeURL)
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", b.redirectURI)
	q.Set("scope", scope)
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("plan", "generic")
	q.Set("referrer", "hermes-agent")
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusTemporaryRedirect)
}

// Callback exchanges the authorization code for tokens and saves them.
func (b *BrowserLogin) Callback(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if desc := query.Get("error_description"); desc != "" {
		http.Error(w, "Grok login failed: "+desc, http.StatusBadRequest)
		return
	}
	code, state := query.Get("code"), query.Get("state")
	if code == "" || state == "" {
		http.Error(w, "missing code or state", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	verifier, ok := b.verifiers[state]
	delete(b.verifiers, state)
	b.mu.Unlock()
	if !ok {
		http.Error(w, "invalid state; start the login again", http.StatusBadRequest)
		return
	}

	tokens, err := b.manager.requestTokens(r.Context(), url.Values{
		"grant_type":            {"authorization_code"},
		"code":                  {code},
		"redirect_uri":          {b.redirectURI},
		"client_id":             {clientID},
		"code_verifier":         {verifier},
		"code_challenge_method": {"S256"},
	}, "")
	if err != nil {
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}
	if err := b.manager.store.SaveTokens(*tokens); err != nil {
		http.Error(w, "failed to save tokens: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte(`<html><body><h1>Logged in to Grok</h1><p>You can close this tab.</p></body></html>`))
	b.once.Do(func() { close(b.done) })
}

func randomString(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
