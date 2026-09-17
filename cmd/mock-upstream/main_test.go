package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joeblew999/dev/cli"
)

// The mock exists to be pointed at, so the least it must do is answer the two
// paths the proxy calls and refuse everything else.
func TestServesV1AndRefusesTheRest(t *testing.T) {
	srv := httptest.NewServer(handler())
	defer srv.Close()
	for path, want := range map[string]int{
		"/v1/models": http.StatusOK,
		"/":          http.StatusNotFound,
		"/v2/models": http.StatusNotFound,
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s answered %d, want %d", path, resp.StatusCode, want)
		}
	}
}

// TestSkill holds every copy of the manual to the verbs; dev check runs it.
func TestSkill(t *testing.T) { cli.CheckSkill(t, app) }

// TestUsage holds usage.md to the markdown subset the terminal rendering
// reads, and catches a `<placeholder>` written without the backticks that
// stop a renderer eating it as an HTML tag.
func TestUsage(t *testing.T) { cli.CheckUsage(t, app) }
