// Package cli holds what every dev command shares: the one shape of a
// command, how its flags and the directory it acts on are read, and the one
// usage error.
package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// Runner is every dev command: the verb it was called as, the arguments after
// it, and where to write. A package that answers to several verbs (build,
// wasm, check, run, workerd are all stage's) switches on verb.
type Runner func(verb string, args []string, stdout, stderr io.Writer) error

// UsageError means the arguments were wrong; main prints the package's usage
// after it.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// Usagef makes a UsageError.
func Usagef(format string, a ...any) error {
	return &UsageError{fmt.Sprintf(format, a...)}
}

// Flags is a flag set that reports to stderr and never exits.
func Flags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// Bool is a flag.Value for booleans that also takes "" as false, so a task
// may pass `--flag=$var` with the variable unset.
type Bool bool

func (b *Bool) String() string { return fmt.Sprint(bool(*b)) }

// IsBoolFlag lets a bare `--flag` mean true.
func (b *Bool) IsBoolFlag() bool { return true }

// Set accepts the usual spellings of true and false, and "" as false.
func (b *Bool) Set(s string) error {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "0", "false", "no", "off":
		*b = false
	case "1", "true", "yes", "on":
		*b = true
	default:
		return fmt.Errorf("want true or false, got %q", s)
	}
	return nil
}

// DirAnd parses "DIR [flags and positionals in any order]": the directory a
// command acts on comes first; positional is how many positionals follow, or
// -1 for any number. Flags may come anywhere, because mise appends what the
// developer typed after a task name to the command; everything after a bare
// "--" is positional, verbatim.
func DirAnd(fs *flag.FlagSet, args []string, positional int) (dir string, rest []string, err error) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		return "", nil, Usagef("%s: the directory comes first", fs.Name())
	}
	rest, err = ParseInterleaved(fs, args[1:])
	if err != nil {
		return "", nil, Usagef("%s: %v", fs.Name(), err)
	}
	if positional >= 0 && len(rest) != positional {
		return "", nil, Usagef("%s: wrong arguments", fs.Name())
	}
	return args[0], rest, nil
}

// ParseInterleaved parses flags wherever they appear and returns the
// positionals, with everything after a bare "--" appended verbatim.
func ParseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals, tail []string
	for i, a := range args {
		if a == "--" {
			args, tail = args[:i], args[i+1:]
			break
		}
	}
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) > 0 {
			positionals = append(positionals, args[0])
			args = args[1:]
		}
	}
	return append(positionals, tail...), nil
}
