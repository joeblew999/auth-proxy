//go:build js && wasm

package main

import (
	"log"
	"net/http"

	workers "github.com/syumai/workers-go"
)

// The same app as a Cloudflare Worker: wrangler.toml beside this file, built
// by mise run gui:build:worker, deployed by mise run gui:deploy. The
// assets are inside the wasm, so the Worker needs no bindings at all.
func main() {
	handler, err := newHandler()
	if err != nil {
		log.Printf("app: %v", err)
		workers.Serve(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "the app could not start; see the Worker logs", http.StatusInternalServerError)
		}))
		return
	}
	workers.Serve(handler)
}
