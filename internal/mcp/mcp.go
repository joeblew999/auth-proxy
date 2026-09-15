//go:build !tinygo

// Package mcp serves the proxy's MCP tools: ask a model a question, and list the
// models every configured provider offers.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/joeblew999/grok-oauth-proxy/internal/proxy"
	"github.com/joeblew999/grok-oauth-proxy/internal/router"
)

const (
	serverName    = "model-proxy"
	serverVersion = "0.2.0"
)

// AskInput is the input of the ask tool.
type AskInput struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// AskOutput is the structured result of the ask tool.
type AskOutput struct {
	RequestedModel string `json:"requested_model"`
	Model          string `json:"model"`
	Text           string `json:"text"`
}

// ListModelsInput is the (empty) input of the list_models tool.
type ListModelsInput struct{}

// ListModelsOutput is the structured result of the list_models tool.
type ListModelsOutput struct {
	Models []string `json:"models"`
}

// NewHandler returns the stateless streamable HTTP MCP handler. Access control is
// the caller's job; the proxy wraps it with the client key check.
func NewHandler(up *proxy.Upstream) http.Handler {
	server := NewServer(up)
	return mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return server },
		&mcpsdk.StreamableHTTPOptions{
			Stateless:    true,
			JSONResponse: true,
			// The SDK rejects non-loopback Host headers when the listener is
			// loopback. A tunnel or Worker forwards public hostnames, and access
			// is already gated by the client key.
			DisableLocalhostProtection: true,
		},
	)
}

// NewServer builds the MCP server and its tools.
func NewServer(up *proxy.Upstream) *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: serverName, Version: serverVersion}, nil)
	providers := strings.Join(up.Config.Names(), ", ")
	t := tools{up: up}

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "ask",
		Description: "Ask a model one self-contained question and get its answer as text. There is no " +
			"conversation history, so put everything the model needs into the prompt. Configured providers: " +
			providers + ". Call list_models for valid model IDs.",
		InputSchema: objectSchema(map[string]any{
			"model": stringSchema("Model ID from list_models, e.g. \"groq/llama-3.3-70b-versatile\". " +
				"The part before the first slash picks the provider; without one the default provider (" + up.Config.Default + ") is used."),
			"prompt": stringSchema("The full question or instruction."),
		}, "model", "prompt"),
	}, adapt(t.ask))

	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name:        "list_models",
		Description: "List the model IDs that ask accepts, across all configured providers (" + providers + ").",
		InputSchema: objectSchema(map[string]any{}),
	}, adapt(t.listModels))

	return server
}

type tools struct {
	up *proxy.Upstream
}

func (t tools) ask(ctx context.Context, in AskInput) (AskOutput, error) {
	prompt, requested := strings.TrimSpace(in.Prompt), strings.TrimSpace(in.Model)
	if prompt == "" {
		return AskOutput{}, fmt.Errorf("prompt is required")
	}
	if requested == "" {
		return AskOutput{}, fmt.Errorf("model is required; call list_models for valid IDs")
	}

	route := router.Resolve(t.up.Config, requested)
	body, err := json.Marshal(map[string]any{
		"model":    route.Model,
		"messages": []any{map[string]any{"role": "user", "content": prompt}},
		"stream":   false,
	})
	if err != nil {
		return AskOutput{}, err
	}
	target, err := url.Parse(route.Provider.BaseURL + "/chat/completions")
	if err != nil {
		return AskOutput{}, err
	}

	start := time.Now()
	header := http.Header{"Content-Type": {"application/json"}}
	resp, err := t.up.Send(ctx, route.Provider, http.MethodPost, target, body, header)
	if err != nil {
		return AskOutput{}, fmt.Errorf("ask %s: %w", requested, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return AskOutput{}, fmt.Errorf("ask %s: read response: %w", requested, err)
	}
	if resp.StatusCode != http.StatusOK {
		return AskOutput{}, fmt.Errorf("ask %s: provider %s returned %d: %s", requested, route.Provider.Name, resp.StatusCode, truncate(respBody, 2048))
	}

	var completion struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return AskOutput{}, fmt.Errorf("ask %s: parse response: %w", requested, err)
	}
	var text strings.Builder
	for _, choice := range completion.Choices {
		text.WriteString(choice.Message.Content)
	}
	if text.Len() == 0 {
		return AskOutput{}, fmt.Errorf("model %s returned no text", requested)
	}
	// Report the served model in the same form list_models uses: prefixed only
	// when more than one provider is configured.
	served := requested
	if completion.Model != "" {
		served = completion.Model
		if len(t.up.Config.Providers) > 1 {
			served = route.Provider.Name + "/" + completion.Model
		}
	}
	log.Printf("MCP ask: model=%s provider=%s duration=%s", requested, route.Provider.Name, time.Since(start))
	return AskOutput{RequestedModel: requested, Model: served, Text: text.String()}, nil
}

func (t tools) listModels(ctx context.Context, _ ListModelsInput) (ListModelsOutput, error) {
	body, errs := t.up.ListModels(ctx)
	for _, err := range errs {
		log.Printf("MCP list_models: %v", err)
	}
	if len(errs) > 0 && len(errs) >= len(t.up.Config.Providers) {
		return ListModelsOutput{}, errs[0]
	}
	var list struct {
		Data []router.Model `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return ListModelsOutput{}, err
	}
	ids := make([]string, 0, len(list.Data))
	for _, m := range list.Data {
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)
	return ListModelsOutput{Models: ids}, nil
}

func adapt[In, Out any](fn func(context.Context, In) (Out, error)) mcpsdk.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, _ *mcpsdk.CallToolRequest, in In) (*mcpsdk.CallToolResult, Out, error) {
		out, err := fn(ctx, in)
		return nil, out, err
	}
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func truncate(body []byte, n int) string {
	text := strings.TrimSpace(string(body))
	if len(text) > n {
		return text[:n]
	}
	return text
}
