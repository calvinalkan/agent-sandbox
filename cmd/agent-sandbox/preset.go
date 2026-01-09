package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
// It protects TypeScript/JavaScript lint and formatting config files:
//   - biome.json, biome.jsonc
//   - .eslintrc, .eslintrc.js, .eslintrc.json, .eslintrc.yml, .eslintrc.yaml
//   - eslint.config.js, eslint.config.mjs, eslint.config.cjs
//   - .prettierrc, .prettierrc.js, .prettierrc.json, .prettierrc.yml, prettier.config.js
//   - tsconfig.json, tsconfig.*.json
//   - .editorconfig
//
// Note: @lint/ts ignores the disabled parameter (it's a simple preset).
func resolveLintTSPreset(ctx PresetContext, _ map[string]bool) PresetPaths {
	return PresetPaths{
		Ro: []string{
			// Biome
			ctx.WorkDir + "/biome.json",
			ctx.WorkDir + "/biome.jsonc",
			// ESLint (legacy config formats)
			ctx.WorkDir + "/.eslintrc",
			ctx.WorkDir + "/.eslintrc.js",
			ctx.WorkDir + "/.eslintrc.json",
			ctx.WorkDir + "/.eslintrc.yml",
			ctx.WorkDir + "/.eslintrc.yaml",
			// ESLint (flat config)
			ctx.WorkDir + "/eslint.config.js",
			ctx.WorkDir + "/eslint.config.mjs",
			ctx.WorkDir + "/eslint.config.cjs",
			// Prettier
			ctx.WorkDir + "/.prettierrc",
			ctx.WorkDir + "/.prettierrc.js",
			ctx.WorkDir + "/.prettierrc.json",
			ctx.WorkDir + "/.prettierrc.yml",
			ctx.WorkDir + "/prettier.config.js",
			// TypeScript (with glob for tsconfig.*.json)
			ctx.WorkDir + "/tsconfig.json",
			ctx.WorkDir + "/tsconfig.*.json",
			// EditorConfig
			ctx.WorkDir + "/.editorconfig",
		},
	}
}

// resolveLintGoPreset returns paths for the @lint/go preset.
// It protects Go lint and formatting config files:
//   - .golangci.yml, .golangci.yaml, .golangci.toml, .golangci.json
//   - .editorconfig
//
// Note: @lint/go ignores the disabled parameter (it's a simple preset).
func resolveLintGoPreset(ctx PresetContext, _ map[string]bool) PresetPaths {
	return PresetPaths{
		Ro: []string{
			// golangci-lint
			ctx.WorkDir + "/.golangci.yml",
			ctx.WorkDir + "/.golangci.yaml",
			ctx.WorkDir + "/.golangci.toml",
			ctx.WorkDir + "/.golangci.json",
			// EditorConfig
			ctx.WorkDir + "/.editorconfig",
		},
	}
}

// resolveLintPythonPreset returns paths for the @lint/python preset.
// It protects Python lint and formatting config files:
//   - pyproject.toml (contains ruff, black, mypy, etc. config)
//   - setup.cfg (legacy config)
//   - .flake8
//   - mypy.ini, .mypy.ini
//   - .pylintrc, pylintrc
//   - ruff.toml, .ruff.toml
//   - .editorconfig
//
// Note: @lint/python ignores the disabled parameter (it's a simple preset).
func resolveLintPythonPreset(ctx PresetContext, _ map[string]bool) PresetPaths {
	return PresetPaths{
		Ro: []string{
			// pyproject.toml (modern Python config: ruff, black, mypy, etc.)
			ctx.WorkDir + "/pyproject.toml",
			// setup.cfg (legacy config)
			ctx.WorkDir + "/setup.cfg",
			// flake8
			ctx.WorkDir + "/.flake8",
			// mypy
			ctx.WorkDir + "/mypy.ini",
			ctx.WorkDir + "/.mypy.ini",
			// pylint
			ctx.WorkDir + "/.pylintrc",
			ctx.WorkDir + "/pylintrc",
			// ruff
			ctx.WorkDir + "/ruff.toml",
			ctx.WorkDir + "/.ruff.toml",
			// EditorConfig
			ctx.WorkDir + "/.editorconfig",
		},
	}
}

