package xaiauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"time"
)

// FlowError is a device flow failure whose message is safe to show the user.
type FlowError struct{ Message string }

func (e *FlowError) Error() string { return e.Message }

// DeviceStatus is the client-facing state of the device flow.
type DeviceStatus struct {
	Status            string `json:"status"` // idle, pending, authenticated, denied, expired, failed
	VerificationURL   string `json:"verificationUrl,omitempty"`
	UserCode          string `json:"userCode,omitempty"`
	ExpiresAt         string `json:"expiresAt,omitempty"`
	RetryAfterSeconds int64  `json:"retryAfterSeconds,omitempty"`
}

// Terminal reports whether polling should stop.
func (s DeviceStatus) Terminal() bool {
	switch s.Status {
	case "authenticated", "denied", "expired", "failed", "idle":
		return true
	}
	return false
}

type deviceSession struct {
	Status                  string `json:"status"`
	DeviceCode              string `json:"deviceCode,omitempty"`
	UserCode                string `json:"userCode,omitempty"`
	VerificationURI         string `json:"verificationUri,omitempty"`
	VerificationURIComplete string `json:"verificationUriComplete,omitempty"`
	ExpiresAt               int64  `json:"expiresAt,omitempty"`
	IntervalMS              int64  `json:"intervalMs,omitempty"`
	NextPollAt              int64  `json:"nextPollAt,omitempty"`
}

// StartDevice begins a device flow, or returns the one already pending.
func (m *Manager) StartDevice(ctx context.Context) (DeviceStatus, error) {
	m.deviceMu.Lock()
	defer m.deviceMu.Unlock()

	current, err := m.deviceStatus()
	if err != nil {
		return DeviceStatus{}, err
	}
	if current.Status == "pending" {
		return current, nil
	}

	status, body, err := m.postForm(ctx, deviceCodeURL, url.Values{
		"client_id": {clientID},
		"scope":     {scope},
		"referrer":  {"pi"},
	})
	if err != nil {
		return DeviceStatus{}, err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return DeviceStatus{}, &FlowError{Message: fmt.Sprintf("device authorization could not be started (HTTP %d)", status)}
	}
	var started struct {
		DeviceCode              string  `json:"device_code"`
		UserCode                string  `json:"user_code"`
		VerificationURI         string  `json:"verification_uri"`
		VerificationURIComplete string  `json:"verification_uri_complete"`
		ExpiresIn               float64 `json:"expires_in"`
		Interval                float64 `json:"interval"`
	}
	if err := json.Unmarshal(body, &started); err != nil || started.DeviceCode == "" || started.UserCode == "" || started.ExpiresIn <= 0 {
		return DeviceStatus{}, &FlowError{Message: "invalid device authorization response"}
	}
	verification, err := trustedVerificationURL(started.VerificationURI)
	if err != nil {
		return DeviceStatus{}, &FlowError{Message: err.Error()}
	}
	complete := ""
	if started.VerificationURIComplete != "" {
		if complete, err = trustedVerificationURL(started.VerificationURIComplete); err != nil {
			return DeviceStatus{}, &FlowError{Message: err.Error()}
		}
	}
	interval := time.Duration(started.Interval * float64(time.Second))
	if interval <= 0 {
		interval = 5 * time.Second
	}
	now := m.now()
	session := deviceSession{
		Status:                  "pending",
		DeviceCode:              started.DeviceCode,
		UserCode:                started.UserCode,
		VerificationURI:         verification,
		VerificationURIComplete: complete,
		ExpiresAt:               now.Add(time.Duration(started.ExpiresIn * float64(time.Second))).UnixMilli(),
		IntervalMS:              interval.Milliseconds(),
		NextPollAt:              now.Add(interval).UnixMilli(),
	}
	if err := m.saveSession(session); err != nil {
		return DeviceStatus{}, err
	}
	return m.view(session), nil
}

// DeviceStatus reports the device flow state without contacting xAI.
func (m *Manager) DeviceStatus() (DeviceStatus, error) {
	m.deviceMu.Lock()
	defer m.deviceMu.Unlock()
	return m.deviceStatus()
}

