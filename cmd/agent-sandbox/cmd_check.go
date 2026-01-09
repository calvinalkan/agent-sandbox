package main

import (
	"context"
	"io"
	"os"

	flag "github.com/spf13/pflag"
)

// isInsideSandbox checks if the current process is running inside a sandbox
// by testing for the presence of the marker file.
// The marker file is mounted read-only at a known path inside the sandbox
// and cannot be created or removed from inside the sandbox.
func isInsideSandbox() bool {
	_, err := os.Stat(SandboxMarkerPath)

	return err == nil
}

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
			inside := isInsideSandbox()

			if !quiet {
				if inside {
					fprintln(stdout, "inside sandbox")
				} else {
					fprintln(stdout, "outside sandbox")
				}
			}

			if inside {
				return nil // exit 0
			}

			return ErrSilentExit // exit 1
		},
	}
}
