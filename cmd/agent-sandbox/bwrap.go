package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DockerSocketPath is the standard Docker socket location.
const DockerSocketPath = "/var/run/docker.sock"

// SandboxBinaryPath is where agent-sandbox is mounted inside the sandbox.
// We use /run/agent-sandbox because /run is a tmpfs and we can create files there.
// /usr/bin would require the file to already exist (root is mounted read-only).
const SandboxBinaryPath = "/run/agent-sandbox"

// ErrDockerSocketNotFound is returned when docker is enabled but the socket cannot be found.
var ErrDockerSocketNotFound = errors.New("docker socket not found")

// ErrSelfBinaryNotFound is returned when the agent-sandbox binary cannot be located.
var ErrSelfBinaryNotFound = errors.New("cannot locate agent-sandbox binary")

// BwrapArgs generates bwrap arguments from resolved paths and configuration.
//
// The argument order is important - bwrap processes arguments in order, so:
//  1. Namespace and process setup (--die-with-parent, --unshare-all, --share-net)
//  2. Virtual mounts for /dev and /proc
//  3. Base root filesystem mount (--ro-bind / /)
//  4. Isolated runtime tmpfs for /run
//  5. Docker socket handling (mask or expose)
//  6. Self binary mount (agent-sandbox at /usr/bin/agent-sandbox)
//  7. Individual path mounts, sorted by depth (shallower first)
//  8. Working directory (--chdir)
//
// Returns an error if docker is enabled but the socket cannot be found or resolved,
// or if the agent-sandbox binary cannot be located.
// Exclude paths are currently ignored (handled by d5g3tgg).
func BwrapArgs(paths []ResolvedPath, cfg *Config) ([]string, error) {
	// Process cleanup and namespace setup first
	args := []string{
		"--die-with-parent", // Auto-cleanup when parent dies
		"--unshare-all",     // Create new namespaces
	}

	// Network sharing (default is on, so we share unless explicitly disabled)
	if cfg.Network != nil && *cfg.Network {
		args = append(args, "--share-net")
	}

	// Always include virtual mounts (per SPEC hardcoded behavior)
	// Root filesystem read-only (per SPEC security guarantees)
	// Isolated runtime tmpfs for /run
	args = append(args,
		"--dev", "/dev",
		"--proc", "/proc",
		"--ro-bind", "/", "/",
		"--tmpfs", "/run",
	)

	// Docker socket handling
	// Because we bind-mount / (read-only) as the base filesystem, the docker socket
	// would otherwise be visible inside the sandbox by default (if /var/run is a
	// real directory, not a symlink to /run). We must actively mask it unless
	// --docker is enabled.
	dockerArgs, err := dockerSocketArgs(cfg.Docker, DockerSocketPath)
	if err != nil {
		return nil, err
	}

	args = append(args, dockerArgs...)

	// Mount agent-sandbox binary into sandbox
	selfArgs, err := selfBinaryArgs()
	if err != nil {
		return nil, err
	}

	args = append(args, selfArgs...)

	// Process paths in order - ResolveAndSort ensures correct depth ordering
	// More specific paths come AFTER less specific, so they overlay correctly
	for _, resolvedPath := range paths {
		switch resolvedPath.Access {
		case PathAccessRo:
			// Use --ro-bind-try for paths that may not exist
			// (e.g., lint configs that only exist in some projects)
			args = append(args, "--ro-bind-try", resolvedPath.Resolved, resolvedPath.Resolved)
		case PathAccessRw:
			// Use --bind-try for optional writable paths
			args = append(args, "--bind-try", resolvedPath.Resolved, resolvedPath.Resolved)
		case PathAccessExclude:
			// Exclude mounts are implemented in d5g3tgg (directories vs files differ).
			// For now, skip exclude paths - they'll be handled separately.
			continue
		}
	}

	// Working directory
	args = append(args, "--chdir", cfg.EffectiveCwd)

	return args, nil
}

// dockerSocketArgs generates bwrap arguments for docker socket handling.
// When docker is enabled, resolves the socket symlink and binds the real socket.
// When docker is disabled, masks the socket with /dev/null to prevent access
// (only if the socket exists and is not under /run - we mount /run as tmpfs).
// The socketPath parameter allows testing with temp directories containing real symlinks.
func dockerSocketArgs(docker *bool, socketPath string) ([]string, error) {
	// Docker is disabled by default (docker == nil means use default which is false)
	dockerEnabled := docker != nil && *docker

	if dockerEnabled {
		// Resolve to the real socket path (may be a symlink).
		resolved, err := filepath.EvalSymlinks(socketPath)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrDockerSocketNotFound, err)
		}

		// Also resolve the parent directory of the socket path to handle symlinks.
		// On many systems, /var/run is a symlink to /run. Since we mount /run as tmpfs,
		// we need to bind the socket to the resolved destination path.
		destResolved, err := filepath.EvalSymlinks(filepath.Dir(socketPath))
		if err != nil {
			return nil, fmt.Errorf("%w: cannot resolve socket directory: %w", ErrDockerSocketNotFound, err)
		}

		destPath := filepath.Join(destResolved, filepath.Base(socketPath))

		// Bind the real socket to the resolved destination path.
		return []string{"--bind", resolved, destPath}, nil
	}

	// Docker disabled: check if we need to mask the socket.
	// We only need to mask if:
	// 1. The socket exists
	// 2. It resolves to a path outside /run (we mount /run as tmpfs)
	maskPath := dockerSocketMaskPath(socketPath)
	if maskPath == "" {
		return nil, nil
	}

	// Socket exists outside /run - mask it with /dev/null.
	// Use /dev/null as the source - this makes the path exist but not be a valid socket.
	// Docker clients will get "not a socket" error when trying to connect.
	return []string{"--ro-bind", "/dev/null", maskPath}, nil
}

// dockerSocketMaskPath returns the path to mask for the docker socket,
// or empty string if no masking is needed.
func dockerSocketMaskPath(socketPath string) string {
	// Check if the socket exists at all.
	_, err := os.Stat(socketPath)
	if err != nil {
		return ""
	}

	// Resolve the socket path to its real location.
	// On many systems, /var/run is a symlink to /run. Since we mount /run as tmpfs,
	// the socket won't be visible there anyway - no masking needed.
	resolved, err := filepath.EvalSymlinks(socketPath)
	if err != nil {
		return ""
	}

	// If the resolved path is under /run, we don't need to mask it because
	// we mount /run as a fresh tmpfs (which will be empty).
	if isPathUnder(resolved, "/run") {
		return ""
	}

	return socketPath
}

// selfBinaryArgs generates bwrap arguments to mount the agent-sandbox binary
// into the sandbox. This enables:
// - The wrap-binary command to work (command wrappers exec agent-sandbox)
// - Users running `agent-sandbox check` inside the sandbox
// - Nested sandbox calls
//
// The binary is mounted read-only at /run/agent-sandbox.
// Symlinks are resolved to get the real binary path.
func selfBinaryArgs() ([]string, error) {
	// Find our own executable
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSelfBinaryNotFound, err)
	}

	// Resolve any symlinks to get the real binary path
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot resolve symlinks: %w", ErrSelfBinaryNotFound, err)
	}

	// Mount at standard location inside the sandbox
	return []string{"--ro-bind", self, SandboxBinaryPath}, nil
}
