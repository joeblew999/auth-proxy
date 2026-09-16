// Package cli holds what every dev command shares: how flags and the
// directory a stage acts on are read.
package cli

import (
	"flag"
	"fmt"
	"strings"
)

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
// command acts on comes first, and positional must be how many positionals
// follow. Flags may come anywhere, because mise appends what the developer
// typed after a task name to the command.
func DirAnd(fs *flag.FlagSet, args []string, positional int) (dir string, rest []string, err error) {
	if len(args) == 0 || args[0] == "" || args[0][0] == '-' {
		return "", nil, fmt.Errorf("%s: the directory comes first", fs.Name())
	}
	rest, err = ParseInterleaved(fs, args[1:])
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", fs.Name(), err)
	}
	if len(rest) != positional {
		return "", nil, fmt.Errorf("%s: wrong arguments", fs.Name())
	}
	return args[0], rest, nil
}

// ParseInterleaved parses flags wherever they appear and returns the
// positionals. Everything after a bare "--" is positional, verbatim: that is
// how `dev run DIR -- serve --config FILE` hands flags to the program it runs.
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
