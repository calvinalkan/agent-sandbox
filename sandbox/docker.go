//go:build linux

package sandbox

// This file contains docker socket exposure/masking logic.
//
// The sandbox defaults to hiding the docker socket. When enabled, the socket is
// bind-mounted read-write so docker clients can talk to the host daemon.
//
// The implementation always emits an explicit docker mount when Docker is
// enabled or disabled (bind the socket in, or mask it with /dev/null).
//
// This is intentional: callers may remount /run or otherwise change mount
// visibility, so relying on "it happens to be under /run" would be brittle.
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// dockerSocketMountPlan returns a mountPlan that either exposes or masks the docker socket.
func dockerSocketMountPlan(dockerEnabled bool, hostEnv map[string]string, paths pathResolver, debugf Debugf) (mountPlan, error) {
	dockerHost := ""
	if hostEnv != nil {
		dockerHost = hostEnv["DOCKER_HOST"]
	}

	socketPath := dockerSocketPathFromEnv(hostEnv)
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}

	if debugf != nil {
		debugf("docker: enabled=%t DOCKER_HOST=%q socket=%q", dockerEnabled, dockerHost, socketPath)
	}

	socketPath = filepath.Clean(socketPath)
	if !filepath.IsAbs(socketPath) {
		if dockerEnabled {
			return mountPlan{}, fmt.Errorf("docker socket not found: %q is not absolute", socketPath)
		}

		// For disabled docker, keep behavior conservative: still mask the default
		// socket path even if DOCKER_HOST was malformed.
		socketPath = "/var/run/docker.sock"
	}

	// Resolve the destination directory. Many systems use a symlinked run dir
	// (for example, /var/run -> /run). bwrap may refuse to create mount targets
	// under symlinked directories, so we mount at the resolved destination path.
	dstPath := socketPath

	resolvedDir, evalErr := filepath.EvalSymlinks(filepath.Dir(socketPath))
	if evalErr == nil && filepath.IsAbs(resolvedDir) {
		dstPath = filepath.Clean(filepath.Join(resolvedDir, filepath.Base(socketPath)))
	}

	depth := paths.Depth(dstPath)
	if depth > 32767 {
		return mountPlan{}, fmt.Errorf("docker socket path %q is too deeply nested (%d)", dstPath, depth)
	}

	if !dockerEnabled {
		if debugf != nil {
			if dstPath != socketPath {
				debugf("docker: masking socket at %q (resolved=%q)", socketPath, dstPath)
			} else {
				debugf("docker: masking socket at %q", socketPath)
			}
		}

		// Mask the socket by bind-mounting /dev/null over it. This is deterministic
		// and does not depend on whether the socket currently exists.
		spec := mountSpec{
			mount:     Mount{Kind: MountRoBind, Src: "/dev/null", Dst: dstPath},
			pathDepth: depth,
		}

		return mountPlan{specs: []mountSpec{spec}}, nil
	}

	resolved, err := filepath.EvalSymlinks(socketPath)
	if err != nil {
		return mountPlan{}, fmt.Errorf("docker socket not found: %q: %w", socketPath, err)
	}

	_, statErr := os.Stat(resolved)
	if statErr != nil {
		return mountPlan{}, fmt.Errorf("docker socket not found: %q: %w", resolved, statErr)
	}

	if debugf != nil {
		debugf("docker: exposing socket %q (resolved=%q dst=%q)", socketPath, resolved, dstPath)
	}

	spec := mountSpec{
		mount:     Mount{Kind: MountBind, Src: resolved, Dst: dstPath},
		pathDepth: depth,
	}

	return mountPlan{specs: []mountSpec{spec}}, nil
}

// dockerSocketPathFromEnv extracts a unix socket path from DOCKER_HOST.
//
// Non-unix schemes are ignored.
func dockerSocketPathFromEnv(hostEnv map[string]string) string {
	if hostEnv == nil {
		return ""
	}

	dockerHost := hostEnv["DOCKER_HOST"]
	if dockerHost == "" {
		return ""
	}

	switch {
	case strings.HasPrefix(dockerHost, "unix:///"):
		return dockerHost[len("unix://"):]
	case strings.HasPrefix(dockerHost, "unix:/"):
		return dockerHost[len("unix:"):]
	default:
		return ""
	}
}
