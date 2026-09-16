package views

import "testing"

var modes = []Mode{
	{ID: "remote", Ready: true, Models: []Model{{ID: "xai/grok-4.3"}, {ID: "groq/llama-3.3-70b"}}},
	{ID: "local"},
	{ID: "browser", Ready: true, Models: []Model{{ID: "browser/Qwen3-0.6B"}}},
}

// The panel that opens is never one that does not exist: a picker whose value
// matches no panel renders with every panel hidden, which looks like a blank
// page rather than a bug.
func TestOpenMode(t *testing.T) {
	for name, tc := range map[string]struct {
		mode, model, want string
	}{
		"the requested mode":              {"browser", "", "browser"},
		"a mode with no models is fine":   {"local", "", "local"},
		"the model's own mode":            {"", "browser/Qwen3-0.6B", "browser"},
		"the request wins over the model": {"remote", "browser/Qwen3-0.6B", "remote"},
		"an unknown mode falls back":      {"nonsense", "", "remote"},
		"an unknown model falls back":     {"", "nope/nope", "remote"},
		"nothing at all falls back":       {"", "", "remote"},
	} {
		if got := OpenMode(modes, tc.mode, tc.model); got != tc.want {
			t.Errorf("%s: OpenMode(%q, %q) = %q, want %q", name, tc.mode, tc.model, got, tc.want)
		}
	}
	if got := OpenMode(nil, "remote", ""); got != "" {
		t.Errorf("OpenMode(nil) = %q, want empty", got)
	}
}

// ModeOf is the routing rule: a model ID alone says where it runs.
func TestModeOf(t *testing.T) {
	if got := ModeOf(modes, "groq/llama-3.3-70b"); got != "remote" {
		t.Errorf("ModeOf() = %q, want remote", got)
	}
	if got := ModeOf(modes, "nope/nope"); got != "" {
		t.Errorf("ModeOf(unknown) = %q, want empty", got)
	}
}

// WithSelection must not write into the shared sample data, or one request
// would change what every later request sees.
func TestWithSelectionCopies(t *testing.T) {
	got := WithSelection(modes, "browser/Qwen3-0.6B")
	if !got[2].Models[0].InUse {
		t.Error("the chosen model is not marked in use")
	}
	if got[0].Models[0].InUse {
		t.Error("a model in another mode was marked in use")
	}
	if modes[2].Models[0].InUse {
		t.Error("WithSelection wrote into the modes it was given")
	}
	if inUse := WithSelection(modes, "nope/nope"); inUse[0].Models[0].InUse {
		t.Error("an unknown model marked something in use")
	}
}

func TestUseURL(t *testing.T) {
	want := "/?mode=browser&model=browser%2FQwen3-0.6B"
	if got := UseURL("browser", "browser/Qwen3-0.6B"); got != want {
		t.Errorf("UseURL() = %q, want %q", got, want)
	}
}

func TestPercent(t *testing.T) {
	if got := (Model{Progress: 42.7}).Percent(); got != "42%" {
		t.Errorf("Percent() = %q, want 42%%", got)
	}
}

func TestInUseAndModeURL(t *testing.T) {
	chosen := WithSelection(modes, "browser/Qwen3-0.6B")
	if got := InUse(chosen); got != "browser/Qwen3-0.6B" {
		t.Errorf("InUse() = %q", got)
	}
	if got := InUse(modes); got != "" {
		t.Errorf("InUse(nothing chosen) = %q, want empty", got)
	}
	// Switching mode keeps the chosen model, so going to look at another mode
	// does not silently change which model answers.
	want := "/?mode=local&model=browser%2FQwen3-0.6B"
	if got := ModeURL(chosen, "local"); got != want {
		t.Errorf("ModeURL() = %q, want %q", got, want)
	}
	if got := ModeURL(modes, "local"); got != "/?mode=local" {
		t.Errorf("ModeURL(nothing chosen) = %q", got)
	}
}
