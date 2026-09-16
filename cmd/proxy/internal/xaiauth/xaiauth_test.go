package xaiauth

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

type memStore struct {
	tokens  *Tokens
	session []byte
}

func (s *memStore) LoadTokens() (*Tokens, error) {
	if s.tokens == nil {
		return nil, nil
	}
	copy := *s.tokens
	return &copy, nil
}
func (s *memStore) SaveTokens(t Tokens) error              { s.tokens = &t; return nil }
func (s *memStore) LoadDeviceSession() ([]byte, error)     { return s.session, nil }
func (s *memStore) SaveDeviceSession(session []byte) error { s.session = session; return nil }

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func form(t *testing.T, r *http.Request) url.Values {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatal(err)
	}
	return values
}

func TestTokenRefreshesBeforeExpiry(t *testing.T) {
	store := &memStore{tokens: &Tokens{AccessToken: "expiring", RefreshToken: "existing-refresh", ExpiresAt: time.Now().Add(time.Minute).Unix()}}
	calls := 0
	m := New(store, doerFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		f := form(t, r)
		if r.URL.String() != tokenURL || f.Get("grant_type") != "refresh_token" || f.Get("client_id") != clientID || f.Get("refresh_token") != "existing-refresh" {
			t.Errorf("refresh request = %s %v", r.URL, f)
		}
		return jsonResponse(http.StatusOK, `{"access_token":"refreshed","expires_in":3600}`), nil
	}))

	token, err := m.Token(t.Context())
	if err != nil || token != "refreshed" {
		t.Fatalf("Token() = %q, %v", token, err)
	}
	if calls != 1 || store.tokens.RefreshToken != "existing-refresh" {
		t.Errorf("calls = %d, stored = %+v", calls, store.tokens)
	}

	// Still fresh: no second refresh.
	if token, err := m.Token(t.Context()); err != nil || token != "refreshed" || calls != 1 {
		t.Errorf("second Token() = %q, %v, calls = %d", token, err, calls)
	}
}

func TestTokenErrors(t *testing.T) {
	never := doerFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unexpected HTTP call")
		return nil, errors.New("unexpected")
	})
	if _, err := New(&memStore{}, never).Token(t.Context()); !errors.Is(err, ErrNotLoggedIn) {
		t.Errorf("empty store: err = %v, want ErrNotLoggedIn", err)
	}

	failing := New(&memStore{tokens: &Tokens{AccessToken: "old", RefreshToken: "r", ExpiresAt: 1}}, doerFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(http.StatusBadRequest, `{}`), nil
	}))
	if _, err := failing.Token(t.Context()); !errors.Is(err, ErrRefreshFailed) {
		t.Errorf("failed refresh: err = %v, want ErrRefreshFailed", err)
	}
}

func TestRefreshSkipsWhenTokenAlreadyReplaced(t *testing.T) {
	store := &memStore{tokens: &Tokens{AccessToken: "new", RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour).Unix()}}
	m := New(store, doerFunc(func(*http.Request) (*http.Response, error) {
		t.Error("refreshed although another request already replaced the token")
		return nil, errors.New("unexpected")
	}))
	if token, err := m.Refresh(t.Context(), "old"); err != nil || token != "new" {
		t.Errorf("Refresh() = %q, %v", token, err)
	}
}

func TestStoreTokensKeepsExistingRefreshToken(t *testing.T) {
	store := &memStore{tokens: &Tokens{AccessToken: "a", RefreshToken: "keep"}}
	m := New(store, nil)
	expires := time.Now().Add(time.Hour)
	if err := m.StoreTokens(" manual ", "", expires); err != nil {
		t.Fatal(err)
	}
	if store.tokens.AccessToken != "manual" || store.tokens.RefreshToken != "keep" || store.tokens.ExpiresAt != expires.Unix() {
		t.Errorf("stored = %+v", store.tokens)
	}
	if err := New(&memStore{}, nil).StoreTokens("a", "", expires); err == nil {
		t.Error("StoreTokens accepted no refresh token with none stored")
	}
}

func TestTokensFromJSONRejectsIncompleteResponse(t *testing.T) {
	if _, err := tokensFromJSON([]byte(`{"refresh_token":"r","expires_in":3600}`), "", time.Now()); err == nil {
		t.Error("accepted a response without an access token")
	}
	if _, err := tokensFromJSON([]byte(`{"access_token":"a"}`), "", time.Now()); err == nil {
		t.Error("accepted a response without any refresh token")
	}
}

