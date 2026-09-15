package main

import (
	"context"
	"os"

	"github.com/joeblew999/grok-oauth-proxy/spikes/hello-world/views"
)

func main() {
	if err := views.Picker(views.DemoModes, "browser", views.DemoModels).Render(context.Background(), os.Stdout); err != nil {
		panic(err)
	}
}
