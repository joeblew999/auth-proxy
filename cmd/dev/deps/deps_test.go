package deps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModulesFindsEveryGoModButSkipsWhatIsNotOurs(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"go.mod", "cmd/gui/go.mod", "node_modules/x/go.mod", "build/y/go.mod"} {
		os.MkdirAll(filepath.Join(root, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(root, p), []byte("module x\n"), 0o644)
	}
	t.Chdir(root)
	got, err := Modules(".")
	if err != nil {
		t.Fatal(err)
	}
	if want := ". cmd/gui"; strings.Join(got, " ") != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
