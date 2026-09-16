package cli

import (
	"flag"
	"strings"
	"testing"
)

func TestDirAndTakesFlagsAnywhere(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	a := fs.Bool("a", false, "")
	b := fs.String("b", "", "")
	dir, rest, err := DirAnd(fs, []string{"cmd/x", "-b", "one", "first", "-a"}, 1)
	if err != nil || dir != "cmd/x" || !*a || *b != "one" || strings.Join(rest, ",") != "first" {
		t.Fatalf("got %q %v %v a=%v b=%q", dir, rest, err, *a, *b)
	}
	if _, _, err := DirAnd(fs, []string{"-a", "cmd/x"}, 0); err == nil {
		t.Fatal("a flag before the directory was accepted")
	}
}

func TestParseInterleavedPassesEverythingAfterDoubleDash(t *testing.T) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	env := fs.String("env", "", "")
	got, err := ParseInterleaved(fs, []string{"--env", "x", "--", "serve", "--config", "f.toml"})
	if err != nil || *env != "x" || strings.Join(got, " ") != "serve --config f.toml" {
		t.Fatalf("got %q %v env=%q", got, err, *env)
	}
}