// resolveLintAllPreset returns paths for the @lint/all preset.
// It expands all lint/* presets (@lint/ts, @lint/go, @lint/python),
// skipping any that are in the disabled map.
//
// This is a composite preset - it respects the disabled map to support
// negations like "!@lint/python".
func resolveLintAllPreset(ctx PresetContext, disabled map[string]bool) PresetPaths {
	var result PresetPaths

	// Map preset names to their resolve functions (avoids init cycle with PresetRegistry)
	lintPresets := []struct {
		name    string
		resolve func(PresetContext, map[string]bool) PresetPaths
	}{
		{"@lint/ts", resolveLintTSPreset},
		{"@lint/go", resolveLintGoPreset},
		{"@lint/python", resolveLintPythonPreset},
	}

	for _, p := range lintPresets {
		if disabled[p.name] {
			continue // Skip if negated via !@lint/ts etc.
		}

		paths := p.resolve(ctx, disabled)
		result.Ro = append(result.Ro, paths.Ro...)
		result.Rw = append(result.Rw, paths.Rw...)
		result.Exclude = append(result.Exclude, paths.Exclude...)
	}

	return result
}

// resolveAllPreset returns paths for the @all preset.
// It expands @base, @caches, @git, and @lint/all, skipping any in the disabled map.
//
// This is a composite preset - it respects the disabled map to support
// negations like "!@lint/python" or "!@caches".
func resolveAllPreset(ctx PresetContext, disabled map[string]bool) PresetPaths {
	var result PresetPaths

	// Map preset names to their resolve functions (avoids init cycle with PresetRegistry)
	allPresets := []struct {
		name    string
		resolve func(PresetContext, map[string]bool) PresetPaths
	}{
		{"@base", resolveBasePreset},
		{"@caches", resolveCachesPreset},
		{"@git", resolveGitPreset},
		{"@lint/all", resolveLintAllPreset},
	}

	for _, p := range allPresets {
		if disabled[p.name] {
			continue // Skip if negated via !@base etc.
		}

		paths := p.resolve(ctx, disabled)
		result.Ro = append(result.Ro, paths.Ro...)
		result.Rw = append(result.Rw, paths.Rw...)
		result.Exclude = append(result.Exclude, paths.Exclude...)
	}

	return result
}

// ErrUnknownPreset indicates that an unknown preset was referenced.
var ErrUnknownPreset = errors.New("unknown preset")

// AvailablePresets returns a sorted list of available preset names.
func AvailablePresets() []string {
	presets := make([]string, 0, len(PresetRegistry))
	for name := range PresetRegistry {
		presets = append(presets, name)
	}
	// Sort for consistent output
	slices.Sort(presets)

	return presets
}

// ExpandPresets processes the preset configuration and returns merged paths.
//
// The preset system works as follows:
//  1. @all is always the implicit starting point (default)
//  2. The presets list contains toggles applied on top of @all:
//     - "!@name" disables a preset
//     - "@name" (non-negated) re-enables a preset (useful after disabling a parent)
//  3. Last mention wins for toggle semantics
//  4. Composite presets (@all, @lint/all) check disabled map for sub-presets
//
// Examples:
//   - []                          → expands @all fully
//   - ["!@lint/python"]           → @all minus python lint configs
//   - ["!@lint/all", "@lint/go"]  → @all minus all lint, then add back go lint
//   - ["!@caches"]                → @all minus cache directories
func ExpandPresets(presets []string, ctx PresetContext) (PresetPaths, error) {
	// Track disabled presets (last mention wins)
	disabled := make(map[string]bool)

	// Track roots to expand in order (deterministic expansion)
	// @all is always the implicit starting point
	roots := []string{"@all"}
	seenRoot := map[string]bool{"@all": true}

	// Process user-specified presets
	for _, p := range presets {
		negated := strings.HasPrefix(p, "!")
		name := strings.TrimPrefix(p, "!")

		// Validate preset exists
		if _, exists := PresetRegistry[name]; !exists {
			return PresetPaths{}, fmt.Errorf("%w: %s (available: %s)", ErrUnknownPreset, name, strings.Join(AvailablePresets(), ", "))
		}

		// Toggle semantics: last mention wins
		disabled[name] = negated

		// Record explicit roots for non-negated presets
		// This allows users to do things like: ["!@all", "@base"]
		// or ["!@lint/all", "@lint/python"]
		if !negated && !seenRoot[name] {
			seenRoot[name] = true
			roots = append(roots, name)
		}
	}

	// Expand roots, respecting disabled presets
	// Duplicates are fine; specificity handles path conflicts later
	var result PresetPaths

	for _, name := range roots {
		if disabled[name] {
			continue
		}

		preset := PresetRegistry[name]
		paths := preset.Resolve(ctx, disabled)
		result.Ro = append(result.Ro, paths.Ro...)
		result.Rw = append(result.Rw, paths.Rw...)
		result.Exclude = append(result.Exclude, paths.Exclude...)
	}

	return result, nil
}
