package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
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

// WrapperFDs holds file descriptors for wrapper script injection.
// The Files slice contains read ends of pipes that should be passed to bwrap
// as ExtraFiles. Close must be called after the command completes to clean up.
type WrapperFDs struct {
	Files []*os.File // Read ends of pipes (add to cmd.ExtraFiles)
	Args  []string   // bwrap args (--perms 0555 --ro-bind-data FD DEST)
}

// Close closes all file descriptors.
func (w *WrapperFDs) Close() {
	for _, f := range w.Files {
		_ = f.Close()
	}
}

// fileID uniquely identifies a file by device and inode.
type fileID struct {
	dev uint64
	ino uint64
}

// getFileID returns a unique identifier for a file based on device and inode.
// Returns zero values if the file cannot be stat'd.
func getFileID(path string) fileID {
	info, err := os.Stat(path)
	if err != nil {
		return fileID{}
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileID{}
	}

	return fileID{dev: stat.Dev, ino: stat.Ino}
}

// PrepareWrapperFDs creates pipes and writes wrapper scripts for FD-based injection.
// Returns nil if setup is nil or has no wrappers.
//
// The returned WrapperFDs contains:
//   - Files: read ends of pipes to pass as ExtraFiles (become FDs 3, 4, 5... in child)
//   - Args: bwrap arguments using --perms 0555 --ro-bind-data
//
// Important: Each destination needs its own FD because --ro-bind-data consumes the
// FD content on first use. However, we deduplicate destinations that point to the
// same file (same inode) because mounting over one hard link affects all hard links.
//
// The caller must call Close() on the returned WrapperFDs after the command completes.
func PrepareWrapperFDs(setup *WrapperSetup) (*WrapperFDs, error) {
	if setup == nil || len(setup.Wrappers) == 0 {
		// Return empty WrapperFDs, not nil - callers handle empty Files gracefully
		return &WrapperFDs{}, nil
	}

	// Count total destinations to pre-allocate
	totalDests := 0
	for _, w := range setup.Wrappers {
		totalDests += len(w.Destinations)
	}

	result := &WrapperFDs{
		Files: make([]*os.File, 0, totalDests),
		Args:  make([]string, 0, totalDests*4),
	}

	// Track FD index - ExtraFiles[0] becomes FD 3, [1] becomes FD 4, etc.
	fdIndex := 0

	// Track which inodes we've already mounted to avoid duplicate mounts on hard links
	mountedInodes := make(map[fileID]bool)

	for _, wrapper := range setup.Wrappers {
		// Deduplicate destinations by inode - hard links to same file only need one mount
		for _, dest := range wrapper.Destinations {
			fid := getFileID(dest)
			if fid != (fileID{}) && mountedInodes[fid] {
				// Already mounted a wrapper for this inode, skip
				continue
			}

			if fid != (fileID{}) {
				mountedInodes[fid] = true
			}

			pipeReader, pipeWriter, err := os.Pipe()
			if err != nil {
				result.Close()

				return nil, fmt.Errorf("creating pipe for wrapper: %w", err)
			}

			// Write script content to write end
			_, err = pipeWriter.WriteString(wrapper.Script)
			if err != nil {
				_ = pipeReader.Close()
				_ = pipeWriter.Close()
				result.Close()

				return nil, fmt.Errorf("writing wrapper script: %w", err)
			}

			// Close write end - bwrap will read from the pipe
			err = pipeWriter.Close()
			if err != nil {
				_ = pipeReader.Close()
				result.Close()

				return nil, fmt.Errorf("closing pipe write end: %w", err)
			}

			// Track read end for ExtraFiles
			result.Files = append(result.Files, pipeReader)

			// Calculate FD number in child process (3 + index since 0=stdin, 1=stdout, 2=stderr)
			childFD := 3 + fdIndex

			// Add bwrap args for this destination
			// --perms 0555 makes the script executable
			// --ro-bind-data FD DEST mounts the FD content at DEST
			result.Args = append(result.Args,
				"--perms", "0555",
				"--ro-bind-data", strconv.Itoa(childFD), dest,
			)

			fdIndex++
		}
	}

	return result, nil
}

// ExecuteSandbox runs bwrap with the generated arguments.
// All environment variables from the parent process are passed through unchanged.
// Returns the exit code from the sandboxed process.
//
// If wrapperSetup is provided, wrapper scripts are injected via FD-based mounting.
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
	wrapperSetup *WrapperSetup,
	stdin io.Reader,
	stdout, stderr io.Writer,
) (int, error) {
	// Skip wrapper FD injection if we're already inside a sandbox (nested sandbox case).
	// The outer sandbox's wrappers are already in place, so we don't need to remount them.
	var wrapperFDs *WrapperFDs

	if !isInsideSandbox() {
		var err error

		wrapperFDs, err = PrepareWrapperFDs(wrapperSetup)
		if err != nil {
			return 1, fmt.Errorf("preparing wrapper FDs: %w", err)
		}

		if wrapperFDs != nil {
			defer wrapperFDs.Close()
			// Append wrapper mount args
			bwrapArgs = append(bwrapArgs, wrapperFDs.Args...)
		}
	}

	// Build full bwrap command: bwrap <args> -- <command>
	args := make([]string, 0, len(bwrapArgs)+1+len(command))
	args = append(args, bwrapArgs...)
	args = append(args, "--")
	args = append(args, command...)

	cmd := exec.Command("bwrap", args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	// Pass wrapper FDs to child process
	if wrapperFDs != nil {
		cmd.ExtraFiles = wrapperFDs.Files
	}

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
