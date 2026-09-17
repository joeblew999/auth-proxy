package main

import (
	"bytes"
	"cmp"
	"embed"
	"log"
	"net/http"
	"os"

	"github.com/gsxhq/vite"

	"github.com/joeblew999/auth-proxy/cmd/gui/views"
)

// A first visit has chosen nothing, so start on the first mode's first model.
var defaultModel = views.SampleModes[0].Models[0].ID

//go:embed all:dist
var distFS embed.FS

//go:embed all:public
var publicFS embed.FS

// newHandler builds the app once for whichever runtime serves it: the native
// server in main.go or the Cloudflare Worker in worker.go. Both serve the
// same pages and the same embedded assets, so the page cannot tell them apart.
func newHandler() (http.Handler, error) {
	devURL := os.Getenv("VITE_DEV_URL") // "" in prod, and always on Workers
	v, err := vite.New(vite.Config{DevURL: devURL, DevBase: "/__vite/", Dist: distFS, DistDir: "dist"})
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("/public/", http.FileServerFS(publicFS))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	if !v.Dev() {
		mux.Handle("/static/", v.StaticHandler())
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// The picker's state is in the URL: ?model= is the model in use and
		// ?mode= is the open panel. So clicking Use is an ordinary request that
		// works with JavaScript switched off, and ui/tabs/tabs.js only saves a
		// round trip when switching modes.
		query := r.URL.Query()
		model := cmp.Or(query.Get("model"), defaultModel)
		modes := views.WithSelection(views.SampleModes, model)
		page := Index("Pick a model", modes, views.OpenMode(modes, query.Get("mode"), model))

		// Rendered into a buffer first, unlike the gsx scaffold, which renders
		// straight to the ResponseWriter: once a byte is out the status is
		// fixed, so a failure half way through logged "superfluous
		// WriteHeader" and served half a page. A page this size costs nothing
		// to buffer, and a failure is a clean 500.
		var rendered bytes.Buffer
		if err := page.Render(r.Context(), &rendered); err != nil {
			log.Printf("render %s: %v", r.URL, err)
			http.Error(w, "the page could not be rendered", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(rendered.Bytes())
	})

	// v.Middleware injects *vite.Vite into each request's context so components
	// read the asset bundle from ctx (no prop threading).
	return v.Middleware(mux), nil
}
