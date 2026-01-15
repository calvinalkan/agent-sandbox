//go:build linux

package sandbox

// Environment describes the host process environment used to resolve and build a sandbox.
type Environment struct {
	// HomeDir is the host home directory.
	HomeDir string
	// WorkDir is the host working directory.
	WorkDir string
	// HostEnv is a snapshot of environment variables (e.g. HOME, PATH, TMPDIR).
	//
	// It is used for path resolution and wrapper discovery. By default, it is
	// also used as the environment for the command executed inside the sandbox.
	// If HostEnv is nil, an empty environment is used.
	HostEnv map[string]string
}
