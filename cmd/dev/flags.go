package main

import (
	"flag"
	"os"
)

type flagSet struct{ *flag.FlagSet }

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}
