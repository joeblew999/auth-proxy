package stage

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMeasureAndLine(t *testing.T) {
	p := filepath.Join(t.TempDir(), "app.wasm")
	os.WriteFile(p, bytes.Repeat([]byte("wasm "), 4096), 0o644)
	raw, gz, err := measure(p)
	if err != nil {
		t.Fatal(err)
	}
	if raw != 20480 || gz >= raw || gz == 0 {
		t.Fatalf("raw %d gzip %d", raw, gz)
	}
	if line := sizeLine("build/go/app.wasm", raw, gz); !strings.HasPrefix(line, "build/go/app.wasm        raw     20 KB   gzip ") {
		t.Fatalf("line %q", line)
	}
}
