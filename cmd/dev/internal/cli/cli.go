// Package cli holds what every dev subcommand shares.
package cli

import (
	"fmt"
	"strings"
)

// Bool is a flag.Value for booleans that also takes "" as false. A mise task
// passes `--flag=$usage_flag`, and usage_flag is empty when nobody gave the
// flag, which the standard bool flag rejects.
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
