package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrInvalidGitFile indicates that a .git file has invalid format.
var ErrInvalidGitFile = errors.New("invalid .git file format")

// PresetPaths holds resolved paths for a preset.
type PresetPaths struct {
	Ro      []string
	Rw      []string
	Exclude []string
}

// PresetContext provides information needed to resolve presets.
type PresetContext struct {
	HomeDir     string
	WorkDir     string
	GitWorktree string // empty if not in a worktree
	// LoadedConfigPaths are absolute paths to config files that were actually loaded
	// (global + project OR --config). Used by @base to protect sandbox config from
	// modification inside the sandbox (per SPEC security guarantees).
	LoadedConfigPaths []string
}

// Preset represents a built-in filesystem preset.
type Preset struct {
	Name        string
	Description string
	Composite   bool // true for @all, @lint/all (they expand sub-presets)
	// Resolve returns the paths for this preset.
	// - ctx: provides home dir, work dir, git worktree info
	// - disabled: presets to skip (for !@preset negation support)
	// Simple presets ignore disabled; composite presets check it.
	Resolve func(ctx PresetContext, disabled map[string]bool) PresetPaths
}

// PresetRegistry holds all built-in presets.
// This is the authoritative list of available presets.
var PresetRegistry = map[string]*Preset{
	"@base": {
		Name:        "@base",
		Description: "Core sandbox: working directory writable, home protected, temp writable, agent configs writable, secrets excluded, sandbox config protected",
		Composite:   false,
		Resolve:     resolveBasePreset,
	},
	"@caches": {
		Name:        "@caches",
		Description: "Build tool caches writable (~/.cache, ~/.bun, ~/go, ~/.npm, ~/.cargo)",
		Composite:   false,
		Resolve:     resolveCachesPreset,
	},
	"@git": {
		Name:        "@git",
		Description: "Git hooks and config protected (.git/hooks, .git/config), with automatic worktree support",
		Composite:   false,
		Resolve:     resolveGitPreset,
	},
	"@lint/ts": {
		Name:        "@lint/ts",
		Description: "TypeScript/JavaScript lint configs protected (biome, eslint, prettier, tsconfig)",
		Composite:   false,
		Resolve:     resolveLintTSPreset,
	},
	"@lint/go": {
		Name:        "@lint/go",
		Description: "Go lint configs protected (golangci)",
		Composite:   false,
		Resolve:     resolveLintGoPreset,
	},
	"@lint/python": {
		Name:        "@lint/python",
		Description: "Python lint configs protected (ruff, flake8, mypy, pylint, pyproject.toml)",
		Composite:   false,
		Resolve:     resolveLintPythonPreset,
	},
	"@lint/all": {
		Name:        "@lint/all",
		Description: "All lint presets combined (@lint/ts, @lint/go, @lint/python)",
		Composite:   true,
		Resolve:     resolveLintAllPreset,
	},
	"@all": {
		Name:        "@all",
		Description: "Everything: @base, @caches, @git, @lint/all",
		Composite:   true,
		Resolve:     resolveAllPreset,
	},
}

// resolveBasePreset returns paths for the @base preset.
// It provides the core sandbox configuration:
//   - WorkDir and /tmp are writable (so agents can work)
//   - Home directory is read-only (protects existing files)
//   - Config files are read-only (prevents config tampering)
//   - Secrets (~/.ssh, ~/.gnupg, ~/.aws) are excluded
//
// Note: @base ignores the disabled parameter (it's a simple preset).
func resolveBasePreset(ctx PresetContext, _ map[string]bool) PresetPaths {
	paths := PresetPaths{}

	// Read-Write: WorkDir and /tmp
	// These must be writable for agents to do useful work
	paths.Rw = []string{
		ctx.WorkDir,
		"/tmp",
	}

	// Read-Only: Home directory (protects existing files)
	paths.Ro = []string{ctx.HomeDir}

	// Read-Only: Project config files in WorkDir (if they could exist)
	// We always add these paths - they'll be skipped if they don't exist
	// during path resolution. This ensures config files are protected.
	paths.Ro = append(paths.Ro,
		ctx.WorkDir+"/.agent-sandbox.json",
		ctx.WorkDir+"/.agent-sandbox.jsonc",
	)

	// Read-Only: Any config files that were actually loaded
	// This includes global config and explicit --config file
	paths.Ro = append(paths.Ro, ctx.LoadedConfigPaths...)

	// Exclude: Secrets - these should not be readable at all
	paths.Exclude = []string{
		ctx.HomeDir + "/.ssh",
		ctx.HomeDir + "/.gnupg",
		ctx.HomeDir + "/.aws",
	}

	return paths
}

// resolveCachesPreset returns paths for the @caches preset.
// It makes build tool cache directories writable:
//   - ~/.cache (XDG cache, used by many tools)
//   - ~/.bun (Bun runtime and packages)
//   - ~/go (Go modules, build cache)
//   - ~/.npm (npm cache)
//   - ~/.cargo (Rust/Cargo cache)
//
// Note: @caches ignores the disabled parameter (it's a simple preset).
func resolveCachesPreset(ctx PresetContext, _ map[string]bool) PresetPaths {
	return PresetPaths{
		Rw: []string{
			ctx.HomeDir + "/.cache",
			ctx.HomeDir + "/.bun",
			ctx.HomeDir + "/go",
			ctx.HomeDir + "/.npm",
			ctx.HomeDir + "/.cargo",
		},
	}
}

