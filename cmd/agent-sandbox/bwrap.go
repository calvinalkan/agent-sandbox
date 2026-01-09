package main

import (
	"errors"
	"fmt"
	"path/filepath"
)

// DockerSocketPath is the standard Docker socket location.
const DockerSocketPath = "/var/run/docker.sock"

// ErrDockerSocketNotFound is returned when docker is enabled but the socket cannot be found.
var ErrDockerSocketNotFound = errors.New("docker socket not found")

// BwrapArgs generates bwrap arguments from resolved paths and configuration.
//
// The argument order is important - bwrap processes arguments in order, so:
//  1. Namespace and process setup (--die-with-parent, --unshare-all, --share-net)
//  2. Virtual mounts for /dev and /proc
//  3. Base root filesystem mount (--ro-bind / /)
//  4. Isolated runtime tmpfs for /run
//  5. Docker socket handling (mask or expose)
//  6. Individual path mounts, sorted by depth (shallower first)
//  7. Working directory (--chdir)
//
// Returns an error if docker is enabled but the socket cannot be found or resolved.
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
// When docker is disabled, masks the socket with /dev/null to prevent access.
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

		// Bind the real socket to the standard path so clients can always use
		// /var/run/docker.sock regardless of where the actual socket lives.
		return []string{"--bind", resolved, socketPath}, nil
	}

	// Docker disabled: mask the socket so it's not usable.
	// Use /dev/null as the source - this makes the path exist but not be a valid socket.
	// Docker clients will get "not a socket" error when trying to connect.
	return []string{"--ro-bind", "/dev/null", socketPath}, nil
}
