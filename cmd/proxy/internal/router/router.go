// Package router decides which provider serves a request and what URL and model
// name that provider receives. It performs no I/O.
package router

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/joeblew999/auth-proxy/cmd/proxy/internal/config"
)

// Route is a resolved request target.
type Route struct {
	Provider *config.Provider
	Model    string // model name as the provider knows it; empty when the request had none
	// UnknownPrefix is set when the model looked prefixed ("groq/x") but no such
	// provider exists, so it went to the default provider unchanged. If that
	// provider rejects the model, the error can say why.
	UnknownPrefix string
}

// Resolve maps a client model name to a provider.
//
//   - An alias is replaced by its "provider/model" target.
//   - "provider/model" goes to that provider with the prefix removed. Only the
//     first segment is a prefix, so "cloudflare/openai/gpt-5.5" sends
//     "openai/gpt-5.5" to the cloudflare provider.
//   - Anything else, including model IDs that contain a slash but start with no
//     provider name, goes to the default provider unchanged.
func Resolve(cfg *config.Config, model string) Route {
	if target, ok := cfg.Aliases[model]; ok {
		model = target
	}
	route := Route{Provider: cfg.Provider(cfg.Default), Model: model}
	if prefix, rest, ok := strings.Cut(model, "/"); ok && rest != "" {
		if p := cfg.Provider(prefix); p != nil {
			return Route{Provider: p, Model: rest}
		}
		route.UnknownPrefix = prefix
	}
	return route
}

// UpstreamURL maps an incoming proxy URL onto a provider. A leading /v1 is
// dropped because the base URL already carries the provider's version path, so
// /v1/chat/completions reaches <base>/chat/completions whether the base ends in
// /v1, /api/v1 or /openai/v1. The key query parameter is removed because it can
// carry the proxy's client key.
func UpstreamURL(p *config.Provider, incoming *url.URL) (*url.URL, error) {
	path := incoming.Path
	if path == "/v1" || strings.HasPrefix(path, "/v1/") {
		path = strings.TrimPrefix(path, "/v1")
	}
	target, err := url.Parse(p.BaseURL + "/" + strings.TrimPrefix(path, "/"))
	if err != nil {
		return nil, fmt.Errorf("provider %s: build upstream URL: %w", p.Name, err)
	}
	query := incoming.Query()
	query.Del("key")
	target.RawQuery = query.Encode()
	return target, nil
}

// ModelFromBody returns the "model" field of a JSON request body, if any.
func ModelFromBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var probe struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &probe) != nil {
		return ""
	}
	return probe.Model
}

// RewriteModel replaces the "model" field of a JSON request body, leaving every
// other field as the client sent it.
func RewriteModel(body []byte, model string) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, fmt.Errorf("request body is not a JSON object: %w", err)
	}
	encoded, err := json.Marshal(model)
	if err != nil {
		return nil, err
	}
	fields["model"] = encoded
	return json.Marshal(fields)
}