// PollDevice checks with xAI whether the user has approved, respecting the
// polling interval xAI asked for.
func (m *Manager) PollDevice(ctx context.Context) (DeviceStatus, error) {
	m.deviceMu.Lock()
	defer m.deviceMu.Unlock()

	session, ok, err := m.loadSession()
	if err != nil {
		return DeviceStatus{}, err
	}
	if !ok {
		return DeviceStatus{Status: "idle"}, nil
	}
	now := m.now().UnixMilli()
	if session.Status == "pending" && now >= session.ExpiresAt {
		return m.finish("expired")
	}
	if session.Status != "pending" || now < session.NextPollAt {
		return m.view(session), nil
	}

	session.NextPollAt = now + session.IntervalMS
	if err := m.saveSession(session); err != nil {
		return DeviceStatus{}, err
	}
	status, body, err := m.postForm(ctx, tokenURL, url.Values{
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"client_id":   {clientID},
		"device_code": {session.DeviceCode},
	})
	if err != nil {
		return DeviceStatus{}, err
	}
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		tokens, err := tokensFromJSON(body, "", m.now())
		if err != nil {
			return m.finish("failed")
		}
		if err := m.store.SaveTokens(*tokens); err != nil {
			return DeviceStatus{}, err
		}
		return m.finish("authenticated")
	}

	code, retry := deviceFailure(body)
	switch code {
	case "authorization_pending":
		return m.view(session), nil
	case "slow_down":
		if retry > 0 {
			session.IntervalMS = retry.Milliseconds()
		} else {
			session.IntervalMS += (5 * time.Second).Milliseconds()
		}
		session.NextPollAt = m.now().UnixMilli() + session.IntervalMS
		if err := m.saveSession(session); err != nil {
			return DeviceStatus{}, err
		}
		return m.view(session), nil
	case "access_denied", "authorization_denied":
		return m.finish("denied")
	case "expired_token":
		return m.finish("expired")
	default:
		if status >= http.StatusInternalServerError {
			return m.view(session), nil
		}
		return m.finish("failed")
	}
}

func (m *Manager) deviceStatus() (DeviceStatus, error) {
	session, ok, err := m.loadSession()
	if err != nil {
		return DeviceStatus{}, err
	}
	if !ok {
		return DeviceStatus{Status: "idle"}, nil
	}
	if session.Status == "pending" && m.now().UnixMilli() >= session.ExpiresAt {
		return m.finish("expired")
	}
	return m.view(session), nil
}

func (m *Manager) loadSession() (deviceSession, bool, error) {
	encoded, err := m.store.LoadDeviceSession()
	if err != nil {
		return deviceSession{}, false, err
	}
	if len(encoded) == 0 {
		return deviceSession{}, false, nil
	}
	var session deviceSession
	if err := json.Unmarshal(encoded, &session); err != nil {
		return deviceSession{}, false, fmt.Errorf("invalid stored device session: %w", err)
	}
	return session, true, nil
}

func (m *Manager) saveSession(session deviceSession) error {
	encoded, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return m.store.SaveDeviceSession(encoded)
}

func (m *Manager) finish(status string) (DeviceStatus, error) {
	if err := m.saveSession(deviceSession{Status: status}); err != nil {
		return DeviceStatus{}, err
	}
	return DeviceStatus{Status: status}, nil
}

func (m *Manager) view(session deviceSession) DeviceStatus {
	if session.Status != "pending" {
		return DeviceStatus{Status: session.Status}
	}
	retry := int64(math.Ceil(float64(session.NextPollAt-m.now().UnixMilli()) / 1000))
	if retry < 1 {
		retry = 1
	}
	verification := session.VerificationURIComplete
	if verification == "" {
		verification = session.VerificationURI
	}
	return DeviceStatus{
		Status:            "pending",
		VerificationURL:   verification,
		UserCode:          session.UserCode,
		ExpiresAt:         time.UnixMilli(session.ExpiresAt).UTC().Format(time.RFC3339Nano),
		RetryAfterSeconds: retry,
	}
}

func trustedVerificationURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", errors.New("untrusted verification URI in xAI OAuth response")
	}
	return parsed.String(), nil
}

func deviceFailure(body []byte) (string, time.Duration) {
	var response struct {
		Error    string  `json:"error"`
		Interval float64 `json:"interval"`
	}
	if json.Unmarshal(body, &response) != nil {
		return "", 0
	}
	if response.Interval <= 0 {
		return response.Error, 0
	}
	return response.Error, time.Duration(response.Interval * float64(time.Second))
}
