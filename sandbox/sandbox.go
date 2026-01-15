//go:build linux

// Package sandbox provides a programmatic API for building commands that run
// inside a bubblewrap (bwrap) filesystem sandbox.
//
// The sandbox package does not execute commands itself; instead it constructs an
// unstarted *exec.Cmd that runs `bwrap ... -- <argv...>`.
//
// # Platform / Dependencies
//
// This package is Linux-only (see the build tag above) and requires the
// `bwrap` executable to be available in PATH at runtime.
//
// # Planning vs Execution
//
// Sandbox construction (New/NewWithEnvironment) validates caller input and may
// perform a small amount of cheap filesystem checking (for example, verifying
// that [Commands.Launcher] exists and is executable when command wrappers are
// configured).
//
// Filesystem-dependent planning (preset expansion that inspects the repo, glob
// expansion, symlink resolution, wrapper target discovery via PATH (from
// [Environment.HostEnv]), reading wrapper scripts referenced via [Wrapper.Path],
// docker socket resolution, etc.) is performed during Sandbox construction.
//
// Planning is designed to be inexpensive enough to run often, and the resulting
// Sandbox is a snapshot of the host environment at that point in time. If you
// expect the host filesystem or environment to change, prefer constructing a new
// Sandbox close to command execution.
//
// # Security Note
//
// This library is intended to reduce accidental access and constrain tools
// through mount policy and namespace isolation. It is not a complete security
// boundary against a determined attacker. Your effective security properties
// depend on bubblewrap, kernel features (namespaces, userns), and the policy you
// configure.
package sandbox

//revive:disable:max-public-structs

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
)

// Sandbox represents a reusable sandbox configuration and environment.
//
// A Sandbox must not be copied after first use.
//
// A Sandbox is safe for concurrent use. Each call to [Sandbox.Command] may
// allocate per-invocation resources (for example, wrapper-script file
// descriptors). Callers must call the returned cleanup function once they are
// finished with the returned *exec.Cmd (even if the command is never started).
//
// A Sandbox computes a deterministic, filesystem-derived plan during construction,
// including glob expansion, symlink resolution, wrapper discovery, and other
// preset-derived discovery.
//
// To pick up changes that affect planning (new files matching globs, git HEAD/branch
// changes for git presets, changes to PATH affecting wrapper target discovery,
// edits to wrapper scripts referenced via [Wrapper.Path], changes to DOCKER_HOST,
// etc.), create a new Sandbox.
//
// Sandbox construction is typically cheap (mostly path parsing and filesystem
// metadata lookups), so it is reasonable to create Sandboxes close to where you
// execute commands rather than keeping a long-lived instance.
//
// Note: even with eager planning, the host filesystem can still change between
// Sandbox construction and [Sandbox.Command] (for example, a source path being
// deleted). In that case, command construction or bubblewrap itself may fail at
// execution time.
//
// For deterministic behavior (tests/embedding), construct via NewWithEnvironment.
//
// Example:
//
//	s, err := sandbox.New(cfg)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	cmd, cleanup, err := s.Command(ctx, []string{"git", "status"})
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer cleanup()
//
//	cmd.Stdout = os.Stdout
//	cmd.Stderr = os.Stderr
//	if err := cmd.Run(); err != nil {
//		log.Fatal(err)
//	}
type Sandbox struct {
	noCopy noCopy

	// v is the validated snapshot of cfg+env. It is nil only for a zero-value
	// Sandbox that was not constructed via New/NewWithEnvironment.
	v *validated

	// plan is the deterministic, filesystem-derived view of this sandbox.
	//
	// It is computed during construction (New/NewWithEnvironment).
	plan *plan
}

// New constructs a Sandbox using an Environment derived from the current
// process (see [DefaultEnvironment]).
func New(cfg *Config) (*Sandbox, error) {
	env, err := DefaultEnvironment()
	if err != nil {
		return nil, fmt.Errorf("sandbox: creating default environment: %w", err)
	}

	return NewWithEnvironment(cfg, env)
}

// NewWithEnvironment constructs a Sandbox using an explicit environment.
//
// This is useful for testing, embedding, or when the sandbox should run commands
// as-if from a different working directory or environment than the current
// process.
//
// Note: cfg and env are deep-copied during construction, so subsequent
// modifications to the passed values do not affect the Sandbox.
func NewWithEnvironment(cfg *Config, env Environment) (*Sandbox, error) {
	clonedCfg := cloneConfig(cfg)
	env = cloneEnvironment(env)

	err := validateConfigAndEnv(&clonedCfg, env)
	if err != nil {
		return nil, fmt.Errorf("sandbox: validating: %w", err)
	}

	validatedCfg := validated{cfg: clonedCfg, env: env, envSlice: envMapToSliceSorted(env.HostEnv)}

	plan, err := buildPlan(&validatedCfg)
	if err != nil {
		return nil, fmt.Errorf("sandbox: planning: %w", err)
	}

	return &Sandbox{v: &validatedCfg, plan: plan}, nil
}

