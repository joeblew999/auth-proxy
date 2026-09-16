// Package xaiauth handles the Grok subscription login: the browser (PKCE) login
// used locally, the device flow used by the Worker, token storage, and refresh.
//
// Tokens from this package are only ever sent to api.x.ai; internal/config
// rejects an xai-oauth provider on any other host.
package xaiauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	clientID           = "b1a00492-073a-47ea-816f-4c329264a828"
	scope              = "openid profile email offline_access grok-cli:access api:access"
	authorizeURL       = "https://auth.x.ai/oauth2/authorize"
	deviceCodeURL      = "https://auth.x.ai/oauth2/device/code"
	tokenURL           = "https://auth.x.ai/oauth2/token"
	refreshSkew        = 5 * time.Minute
	requestTimeout     = 30 * time.Second
	responseLimit      = 1 << 20
	defaultTokenExpiry = 3600
)

var (
	// ErrNotLoggedIn means no Grok tokens are stored.
	ErrNotLoggedIn = errors.New("not logged in to Grok")
	// ErrRefreshFailed means stored tokens exist but could not be refreshed.
	ErrRefreshFailed = errors.New("Grok login expired and could not be refreshed")
)

// Tokens are the stored Grok OAuth credentials. ExpiresAt is Unix seconds.
type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
}

// Store persists tokens and the in-progress device flow session.
type Store interface {
	// LoadTokens returns nil, nil when no tokens are stored.
	LoadTokens() (*Tokens, error)
	SaveTokens(Tokens) error
	// LoadDeviceSession returns nil, nil when no session is stored.
	LoadDeviceSession() ([]byte, error)
	SaveDeviceSession([]byte) error
}

// Doer sends HTTP requests; *http.Client and the Worker's fetch client both fit.
type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

// Manager owns the Grok login for one token store.
type Manager struct {
	store  Store
	client Doer
	now    func() time.Time

	refreshMu sync.Mutex
	deviceMu  sync.Mutex
}

// New returns a Manager backed by store, sending OAuth requests with client.
func New(store Store, client Doer) *Manager {
	return &Manager{store: store, client: client, now: time.Now}
}

// LoggedIn reports whether an access token is stored.
func (m *Manager) LoggedIn() bool {
	tokens, err := m.store.LoadTokens()
	return err == nil && tokens != nil && tokens.AccessToken != ""
}

// Token returns a usable access token, refreshing it shortly before it expires.
func (m *Manager) Token(ctx context.Context) (string, error) {
	tokens, err := m.load()
	if err != nil {
		return "", err
	}
	if tokens.ExpiresAt > m.now().Add(refreshSkew).Unix() {
		return tokens.AccessToken, nil
	}

	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	if tokens, err = m.load(); err != nil {
		return "", err
	}
	if tokens.ExpiresAt > m.now().Add(refreshSkew).Unix() {
		return tokens.AccessToken, nil
	}
	return m.refreshAndSave(ctx, tokens)
}

// Refresh forces a refresh after the upstream rejected failedToken, unless
// another request already replaced it.
func (m *Manager) Refresh(ctx context.Context, failedToken string) (string, error) {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	tokens, err := m.load()
	if err != nil {
		return "", err
	}
	if tokens.AccessToken != failedToken {
		return tokens.AccessToken, nil
	}
	return m.refreshAndSave(ctx, tokens)
}

// StoreTokens saves manually supplied tokens. An empty refresh token keeps the
// stored one, and one of the two must exist.
func (m *Manager) StoreTokens(accessToken, refreshToken string, expiresAt time.Time) error {
	accessToken, refreshToken = strings.TrimSpace(accessToken), strings.TrimSpace(refreshToken)
	if accessToken == "" {
		return errors.New("access token is required")
	}
	if refreshToken == "" {
		if current, err := m.store.LoadTokens(); err == nil && current != nil {
			refreshToken = current.RefreshToken
		}
	}
	if refreshToken == "" {
		return errors.New("refresh token is required when none is stored")
	}
	return m.store.SaveTokens(Tokens{AccessToken: accessToken, RefreshToken: refreshToken, ExpiresAt: expiresAt.Unix()})
}

func (m *Manager) load() (*Tokens, error) {
	tokens, err := m.store.LoadTokens()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotLoggedIn, err)
	}
	if tokens == nil || tokens.AccessToken == "" {
		return nil, ErrNotLoggedIn
	}
	return tokens, nil
}

func (m *Manager) refreshAndSave(ctx context.Context, tokens *Tokens) (string, error) {
	if strings.TrimSpace(tokens.RefreshToken) == "" {
		return "", fmt.Errorf("%w: no refresh token stored", ErrRefreshFailed)
	}
	fresh, err := m.requestTokens(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {tokens.RefreshToken},
	}, tokens.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrRefreshFailed, err)
	}
	if err := m.store.SaveTokens(*fresh); err != nil {
		return "", fmt.Errorf("%w: save refreshed tokens: %v", ErrRefreshFailed, err)
	}
	return fresh.AccessToken, nil
}

func (m *Manager) requestTokens(ctx context.Context, form url.Values, previousRefreshToken string) (*Tokens, error) {
	status, body, err := m.postForm(ctx, tokenURL, form)
	if err != nil {
		return nil, fmt.Errorf("send token request: %w", err)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("token request failed with status %d", status)
	}
	tokens, err := tokensFromJSON(body, previousRefreshToken, m.now())
	if err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	return tokens, nil
}

func (m *Manager) postForm(ctx context.Context, endpoint string, form url.Values) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := m.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit))
	return resp.StatusCode, body, err
}

func tokensFromJSON(body []byte, previousRefreshToken string, now time.Time) (*Tokens, error) {
	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.AccessToken == "" {
		return nil, errors.New("invalid token response")
	}
	if result.RefreshToken == "" {
		result.RefreshToken = previousRefreshToken
	}
	if result.RefreshToken == "" {
		return nil, errors.New("token response did not include a refresh token")
	}
	if result.ExpiresIn == 0 {
		result.ExpiresIn = defaultTokenExpiry
	}
	if result.ExpiresIn < 0 {
		return nil, errors.New("invalid token expiry")
	}
	return &Tokens{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    now.Add(time.Duration(result.ExpiresIn) * time.Second).Unix(),
	}, nil
}
