package router

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Model is one entry of an OpenAI-style /v1/models list.
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created,omitempty"`
	OwnedBy string `json:"owned_by,omitempty"`
}

// xaiExtraModels are xAI aliases that api.x.ai omits from /models but serves.
var xaiExtraModels = []Model{
	{ID: "grok-composer-2.5-fast", Object: "model", Created: 1776384000, OwnedBy: "xai"},
}

// ProviderModels is the result of listing one provider's models.
type ProviderModels struct {
	Provider string
	IsXAI    bool
	Body     []byte // the provider's raw /models response
}

// MergeModels combines per-provider /models responses into one list.
//
// With more than one provider every ID is prefixed with its provider name
// ("groq/llama-3.3-70b"), which is exactly what a client sends back to route to
// it. With one provider IDs are left as the provider reports them. xAI-only
// aliases are added to xAI providers. Unparseable responses are skipped and
// reported in the returned errors, so one broken provider does not hide the rest.
func MergeModels(results []ProviderModels, providerCount int) ([]byte, []error) {
	var (
		merged []Model
		errs   []error
	)
	for _, result := range results {
		var list struct {
			Data []Model `json:"data"`
		}
		if err := json.Unmarshal(result.Body, &list); err != nil {
			errs = append(errs, fmt.Errorf("provider %s: parse /models: %w", result.Provider, err))
			continue
		}
		models := list.Data
		if result.IsXAI {
			models = withExtras(models, xaiExtraModels)
		}
		for _, m := range models {
			if m.ID == "" {
				continue
			}
			if m.Object == "" {
				m.Object = "model"
			}
			if providerCount > 1 {
				m.ID = result.Provider + "/" + m.ID
			}
			merged = append(merged, m)
		}
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].ID < merged[j].ID })
	body, err := json.Marshal(struct {
		Object string  `json:"object"`
		Data   []Model `json:"data"`
	}{Object: "list", Data: nonNil(merged)})
	if err != nil {
		errs = append(errs, err)
	}
	return body, errs
}

func withExtras(models, extras []Model) []Model {
	seen := make(map[string]bool, len(models))
	for _, m := range models {
		seen[m.ID] = true
	}
	for _, extra := range extras {
		if !seen[extra.ID] {
			models = append(models, extra)
		}
	}
	return models
}

func nonNil(models []Model) []Model {
	if models == nil {
		return []Model{}
	}
	return models
}