// DefaultEnvironment returns an Environment derived from the current process.
//
// HomeDir is resolved from os.UserHomeDir(). WorkDir is resolved from os.Getwd().
// HostEnv is populated from os.Environ(). Invalid KEY=VALUE entries are ignored.
func DefaultEnvironment() (Environment, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return Environment{}, fmt.Errorf("get working directory: %w", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Environment{}, fmt.Errorf("get home directory: %w", err)
	}

	hostEnv := make(map[string]string, len(os.Environ()))
	for _, kv := range os.Environ() {
		// Best-effort parse of KEY=VALUE. Invalid entries are ignored.
		key, value, ok := strings.Cut(kv, "=")
		if !ok || key == "" {
			continue
		}

		hostEnv[key] = value
	}

	return Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: hostEnv,
	}, nil
}

// Config configures sandbox behavior.
//
// Config is intentionally independent from any config-file loading or CLI flag
// parsing; callers are expected to produce a final Config before constructing a
// Sandbox.
//
// The zero value of Config is a usable default:
//   - Network defaults to enabled
//   - Docker defaults to disabled
//   - BaseFS defaults to BaseFSHost
//   - Filesystem.Presets defaults to "@all"
//
// Some errors depend on the host filesystem (for example, missing mount targets,
// wrapper discovery failures, invalid globs). These are discovered during planning
// and returned from [New] / [NewWithEnvironment].
type Config struct {
	// Network controls whether the sandbox shares the host network namespace.
	// If nil, the implementation applies its default behavior (true).
	Network *bool

	// Docker controls docker socket exposure inside the sandbox.
	// If nil, the implementation applies its default behavior (false).
	//
	// Note: the sandbox always emits an explicit mount for the docker socket:
	// when disabled it is masked (typically by bind-mounting /dev/null), and when
	// enabled the resolved socket path is bind-mounted read-write.
	Docker *bool

	// BaseFS controls how the sandbox root filesystem is constructed.
	//
	// The default (BaseFSHost) bind-mounts the host root filesystem at
	// "/" read-only. BaseFSEmpty mounts a fresh tmpfs at "/".
	BaseFS BaseFS

	// Filesystem configures filesystem policy mounts and low-level mounts.
	Filesystem Filesystem

	// Commands configures command wrapper behavior.
	Commands Commands

	// TempDir is the host temp directory to bind-mount as /tmp inside the sandbox.
	//
	// When set, the host path is bind-mounted to /tmp and TMPDIR is set to "/tmp"
	// in the sandbox environment. This normalizes temp directory access regardless
	// of the host's TMPDIR setting.
	//
	// When empty, no temp directory normalization is done.
	TempDir string

	// Debugf receives debug messages from sandbox preparation and command construction.
	Debugf Debugf
}

// Commands configures command wrapper behavior.
//
// # Why a Launcher Binary?
//
// Command wrappers use a compiled launcher binary rather than mounting scripts
// directly over command paths. This design provides several benefits:
//
//  1. Opacity: If a shell script were mounted at /usr/bin/git, users could
//     `cat /usr/bin/git` and read the script source, revealing the real binary
//     location. With a compiled launcher, `cat` shows binary data.
//
//  2. Performance: Wrapper logic (like the @git preset) can be implemented in
//     Go rather than shell, enabling faster and more sophisticated argument parsing.
//
//  3. Discovery prevention: Combined with 0111 (execute-only) directory permissions
//     on MountPath, users cannot easily list what commands are wrapped or where
//     real binaries are mounted.
//
// Note: This is deterrence, not absolute security. Determined users can discover
// wrapper mounts via /proc/self/mountinfo. Filesystem rules (RO/RW/Exclude) are
// the ultimate enforcement boundary.
//
// When Block or Wrappers is non-empty, Launcher must be set.
type Commands = commands

