package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"
)

// ExecuteSandbox runs bwrap with the generated arguments.
// All environment variables from the parent process are passed through unchanged.
// Returns the exit code from the sandboxed process.
//
// When the context is cancelled, SIGTERM is sent to the sandboxed process to allow
// graceful shutdown. The process may exit with any code; context cancellation is
// signaled separately via the returned error.
func ExecuteSandbox(
	ctx context.Context,
	bwrapArgs []string,
	command []string,
	env map[string]string,
	stdin io.Reader,
	stdout, stderr io.Writer,
) (int, error) {
	// Build full bwrap command: bwrap <args> -- <command>
	args := make([]string, 0, len(bwrapArgs)+1+len(command))
	args = append(args, bwrapArgs...)
	args = append(args, "--")
	args = append(args, command...)

	cmd := exec.Command("bwrap", args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	// Pass all environment variables through
	cmd.Env = make([]string, 0, len(env))
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// Start the process
	err := cmd.Start()
	if err != nil {
		return 1, fmt.Errorf("starting bwrap: %w", err)
	}

	// Watch for context cancellation in a goroutine
	// When cancelled, send SIGTERM to allow graceful shutdown
	done := make(chan struct{})

	go func() {
		select {
		case <-ctx.Done():
			// Context cancelled - send SIGTERM for graceful shutdown
			if cmd.Process != nil {
				_ = cmd.Process.Signal(syscall.SIGTERM)
			}
		case <-done:
			// Process exited normally, nothing to do
		}
	}()

	// Wait for process to complete
	err = cmd.Wait()

	close(done)

	// Extract exit code
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		// Some other error (e.g., process couldn't be started properly)
		return 1, fmt.Errorf("waiting for bwrap: %w", err)
	}

	return 0, nil
}