func TestDeviceFlowStartAndPoll(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	store := &memStore{}
	requests := 0
	m := New(store, doerFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		f := form(t, r)
		if requests == 1 {
			if r.URL.String() != deviceCodeURL || f.Get("client_id") != clientID || f.Get("scope") != scope {
				t.Errorf("device request = %s %v", r.URL, f)
			}
			return jsonResponse(http.StatusOK, `{"device_code":"device","user_code":"ABCD-EFGH","verification_uri":"https://auth.x.ai/activate","verification_uri_complete":"https://auth.x.ai/activate?user_code=ABCD-EFGH","expires_in":900,"interval":5}`), nil
		}
		if r.URL.String() != tokenURL || f.Get("device_code") != "device" {
			t.Errorf("poll request = %s %v", r.URL, f)
		}
		return jsonResponse(http.StatusOK, `{"access_token":"access","refresh_token":"refresh","expires_in":3600}`), nil
	}))
	m.now = func() time.Time { return now }

	started, err := m.StartDevice(t.Context())
	if err != nil || started.Status != "pending" || started.UserCode != "ABCD-EFGH" || !strings.HasPrefix(started.VerificationURL, "https://auth.x.ai/") {
		t.Fatalf("StartDevice() = %+v, %v", started, err)
	}
	if again, _ := m.StartDevice(t.Context()); again.UserCode != "ABCD-EFGH" || requests != 1 {
		t.Errorf("second StartDevice() = %+v, requests = %d; want the pending session reused", again, requests)
	}

	now = now.Add(5 * time.Second)
	done, err := m.PollDevice(t.Context())
	if err != nil || done.Status != "authenticated" || !done.Terminal() {
		t.Fatalf("PollDevice() = %+v, %v", done, err)
	}
	if store.tokens == nil || store.tokens.AccessToken != "access" || !m.LoggedIn() {
		t.Errorf("stored tokens = %+v", store.tokens)
	}
}

func TestDevicePollWaitsForInterval(t *testing.T) {
	now := time.Now()
	store := &memStore{session: []byte(`{"status":"pending","deviceCode":"device","userCode":"code","verificationUri":"https://auth.x.ai/activate","expiresAt":` +
		strconv.FormatInt(now.Add(time.Minute).UnixMilli(), 10) + `,"intervalMs":5000,"nextPollAt":` + strconv.FormatInt(now.Add(5*time.Second).UnixMilli(), 10) + `}`)}
	m := New(store, doerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("polled xAI before the interval")
		return nil, nil
	}))
	m.now = func() time.Time { return now }
	status, err := m.PollDevice(t.Context())
	if err != nil || status.Status != "pending" || status.RetryAfterSeconds != 5 || status.Terminal() {
		t.Errorf("PollDevice() = %+v, %v", status, err)
	}
}

func TestTrustedVerificationURL(t *testing.T) {
	if _, err := trustedVerificationURL("http://auth.x.ai/activate"); err == nil {
		t.Error("accepted plain HTTP")
	}
	if got, err := trustedVerificationURL("https://auth.x.ai/activate"); err != nil || got == "" {
		t.Errorf("trustedVerificationURL() = %q, %v", got, err)
	}
}

func TestFileStoreRoundTrip(t *testing.T) {
	store := FileStore{Dir: t.TempDir()}
	if tokens, err := store.LoadTokens(); tokens != nil || err != nil {
		t.Fatalf("empty LoadTokens() = %v, %v; want nil, nil", tokens, err)
	}
	if err := store.SaveTokens(Tokens{AccessToken: "a", RefreshToken: "r", ExpiresAt: 42}); err != nil {
		t.Fatal(err)
	}
	if tokens, err := store.LoadTokens(); err != nil || tokens.AccessToken != "a" || tokens.ExpiresAt != 42 {
		t.Errorf("LoadTokens() = %+v, %v", tokens, err)
	}
	if err := store.SaveDeviceSession([]byte(`{"status":"idle"}`)); err != nil {
		t.Fatal(err)
	}
	if session, err := store.LoadDeviceSession(); err != nil || string(session) != `{"status":"idle"}` {
		t.Errorf("LoadDeviceSession() = %s, %v", session, err)
	}
}

func TestBrowserLoginRejectsUnknownState(t *testing.T) {
	login := New(&memStore{}, nil).NewBrowserLogin("http://127.0.0.1:56121/callback")

	start := httptest.NewRecorder()
	login.Start(start, httptest.NewRequest(http.MethodGet, "/login", nil))
	location, err := url.Parse(start.Header().Get("Location"))
	if err != nil || start.Code != http.StatusTemporaryRedirect || location.Host != "auth.x.ai" || location.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("Start redirect = %d %s", start.Code, start.Header().Get("Location"))
	}

	callback := httptest.NewRecorder()
	login.Callback(callback, httptest.NewRequest(http.MethodGet, "/callback?code=c&state=forged", nil))
	if callback.Code != http.StatusBadRequest {
		t.Errorf("forged state status = %d, want 400", callback.Code)
	}
}