type commands struct {
	// Block lists commands to block entirely.
	// These commands will print an error and exit when invoked.
	Block []string

	// Wrappers intercept commands with custom scripts.
	//
	// For wrapped commands, the first PATH match is exposed at
	// `{MountPath}/bin/{cmd}` so wrapper scripts can exec the real binary.
	//
	// Wrapper discovery is PATH-based. If Block or Wrappers is non-empty and
	// PATH is empty, or a configured command cannot be found in PATH, an error
	// is returned.
	Wrappers map[string]Wrapper

	// Launcher is the absolute host path to a multicall launcher binary.
	//
	// When set, the sandbox bind-mounts this binary over each discovered command
	// target (e.g., /usr/bin/git). The launcher is expected to dispatch based on
	// argv[0] and handle the wrapper logic.
	//
	// Required when Block or Wrappers is non-empty.
	Launcher string

	// MountPath is the sandbox path where wrapper runtime files are mounted.
	//
	// The following subdirectories are created with mode 0111 (execute-only,
	// prevents directory listing):
	//
	//   {MountPath}/
	//   ├── bin/       # real binaries
	//   └── policies/  # wrapper scripts
	//
	// If empty, defaults to `/run/{basename(Launcher)}`.
	//
	// Set this explicitly if you need a stable, launcher-independent location.
	MountPath string
}

// BaseFS controls how the sandbox root filesystem (/) is constructed.
//
// In BaseFSHost (default), the sandbox starts by bind-mounting the host
// root filesystem at "/" read-only (bwrap: `--ro-bind / /`). Policy mounts then
// refine access by re-mounting selected paths read-write or masking them.
//
// In BaseFSEmpty, the sandbox starts from an empty tmpfs mounted at "/" (bwrap:
// `--tmpfs /`). This is useful when you want an explicit allowlist of host paths
// available inside the sandbox.
//
// Note: In BaseFSEmpty you usually need to mount a minimal runtime for
// dynamically-linked binaries (for example `/usr` and `/lib*`), plus any config
// files you rely on.
//
// Example:
//
//	cfg := sandbox.Config{
//		BaseFS: sandbox.BaseFSEmpty,
//		Filesystem: sandbox.Filesystem{
//			Presets: []string{"!@all"},
//			Mounts: []sandbox.Mount{
//				sandbox.RO("/usr"),
//				sandbox.RO("/lib"),
//				sandbox.RO("/lib64"),
//				sandbox.RO("/bin"),
//				sandbox.RW("."),
//			},
//		},
//	}
type BaseFS string

const (
	// BaseFSHost bind-mounts the host root filesystem at "/" read-only.
	BaseFSHost BaseFS = "host"
	// BaseFSEmpty mounts a fresh tmpfs at "/".
	BaseFSEmpty BaseFS = "empty"
)

// Filesystem configures filesystem mounts.
//
// There are two categories of mounts:
//
//   - Policy mounts (created via RO/RW/Exclude and their Try variants): these accept
//     absolute, relative, "~" and glob patterns. They are resolved against the host
//     filesystem during planning (tilde expansion, glob expansion, symlink
//     resolution).
//
//     Each resolved host path is mounted inside the sandbox at its resolved absolute
//     path. This means the destination may differ from what you typed when the host
//     path traverses symlinks (for example, /var/run -> /run). If you need explicit
//     control over the sandbox destination path, use a direct mount (RoBind/Bind/etc.).
//
//   - Direct mounts (RoBind, Bind, Tmpfs, Dir, RoBindData, ...): these require
//     absolute paths and are appended after policy mounts in a deterministic order.
//
// Policy precedence rules:
//   - More specific destinations win (a deeper path beats an ancestor).
//   - For the same resolved destination:
//   - exact path mounts beat glob mounts
//   - otherwise, later mounts win (presets are earlier than cfg.Mounts)
//
// There is no inherent priority between RO/RW/Exclude beyond these rules. For
// example, an Exclude can be overridden by a later or more specific RW mount.
type Filesystem = filesystem

type filesystem struct {
	// Presets are optional built-in bundles of filesystem rules (e.g. "@base",
	// "@git").
	//
	// Semantics:
	//   - nil: apply the default preset set (equivalent to []string{"@all"})
	//   - empty but non-nil: apply no presets
	Presets []string

	// Mounts are applied after presets, in the order provided.
	Mounts []Mount
}

