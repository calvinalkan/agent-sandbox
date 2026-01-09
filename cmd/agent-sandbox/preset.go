package main

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
// Stub implementation - expansion logic will be added in a future ticket.
func resolveBasePreset(_ PresetContext, _ map[string]bool) PresetPaths {
	return PresetPaths{}
}

// resolveCachesPreset returns paths for the @caches preset.
// Stub implementation - expansion logic will be added in a future ticket.
func resolveCachesPreset(_ PresetContext, _ map[string]bool) PresetPaths {
	return PresetPaths{}
}

// resolveGitPreset returns paths for the @git preset.
// Stub implementation - expansion logic will be added in a future ticket.
func resolveGitPreset(_ PresetContext, _ map[string]bool) PresetPaths {
	return PresetPaths{}
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
