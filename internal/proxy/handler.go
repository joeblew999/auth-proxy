package proxy

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/joeblew999/grok-oauth-proxy/internal/config"
	"github.com/joeblew999/grok-oauth-proxy/internal/router"
)

// DefaultRequestLimit caps buffered request bodies.
const DefaultRequestLimit = 32 << 20

// Options configures the handler.
type Options struct {
	Upstream *Upstream
	// MCP serves /mcp; nil answers 501.
	MCP http.Handler
	// RequestLimit caps request bodies; zero means DefaultRequestLimit.
	RequestLimit int64
	// Extra routes served without the client key, such as the local browser
	// login. Keys are ServeMux patterns.
	Public map[string]http.HandlerFunc
}

// NewHandler returns the proxy's full HTTP surface:
//
//	GET  /health               public liveness check
//	GET  /admin/status         provider readiness, with fixes
//	POST /admin/auth/start     start the Grok device login
//	GET  /admin/auth/status    device login state
//	POST /admin/auth/status    poll the device login
//	POST /admin/tokens         store Grok tokens manually
//	     /mcp                  MCP tools
//	GET  /v1/models            every provider's models
//	     /v1/...               proxied to the provider chosen by the model name
//
// Everything except /health and Options.Public requires the client key.
func NewHandler(opts Options) http.Handler {
	h := &handler{Options: opts}
	if h.RequestLimit == 0 {
		h.RequestLimit = DefaultRequestLimit
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	for pattern, fn := range opts.Public {
		mux.HandleFunc(pattern, fn)
	}
	mux.HandleFunc("/admin/status", h.requireKey(h.status))
	mux.HandleFunc("/admin/auth/start", h.requireKey(h.deviceStart))
	mux.HandleFunc("/admin/auth/status", h.requireKey(h.deviceStatus))
	mux.HandleFunc("/admin/tokens", h.requireKey(h.tokens))
	mcp := h.requireKey(func(w http.ResponseWriter, r *http.Request) {
		if h.MCP == nil {
			writeError(w, &Error{Status: http.StatusNotImplemented, Message: "MCP is not available in this build"})
			return
		}
		h.MCP.ServeHTTP(w, r)
	})
	mux.HandleFunc("/mcp", mcp)
	mux.HandleFunc("/mcp/", mcp)
	mux.HandleFunc("/", h.requireKey(h.proxy))
	return mux
}

type handler struct {
	Options
}

func (h *handler) config() *config.Config { return h.Upstream.Config }

// requireKey accepts the client key as a bearer token, an X-API-Key header, or a
// key query parameter.
func (h *handler) requireKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		expected := h.config().AdminKey
		if expected == "" {
			writeError(w, &Error{Status: http.StatusInternalServerError,
				Message: config.AdminKeyName + " is not set, so no client can be authorised", Fix: "mise run keys:set admin"})
			return
		}
		provided := r.URL.Query().Get("key")
		if key := r.Header.Get("X-API-Key"); key != "" {
			provided = key
		}
		if auth := r.Header.Get("Authorization"); auth != "" {
			scheme, token, ok := strings.Cut(auth, " ")
			if !ok || !strings.EqualFold(scheme, "Bearer") {
				writeError(w, &Error{Status: http.StatusUnauthorized, Message: "Authorization header must be \"Bearer <key>\""})
				return
			}
			provided = strings.TrimSpace(token)
		}
		providedHash, expectedHash := sha256.Sum256([]byte(provided)), sha256.Sum256([]byte(expected))
		if provided == "" || subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) != 1 {
			log.Printf("rejected %s %s: missing or wrong client key", r.Method, r.URL.Path)
			writeError(w, &Error{Status: http.StatusUnauthorized, Message: "missing or wrong client key"})
			return
		}
		next(w, r)
	}
}

func (h *handler) proxy(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && isModelsPath(r.URL.Path) {
		h.models(w, r)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, h.RequestLimit+1))
	if err != nil {
		writeError(w, &Error{Status: http.StatusBadRequest, Message: "could not read request body"})
		return
	}
	if int64(len(body)) > h.RequestLimit {
		writeError(w, &Error{Status: http.StatusRequestEntityTooLarge, Message: "request body too large"})
		return
	}

	requested := router.ModelFromBody(body)
	route := router.Resolve(h.config(), requested)
	if route.Model != requested {
		if body, err = router.RewriteModel(body, route.Model); err != nil {
			writeError(w, &Error{Status: http.StatusBadRequest, Message: err.Error()})
			return
		}
	}
	target, err := router.UpstreamURL(route.Provider, r.URL)
	if err != nil {
		writeError(w, &Error{Status: http.StatusBadRequest, Provider: route.Provider.Name, Message: err.Error()})
		return
	}

	resp, err := h.Upstream.Send(r.Context(), route.Provider, r.Method, target, body, r.Header)
	if err != nil {
		writeError(w, err)
		return
	}
	defer resp.Body.Close()
	if route.UnknownPrefix != "" && resp.StatusCode >= 400 && resp.StatusCode < 500 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		writeError(w, &Error{
			Status:   resp.StatusCode,
			Provider: route.Provider.Name,
			Message: fmt.Sprintf("model %q went to the default provider because %q is not a configured provider (providers: %s); it answered %d: %s",
				requested, route.UnknownPrefix, strings.Join(h.config().Names(), ", "), resp.StatusCode, snippet(detail)),
			Fix: fmt.Sprintf("use an ID from mise run models, or add [providers.%s] to providers.toml", route.UnknownPrefix),
		})
		return
	}
	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if err := copyFlushing(w, resp.Body); err != nil {
		log.Printf("provider %s: copy response: %v", route.Provider.Name, err)
	}
}

