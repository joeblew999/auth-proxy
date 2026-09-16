package views

import (
	"context"
	"strings"
	"testing"
)

// With JavaScript off, gsxui's tab triggers are inert buttons, so every mode
// has to be reachable as a plain link. A browser cannot check this — it runs
// the JavaScript — so it is checked here, on the rendered markup.
func TestEveryModeIsReachableWithoutJavaScript(t *testing.T) {
	var page strings.Builder
	if err := Picker(WithSelection(SampleModes, "xai/grok-4.3"), "remote").Render(context.Background(), &page); err != nil {
		t.Fatal(err)
	}
	html := page.String()
	fallback := html[strings.Index(html, "<noscript>"):strings.Index(html, "</noscript>")]
	if !strings.Contains(html, "<noscript>") {
		t.Fatal("the picker renders no noscript fallback")
	}
	for _, mode := range SampleModes {
		link := ModeURL(SampleModes, mode.ID)
		// The renderer escapes & in attributes, as it must.
		if !strings.Contains(fallback, strings.ReplaceAll(link, "&", "&amp;")) {
			t.Errorf("no link to the %s mode in the noscript fallback: want %s", mode.ID, link)
		}
	}
}
