package main

import (
	"bytes"
	"cmp"
	"context"
	"embed"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gsxhq/vite"

	"github.com/joeblew999/grok-oauth-proxy/spikes/hello-world/views"
)

// A first visit has chosen nothing, so start on the first mode's first model.
var defaultModel = views.SampleModes[0].Models[0].ID

//go:embed all:dist
var distFS embed.FS

//go:embed all:public
var publicFS embed.FS

func main() {
	devURL := os.Getenv("VITE_DEV_URL") // "" in prod
	v, err := vite.New(vite.Config{DevURL: devURL, DevBase: "/__vite/", Dist: distFS, DistDir: "dist"})
	if err != nil {
		log.Fatal(err)
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
	port := cmp.Or(os.Getenv("GO_PORT"), "7777")
	srv := &http.Server{Addr: ":" + port, Handler: v.Middleware(mux)}

	// Serve in the background so the main goroutine can wait for a shutdown
	// signal. gsx dev sends SIGTERM on each rebuild; shutting down gracefully
	// releases the port BEFORE exit, so the next build re-binds cleanly.
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	log.Printf("listening on http://localhost:%s", port)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
