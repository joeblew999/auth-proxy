package main

import "flag"

// flagSetT is a flag set that also keeps the positionals a stage found after
// its flags, wherever they appeared; Args returns them.
type flagSetT struct {
	*flag.FlagSet
	rest []string
}

// Args are the positionals after the directory, for `dev run DIR -- ARGS`.
func (f *flagSetT) Args() []string { return f.rest }
