package views

import (
	"net/url"
	"strconv"
)

// The picker's whole state is two strings in the URL: which mode is open and
// which model is in use. Nothing is kept in server memory, so a link, a reload
// and a screenshot all show the same page, and choosing a model works with
// JavaScript switched off.

// Percent labels a download in progress, e.g. "42%".
func (m Model) Percent() string { return strconv.Itoa(int(m.Progress)) + "%" }

// UseURL is the link that puts a model in use.
func UseURL(mode, model string) string {
	return "/?" + url.Values{"mode": {mode}, "model": {model}}.Encode()
}

// InUse is the model requests go to today, or "" when nothing is chosen.
func InUse(modes []Mode) string {
	for _, mode := range modes {
		for _, model := range mode.Models {
			if model.InUse {
				return model.ID
			}
		}
	}
	return ""
}

// ModeURL is the link that opens a mode, keeping the model in use. gsxui's tab
// triggers are buttons, so without JavaScript they do nothing; this is what the
// noscript fallback offers instead, and it is the same request the tab strip
// would have saved.
func ModeURL(modes []Mode, mode string) string {
	query := url.Values{"mode": {mode}}
	if model := InUse(modes); model != "" {
		query.Set("model", model)
	}
	return "/?" + query.Encode()
}

// ModeOf is the routing rule, applied: find the mode that serves this model.
// The model's prefix names its provider and the provider says where it runs,
// so a model ID alone decides which panel should be open. Returns "" when no
// mode claims the model.
func ModeOf(modes []Mode, model string) string {
	for _, mode := range modes {
		for _, candidate := range mode.Models {
			if candidate.ID == model {
				return mode.ID
			}
		}
	}
	return ""
}

// OpenMode picks the panel to open at first paint: the requested mode when it
// exists, else the model's own mode, else the first one. It never returns an
// unknown ID, because a picker whose value matches no panel renders with every
// panel hidden.
func OpenMode(modes []Mode, mode, model string) string {
	for _, candidate := range modes {
		if candidate.ID == mode {
			return mode
		}
	}
	if found := ModeOf(modes, model); found != "" {
		return found
	}
	if len(modes) > 0 {
		return modes[0].ID
	}
	return ""
}

// WithSelection copies modes with model marked as the one in use. It copies
// rather than mutating, because the modes it is given are shared by every
// request.
func WithSelection(modes []Mode, model string) []Mode {
	out := make([]Mode, len(modes))
	for i, mode := range modes {
		mode.Models = append([]Model(nil), mode.Models...)
		for j := range mode.Models {
			mode.Models[j].InUse = mode.Models[j].ID == model
		}
		out[i] = mode
	}
	return out
}
