// Command tinygo-render renders the picker and writes it to stdout. It exists
// to be built with TinyGo (mise run gui:check), because the browser topology
// runs this same markup inside a Service Worker: if the components stop
// compiling under TinyGo, that plan is dead and this is where it shows up.
//
// Components are called positionally from Go, the same signature the markup
// binds by name.
package main

import (
	"context"
	"os"

	"github.com/joeblew999/grok-oauth-proxy/cmd/gui/views"
)

func main() {
	if err := views.Picker(views.SampleModes, "browser").Render(context.Background(), os.Stdout); err != nil {
		panic(err)
	}
}
