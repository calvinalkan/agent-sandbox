package main

import (
	"context"
	"io"

	flag "github.com/spf13/pflag"
)

// CheckCmd creates the check command for sandbox detection.
func CheckCmd() *Command {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.BoolP("help", "h", false, "Show help")
	flags.BoolP("quiet", "q", false, "Quiet mode, no output")

	return &Command{
		Flags:   flags,
		Usage:   "check [flags]",
		Short:   "Check if running inside sandbox",
		Long:    "Detect if the current process is running inside a bubblewrap sandbox.\nExits 0 if sandboxed, 1 otherwise.",
		Aliases: []string{},
		Exec: func(_ context.Context, _ io.Reader, stdout, _ io.Writer, _ []string) error {
			quiet, _ := flags.GetBool("quiet")

			if !quiet {
				fprintln(stdout, "not implemented")
			}

			return ErrSilentExit
		},
	}
}