func (h *handler) models(w http.ResponseWriter, r *http.Request) {
	body, errs := h.Upstream.ListModels(r.Context())
	for _, err := range errs {
		log.Printf("list models: %v", err)
	}
	if len(errs) > 0 && len(errs) >= len(h.config().Providers) {
		writeError(w, errs[0])
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func (h *handler) status(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"source":    h.config().Source,
		"default":   h.config().Default,
		"providers": h.Upstream.Status(),
	})
}

// requireOAuth answers 409 unless a provider uses the Grok login.
func (h *handler) requireOAuth(w http.ResponseWriter) bool {
	if h.config().OAuthProvider() != nil && h.Upstream.XAI != nil {
		return true
	}
	writeError(w, &Error{Status: http.StatusConflict,
		Message: "no provider uses auth = \"xai-oauth\", so there is no Grok login to manage",
		Fix:     "add auth = \"xai-oauth\" to the xAI provider in providers.toml"})
	return false
}

func (h *handler) deviceStart(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodPost) || !h.requireOAuth(w) {
		return
	}
	status, err := h.Upstream.XAI.StartDevice(r.Context())
	h.writeDevice(w, status, err)
}

func (h *handler) deviceStatus(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodGet, http.MethodPost) || !h.requireOAuth(w) {
		return
	}
	if r.Method == http.MethodGet {
		status, err := h.Upstream.XAI.DeviceStatus()
		h.writeDevice(w, status, err)
		return
	}
	status, err := h.Upstream.XAI.PollDevice(r.Context())
	h.writeDevice(w, status, err)
}

func (h *handler) writeDevice(w http.ResponseWriter, status any, err error) {
	if err != nil {
		log.Printf("Grok device login: %v", err)
		writeError(w, &Error{Status: http.StatusBadGateway, Message: "Grok device login failed: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *handler) tokens(w http.ResponseWriter, r *http.Request) {
	if !allowMethods(w, r, http.MethodPost) || !h.requireOAuth(w) {
		return
	}
	var input struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    int64  `json:"expiresAt"` // Unix milliseconds
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 65536))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.ExpiresAt < 1_000_000_000_000 {
		writeError(w, &Error{Status: http.StatusBadRequest, Message: "expected JSON with accessToken, expiresAt (Unix milliseconds) and optional refreshToken"})
		return
	}
	if err := h.Upstream.XAI.StoreTokens(input.AccessToken, input.RefreshToken, time.UnixMilli(input.ExpiresAt)); err != nil {
		writeError(w, &Error{Status: http.StatusBadRequest, Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"stored": true})
}

func isModelsPath(path string) bool {
	path = strings.TrimSuffix(path, "/")
	return path == "/models" || path == "/v1/models"
}

// allowMethods guards the admin routes: it checks the method and, because a
// browser can attach the key query parameter, rejects cross-origin requests.
func allowMethods(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	allowed := false
	for _, m := range methods {
		allowed = allowed || r.Method == m
	}
	if !allowed {
		w.Header().Set("Allow", strings.Join(methods, ", "))
		writeError(w, &Error{Status: http.StatusMethodNotAllowed, Message: "method not allowed"})
		return false
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		scheme := "http"
		if r.TLS != nil || r.URL.Scheme == "https" {
			scheme = "https"
		}
		if origin != scheme+"://"+r.Host {
			writeError(w, &Error{Status: http.StatusForbidden, Message: "cross-origin admin requests are not allowed"})
			return false
		}
	}
	return true
}

// writeError answers in the OpenAI error shape so clients show the message, with
// the provider and fix added for people reading it.
func writeError(w http.ResponseWriter, err error) {
	var perr *Error
	if !errors.As(err, &perr) {
		perr = &Error{Status: http.StatusBadGateway, Message: err.Error()}
	}
	writeJSON(w, perr.Status, map[string]any{"error": map[string]string{
		"message":  perr.Error(),
		"type":     "proxy_error",
		"provider": perr.Provider,
		"fix":      perr.Fix,
	}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// copyFlushing streams a response, flushing after each read so server-sent
// events reach the client as they arrive.
func copyFlushing(w http.ResponseWriter, body io.Reader) error {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
