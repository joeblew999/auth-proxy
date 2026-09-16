// Package sizes prints a file's size raw and gzipped, the two numbers a wasm
// build is judged by. It runs as `dev sizes FILE...`.
package sizes

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
)

// Run prints one line per file.
func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: dev sizes FILE...")
		return errors.New("sizes needs at least one file")
	}
	for _, p := range args {
		raw, gz, err := Measure(p)
		if err != nil {
			return err
		}
		fmt.Fprint(stdout, Line(p, raw, gz))
	}
	return nil
}

// Line formats one file's sizes, in KB, aligned for a table.
func Line(name string, raw, gzipped int64) string {
	return fmt.Sprintf("%-24s raw %6d KB   gzip %6d KB\n", name, raw/1024, gzipped/1024)
}

type counter struct{ n int64 }

func (c *counter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// Measure returns a file's size raw and after gzip at best compression.
func Measure(path string) (raw, gzipped int64, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	var c counter
	w, err := gzip.NewWriterLevel(&c, gzip.BestCompression)
	if err != nil {
		return 0, 0, err
	}
	if _, err := w.Write(data); err != nil {
		return 0, 0, err
	}
	if err := w.Close(); err != nil {
		return 0, 0, err
	}
	return int64(len(data)), c.n, nil
}