// GitPaths holds resolved paths for a git repository.
// For normal repos, only Hooks and Config are set.
// For worktrees, MainHooks and MainConfig are also set (pointing to the main repo).
type GitPaths struct {
	Hooks      string // worktree or normal .git/hooks
	Config     string // worktree or normal .git/config
	MainHooks  string // main repo hooks (only for worktrees)
	MainConfig string // main repo config (only for worktrees)
}

// resolveGitPreset returns paths for the @git preset.
// It protects .git/hooks and .git/config from modification.
// For worktrees, it also protects the main repo's hooks and config.
//
// Note: @git ignores the disabled parameter (it's a simple preset).
func resolveGitPreset(ctx PresetContext, _ map[string]bool) PresetPaths {
	gitPaths, err := resolveGitPaths(ctx.WorkDir)
	if err != nil {
		// Error reading git files - return empty paths
		return PresetPaths{}
	}

	paths := PresetPaths{}

	// Add worktree or normal repo paths (empty strings are skipped)
	if gitPaths.Hooks != "" {
		paths.Ro = append(paths.Ro, gitPaths.Hooks)
	}

	if gitPaths.Config != "" {
		paths.Ro = append(paths.Ro, gitPaths.Config)
	}

	// Add main repo paths (only set for worktrees)
	if gitPaths.MainHooks != "" {
		paths.Ro = append(paths.Ro, gitPaths.MainHooks)
	}

	if gitPaths.MainConfig != "" {
		paths.Ro = append(paths.Ro, gitPaths.MainConfig)
	}

	return paths
}

// resolveGitPaths detects git repository type and returns paths to protect.
// Returns an empty GitPaths (all fields empty) if workDir is not a git repository.
// Returns error if .git file format is invalid.
func resolveGitPaths(workDir string) (GitPaths, error) {
	gitPath := filepath.Join(workDir, ".git")

	info, err := os.Lstat(gitPath)
	if errors.Is(err, os.ErrNotExist) {
		// No .git, not a git repo - return empty paths (not an error)
		return GitPaths{}, nil
	}

	if err != nil {
		return GitPaths{}, fmt.Errorf("checking .git path: %w", err)
	}

	var result GitPaths

	if info.IsDir() {
		// Normal repo - .git is a directory
		result.Hooks = filepath.Join(gitPath, "hooks")
		result.Config = filepath.Join(gitPath, "config")

		return result, nil
	}

	// Worktree: .git is a file containing "gitdir: /path/to/.git/worktrees/name"
	content, err := os.ReadFile(gitPath)
	if err != nil {
		return GitPaths{}, fmt.Errorf("reading .git file: %w", err)
	}

	// Parse "gitdir: /path/to/.git/worktrees/name"
	gitdirLine := strings.TrimSpace(string(content))
	if !strings.HasPrefix(gitdirLine, "gitdir: ") {
		return GitPaths{}, fmt.Errorf("%w: expected 'gitdir: <path>', got %q", ErrInvalidGitFile, gitdirLine)
	}

	worktreeGitDir := strings.TrimPrefix(gitdirLine, "gitdir: ")

	// Handle relative paths in gitdir (resolve relative to workDir)
	if !filepath.IsAbs(worktreeGitDir) {
		worktreeGitDir = filepath.Join(workDir, worktreeGitDir)
	}

	worktreeGitDir, err = filepath.Abs(worktreeGitDir)
	if err != nil {
		return GitPaths{}, fmt.Errorf("resolving worktree git dir: %w", err)
	}

	// Protect worktree-specific hooks/config
	result.Hooks = filepath.Join(worktreeGitDir, "hooks")
	result.Config = filepath.Join(worktreeGitDir, "config")

	// Find commondir to get main repo's .git
	commondirPath := filepath.Join(worktreeGitDir, "commondir")

	commondirContent, err := os.ReadFile(commondirPath)
	if err == nil {
		commondir := strings.TrimSpace(string(commondirContent))
		mainGitDir := filepath.Join(worktreeGitDir, commondir)

		mainGitDir, err = filepath.Abs(mainGitDir)
		if err == nil {
			// Also protect main repo's hooks/config
			result.MainHooks = filepath.Join(mainGitDir, "hooks")
			result.MainConfig = filepath.Join(mainGitDir, "config")
		}
	}

	return result, nil
}

// resolveLintTSPreset returns paths for the @lint/ts preset.
// Stub implementation - expansion logic will be added in a future ticket.
func resolveLintTSPreset(_ PresetContext, _ map[string]bool) PresetPaths {
	return PresetPaths{}
}

// resolveLintGoPreset returns paths for the @lint/go preset.
// Stub implementation - expansion logic will be added in a future ticket.
func resolveLintGoPreset(_ PresetContext, _ map[string]bool) PresetPaths {
	return PresetPaths{}
}

// resolveLintPythonPreset returns paths for the @lint/python preset.
// Stub implementation - expansion logic will be added in a future ticket.
func resolveLintPythonPreset(_ PresetContext, _ map[string]bool) PresetPaths {
	return PresetPaths{}
}

// resolveLintAllPreset returns paths for the @lint/all preset.
// It expands all lint/* presets, skipping any in the disabled map.
// Stub implementation - expansion logic will be added in a future ticket.
func resolveLintAllPreset(_ PresetContext, _ map[string]bool) PresetPaths {
	return PresetPaths{}
}

// resolveAllPreset returns paths for the @all preset.
// It expands @base, @caches, @git, and @lint/all, skipping any in the disabled map.
// Stub implementation - expansion logic will be added in a future ticket.
func resolveAllPreset(_ PresetContext, _ map[string]bool) PresetPaths {
	return PresetPaths{}
}