// Wrapper configures a script to intercept a command.
//
// Wrappers intercept commands by mounting a launcher binary (see
// [Commands.Launcher]) over their discovered locations in PATH.
// When the command is invoked, the launcher is responsible for running the
// wrapper script.
//
// Wrapper scripts are mounted into the sandbox at `{MountPath}/policies/{cmd}` as
// an executable file (mode 0555).
//
// For wrapped commands, the first PATH match is available at `{MountPath}/bin/{cmd}`.
//
// # Runtime Layout
//
// Command wrappers use a runtime directory inside the sandbox, configured via
// [Commands.MountPath] (default: /run/{basename(Launcher)}):
//
//	{MountPath}/
//	├── bin/{cmd}       # real binary (first PATH match)
//	└── policies/{cmd}  # wrapper scripts
//
// Example:
//
//	cfg.Commands = sandbox.Commands{
//		Block: []string{"rm", "chmod"},
//		Wrappers: map[string]sandbox.Wrapper{
//			"git": sandbox.Wrap("~/bin/git-wrapper.sh"),
//		},
//		Launcher: "/path/to/launcher",
//	}
type Wrapper struct {
	// Path is the host path to a wrapper script.
	// May be absolute, relative to [Environment.WorkDir], or "~"-prefixed.
	//
	// When Path is used, the script file is read during planning (during Sandbox
	// construction) and cached; later changes to the file are not picked up by an
	// existing Sandbox.
	Path string

	// InlineScript is inline script content.
	// Takes precedence over Path if both are set.
	InlineScript string
}

// Wrap creates a wrapper that uses a script file.
func Wrap(path string) Wrapper {
	return Wrapper{Path: path}
}

// MountKind describes a mount or policy operation understood by this package.
//
// Some kinds correspond directly to bubblewrap flags (for example MountRoBind
// -> `--ro-bind`), while others are higher-level policy kinds that are resolved
// against the host filesystem during planning (for example MountReadOnly
// produced by [RO]).
//
// The zero value is invalid.
type MountKind int

const (
	// MountReadOnly grants read-only access to a path pattern (RO helper).
	MountReadOnly MountKind = iota + 1

	// MountReadWrite grants read-write access to a path pattern (RW helper).
	MountReadWrite

	// MountExclude hides a path pattern inside the sandbox (Exclude helper).
	MountExclude

	// MountRoBind adds a read-only bind mount (--ro-bind).
	MountRoBind

	// MountRoBindTry adds a read-only bind mount that is skipped if missing
	// (--ro-bind-try).
	MountRoBindTry

	// MountBind adds a read-write bind mount (--bind).
	MountBind

	// MountBindTry adds a read-write bind mount that is skipped if missing
	// (--bind-try).
	MountBindTry

	// MountTmpfs mounts an empty tmpfs at Dst (--tmpfs).
	MountTmpfs

	// MountDir creates a directory mount point (--dir).
	MountDir

	// MountRoBindData mounts file data from an inherited FD to Dst
	// (--ro-bind-data). Perms describes the file mode.
	MountRoBindData

	// MountReadOnlyTry grants read-only access to a path pattern but is skipped if
	// it does not exist at planning time (ROTry helper).
	MountReadOnlyTry

	// MountReadWriteTry grants read-write access to a path pattern but is skipped
	// if it does not exist at planning time (RWTry helper).
	MountReadWriteTry

	// MountExcludeTry hides a path pattern inside the sandbox but is skipped if it
	// does not exist at planning time (ExcludeTry helper).
	MountExcludeTry

	// MountExcludeFile hides a path by masking it with an unreadable empty file.
	MountExcludeFile

	// MountExcludeDir hides a path by masking it with an empty directory.
	MountExcludeDir
)

// RO grants read-only access to a path pattern.
//
// The path may be absolute, relative, "~"-prefixed, or a glob pattern.
// If the pattern matches no existing host paths, planning returns an error.
func RO(path string) Mount {
	return Mount{Kind: MountReadOnly, Dst: path}
}

// ROTry grants read-only access to a path pattern.
//
// If the path does not exist on the host at planning time, it is ignored.
func ROTry(path string) Mount {
	return Mount{Kind: MountReadOnlyTry, Dst: path}
}

// RW grants read-write access to a path pattern.
//
// The path may be absolute, relative, "~"-prefixed, or a glob pattern.
// If the pattern matches no existing host paths, planning returns an error.
func RW(path string) Mount {
	return Mount{Kind: MountReadWrite, Dst: path}
}

// RWTry grants read-write access to a path pattern.
//
// If the path does not exist on the host at planning time, it is ignored.
func RWTry(path string) Mount {
	return Mount{Kind: MountReadWriteTry, Dst: path}
}

// Exclude hides a path pattern inside the sandbox.
//
// The path may be absolute, relative, "~"-prefixed, or a glob pattern.
// If the pattern matches no existing host paths, planning returns an error.
func Exclude(path string) Mount {
	return Mount{Kind: MountExclude, Dst: path}
}

// ExcludeTry hides a path pattern inside the sandbox.
//
// If the path does not exist on the host at planning time, it is ignored.
func ExcludeTry(path string) Mount {
	return Mount{Kind: MountExcludeTry, Dst: path}
}

