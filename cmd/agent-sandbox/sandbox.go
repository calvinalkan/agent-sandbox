package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"
)

// forceKillChKey is the context key for the force-kill channel.
type forceKillChKey struct{}

// WithForceKillCh returns a context with a force-kill channel.
// When the channel is closed, ExecuteSandbox will send SIGKILL to the sandboxed process.
func WithForceKillCh(ctx context.Context, ch <-chan struct{}) context.Context {
	return context.WithValue(ctx, forceKillChKey{}, ch)
}

// getForceKillCh retrieves the force-kill channel from context.
// Returns nil if not set.
func getForceKillCh(ctx context.Context) <-chan struct{} {
	ch, _ := ctx.Value(forceKillChKey{}).(<-chan struct{})

	return ch
}

// ExecuteSandbox runs bwrap with the generated arguments.
// All environment variables from the parent process are passed through unchanged.
// Returns the exit code from the sandboxed process.
//
// Signal handling:
//   - When ctx is cancelled, SIGTERM is sent for graceful shutdown
//   - When the force-kill channel (from WithForceKillCh) is closed, SIGKILL is sent
//
// The process may exit with any code; context cancellation is signaled separately
// via the returned error.
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
		return 1, fmt.Errorf("starting bwrap: %w (check if kernel supports user namespaces: sysctl kernel.unprivileged_userns_clone)", err)
	}

	// Get force-kill channel from context (may be nil)
	forceKillCh := getForceKillCh(ctx)

	// Wait for process completion in a goroutine
	waitDone := make(chan error, 1)

	go func() {
		waitDone <- cmd.Wait()
	}()

	// Wait for: process completion, context cancellation (SIGTERM), or force-kill (SIGKILL)
	select {
	case waitErr := <-waitDone:
		// Process completed normally - extract exit code
		return extractExitCode(waitErr)

	case <-ctx.Done():
		// Context cancelled - send SIGTERM for graceful shutdown
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
		// Now wait for either process completion or force-kill
		select {
		case waitErr := <-waitDone:
			return extractExitCode(waitErr)
		case <-forceKillCh:
			// Force kill requested - send SIGKILL
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}

			<-waitDone // Wait for process to actually terminate

			return 0, nil
		}

	case <-forceKillCh:
		// Force kill without SIGTERM first
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}

		<-waitDone

		return 0, nil
	}
}

// extractExitCode returns the exit code from a Wait() error.
func extractExitCode(waitErr error) (int, error) {
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return exitErr.ExitCode(), nil
		}

		return 1, fmt.Errorf("waiting for bwrap: %w", waitErr)
	}

	return 0, nil
}
