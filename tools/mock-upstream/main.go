// Command mock-upstream is a minimal OpenAI-compatible server for testing the
// proxy against something other than xAI, at zero cost.
//
// It is dependency-free and deterministic. Use it to verify request routing,
// SSE streaming, and which bearer token the proxy forwards upstream — the last
// of which is the thing worth checking when switching providers.
//
//	go run ./tools/mock-upstream -addr 127.0.0.1:18080
//
// Then point the proxy at it (see the proxy_local_mock mise task):
//
//	UPSTREAM_BASE_URL=http://127.0.0.1:18080/v1 UPSTREAM_API_KEY=mock-key
//
// It intentionally logs only a masked form of the bearer token plus a short
// hash, so it stays safe to run even if pointed at a real key.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18080", "address to listen on")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", handleModels)
	mux.HandleFunc("/v1/chat/completions", handleChat)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		logf("%s %s -> 404 (mock only serves /v1/*)", r.Method, r.URL.Path)
		http.NotFound(w, r)
	})

	// Log every request before dispatch so the upstream credential the proxy
	// chose is visible even for requests that fail.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logf("%s %s auth=%s", r.Method, r.URL.Path, describeAuth(r))
		mux.ServeHTTP(w, r)
	})

	logf("mock upstream listening on http://%s/v1", *addr)
	if err := http.ListenAndServe(*addr, handler); err != nil {
		log.Fatal(err)
	}
}

func logf(format string, args ...any) {
	log.Printf("[mock-upstream] "+format, args...)
}

// describeAuth returns a masked description of the bearer token, never the
// token itself. The hash is stable, so a test can confirm which credential
// arrived without the log disclosing it.
func describeAuth(r *http.Request) string {
	raw := r.Header.Get("Authorization")
	if raw == "" {
		return "MISSING"
	}
	token, found := strings.CutPrefix(raw, "Bearer ")
	if !found {
		return "MALFORMED-NO-BEARER-PREFIX"
	}
	if token == "" {
		return "EMPTY"
	}

	prefixLen := min(4, len(token))
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%s... (len=%d sha256=%x)", token[:prefixLen], len(token), sum[:4])
}

func handleModels(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data": []any{
			map[string]any{"id": "mock-model", "object": "model", "owned_by": "mock"},
			map[string]any{"id": "mock-reasoning", "object": "model", "owned_by": "mock"},
		},
	})
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]any{"message": "invalid JSON body", "type": "invalid_request_error"},
		})
		return
	}
	if req.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]any{"message": "model is required", "type": "invalid_request_error"},
		})
		return
	}

	if req.Stream {
		streamChat(w, req.Model)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":      "mock-completion-1",
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []any{
			map[string]any{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "hello from mock upstream"},
				"finish_reason": "stop",
			},
		},
	})
}

// streamChat emits a short SSE completion with deliberate delays, so that a
// streaming client can observe incremental delivery rather than one buffer.
func streamChat(w http.ResponseWriter, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	words := []string{"hello", " from", " the", " mock", " upstream"}
	for i, word := range words {
		chunk := map[string]any{
			"id":      "mock-completion-1",
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []any{
				map[string]any{
					"index": 0,
					"delta": map[string]any{"content": word},
				},
			},
		}
		encoded, err := json.Marshal(chunk)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", encoded)
		flusher.Flush()
		logf("  streamed chunk %d/%d %q", i+1, len(words), word)
		time.Sleep(150 * time.Millisecond)
	}

	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
	logf("  stream complete")
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		logf("write json response: %v", err)
	}
}