// ExcludeFile hides a single path inside the sandbox by masking it with an unreadable
// empty file.
//
// The mask is applied regardless of whether the path exists on the host, which makes
// it useful to prevent both reading and creating sensitive files.
//
// Unlike Exclude/ExcludeTry, ExcludeFile does not accept glob patterns.
func ExcludeFile(path string) Mount {
	return Mount{Kind: MountExcludeFile, Dst: path}
}

// ExcludeDir hides a single path inside the sandbox by masking it with an empty
// directory (implemented as a tmpfs mount).
//
// The mask is applied regardless of whether the path exists on the host, which makes
// it useful to block access to and creation of whole directory trees.
//
// Unlike Exclude/ExcludeTry, ExcludeDir does not accept glob patterns.
func ExcludeDir(path string) Mount {
	return Mount{Kind: MountExcludeDir, Dst: path}
}

// RoBind returns a read-only bind mount from src (host path) to dst (sandbox path).
func RoBind(src, dst string) Mount {
	return Mount{Kind: MountRoBind, Src: src, Dst: dst}
}

// RoBindTry returns a read-only bind mount from src (host path) to dst (sandbox path)
// that is skipped if src does not exist.
func RoBindTry(src, dst string) Mount {
	return Mount{Kind: MountRoBindTry, Src: src, Dst: dst}
}

// Bind returns a read-write bind mount from src (host path) to dst (sandbox path).
func Bind(src, dst string) Mount {
	return Mount{Kind: MountBind, Src: src, Dst: dst}
}

// BindTry returns a read-write bind mount from src (host path) to dst (sandbox path)
// that is skipped if src does not exist.
func BindTry(src, dst string) Mount {
	return Mount{Kind: MountBindTry, Src: src, Dst: dst}
}

// Tmpfs returns an empty tmpfs mount at dst (sandbox path).
func Tmpfs(dst string) Mount {
	return Mount{Kind: MountTmpfs, Dst: dst}
}

// Dir returns a directory creation operation at dst (sandbox path).
//
// This maps to bwrap's --dir and is typically used to ensure parent directories
// exist before bind mounts.
//
// If perms is provided, the sandbox will apply it using bwrap's --chmod after
// all mounts have been materialized.
func Dir(dst string, perms ...os.FileMode) Mount {
	m := Mount{Kind: MountDir, Dst: dst}
	if len(perms) > 0 {
		m.Perms = perms[0]
	}

	return m
}

// Debugf receives debug messages from sandbox preparation and command
// construction.
//
// The function should be safe to call from any goroutine.
type Debugf func(format string, args ...any)

// cloneConfig returns a deep copy of cfg. Slices, maps, and pointers are
// cloned so modifications to the copy don't affect the original.
func cloneConfig(cfg *Config) Config {
	out := *cfg

	if cfg.Network != nil {
		v := *cfg.Network
		out.Network = &v
	}

	if cfg.Docker != nil {
		v := *cfg.Docker
		out.Docker = &v
	}

	out.BaseFS = cfg.BaseFS
	out.Filesystem.Presets = slices.Clone(cfg.Filesystem.Presets)
	out.Filesystem.Mounts = slices.Clone(cfg.Filesystem.Mounts)

	out.Commands.Block = slices.Clone(cfg.Commands.Block)
	out.Commands.Launcher = cfg.Commands.Launcher

	out.Commands.MountPath = cfg.Commands.MountPath
	if cfg.Commands.Wrappers != nil {
		out.Commands.Wrappers = make(map[string]Wrapper, len(cfg.Commands.Wrappers))
		maps.Copy(out.Commands.Wrappers, cfg.Commands.Wrappers)
	}

	out.Debugf = cfg.Debugf

	return out
}

// cloneEnvironment returns a deep copy of env.
func cloneEnvironment(env Environment) Environment {
	out := env

	if env.HostEnv == nil {
		out.HostEnv = map[string]string{}
	} else {
		out.HostEnv = make(map[string]string, len(env.HostEnv))
		maps.Copy(out.HostEnv, env.HostEnv)
	}

	return out
}

type validated struct {
	cfg      Config
	env      Environment
	envSlice []string
}

// marker got go vet.
type noCopy struct{}

func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

// internalErrorf reports an internal invariant violation.
//
// These errors indicate a bug in this package (or an unexpected environment
// mismatch after planning), rather than invalid caller input.
func internalErrorf(op, format string, args ...any) error {
	detail := fmt.Sprintf(format, args...)

	if op == "" {
		return fmt.Errorf("sandbox: internal error: %s", detail)
	}

	return fmt.Errorf("sandbox: internal error: %s: %s", op, detail)
}
