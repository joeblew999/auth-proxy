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
