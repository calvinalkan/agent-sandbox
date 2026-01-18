//go:build linux

package sandbox

// This file implements preset expansion.
//
// Presets are convenience bundles of filesystem policy mounts (RO/RW/Exclude)
// that approximate common "developer sandbox" needs. Presets never emit direct
// mounts; all output is expressed as policy mounts and then resolved against the
// host filesystem by the planner.
//
// Presets are applied in a fixed order for determinism.

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// expandPresets expands preset toggles into policy mounts.
//
// Supported presets:
//   - @all (default)
//   - @base
//   - @caches
//   - @agents
//   - @git
//   - @git-strict
//   - @lint/all
//   - @lint/ts
//   - @lint/go
//   - @lint/python
//
// Presets can be negated by prefixing with '!'. For example, []string{"!@all"}
// disables all defaults.
//
// Note: A nil preset slice means "defaults"; use an explicit empty slice
// (or "!@all") to request no presets.
func expandPresets(presets []string, env Environment) ([]Mount, error) {
	enabled, err := resolvePresetToggles(presets)
	if err != nil {
		return nil, err
	}

	// Emit preset mounts in a fixed order for determinism.
	var mounts []Mount

	if enabled["@base"] {
		mounts = append(mounts,
			RW(env.WorkDir),
			RO(env.HomeDir),
			ExcludeTry("~/.ssh"),
			ExcludeTry("~/.gnupg"),
			ExcludeTry("~/.aws"),
		)
	}

	if enabled["@caches"] {
		mounts = append(mounts,
			RWTry("~/.cache"),
			RWTry("~/.bun"),
			RWTry("~/go"),
			RWTry("~/.npm"),
			RWTry("~/.cargo"),
		)
	}

	if enabled["@agents"] {
		mounts = append(mounts,
			RWTry("~/.codex"),
			RWTry("~/.claude"),
			RWTry("~/.claude.json"),
			RWTry("~/.pi"),
		)
	}

	if enabled["@git"] || enabled["@git-strict"] {
		gitMounts, err := gitPresetRules(env.WorkDir, enabled["@git-strict"])
		if err != nil {
			return nil, err
		}

		mounts = append(mounts, gitMounts...)
	}

	if enabled["@lint/ts"] {
		mounts = append(mounts, lintTSMounts(env.WorkDir)...)
	}

	if enabled["@lint/go"] {
		mounts = append(mounts, lintGoMounts(env.WorkDir)...)
	}

	if enabled["@lint/python"] {
		mounts = append(mounts, lintPythonMounts(env.WorkDir)...)
	}

	// Shared lint protection: .editorconfig is protected when any lint preset is enabled.
	if enabled["@lint/ts"] || enabled["@lint/go"] || enabled["@lint/python"] {
		mounts = append(mounts, ROTry(filepath.Join(env.WorkDir, ".editorconfig")))
	}

	return mounts, nil
}

// resolvePresetToggles computes the final enabled/disabled state for each preset.
//
// Toggle semantics are "last one wins". Macros like @all and @lint/all expand to
// multiple underlying presets.
func resolvePresetToggles(presets []string) (map[string]bool, error) {
	known := map[string]bool{
		"@all":         true,
		"@base":        true,
		"@caches":      true,
		"@agents":      true,
		"@git":         true,
		"@git-strict":  true,
		"@lint/all":    true,
		"@lint/ts":     true,
		"@lint/go":     true,
		"@lint/python": true,
	}

	// Default: @all enabled when presets are not specified.
	//
	// A nil slice means "use defaults". A non-nil but empty slice means "no presets".
	if presets == nil {
		presets = []string{"@all"}
	}

	state := make(map[string]bool)

	for _, name := range presets {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, errors.New("unknown preset: empty preset name")
		}

		enable := true

		if strings.HasPrefix(name, "!") {
			enable = false
			name = strings.TrimPrefix(name, "!")
		}

		if !known[name] {
			return nil, fmt.Errorf("unknown preset: %s", name)
		}

		switch name {
		case "@all":
			// @all expands to the default preset set.
			for _, p := range []string{"@base", "@caches", "@agents", "@git", "@lint/all"} {
				applyPresetMacro(state, p, enable)
			}
		default:
			applyPresetMacro(state, name, enable)
		}
	}

	return state, nil
}

// applyPresetMacro applies a toggle for a preset name, expanding macros.
func applyPresetMacro(state map[string]bool, name string, enable bool) {
	switch name {
	case "@lint/all":
		state["@lint/ts"] = enable
		state["@lint/go"] = enable
		state["@lint/python"] = enable
	default:
		state[name] = enable
	}
}

func lintTSMounts(workDir string) []Mount {
	files := []string{
		"biome.json",
		"biome.jsonc",
		".eslintrc",
		".eslintrc.js",
		".eslintrc.json",
		"eslint.config.js",
		"eslint.config.mjs",
		".oxlintrc.json",
		".prettierrc",
		".prettierrc.json",
		"prettier.config.js",
		"tsconfig.json",
		"tsconfig.build.json",
	}

	out := make([]Mount, 0, len(files))
	for _, f := range files {
		out = append(out, ROTry(filepath.Join(workDir, f)))
	}

	return out
}

func lintGoMounts(workDir string) []Mount {
	files := []string{
		".golangci.yml",
		".golangci.yaml",
		".golangci.toml",
		".golangci.json",
	}

	out := make([]Mount, 0, len(files))
	for _, f := range files {
		out = append(out, ROTry(filepath.Join(workDir, f)))
	}

	return out
}

func lintPythonMounts(workDir string) []Mount {
	files := []string{
		"pyproject.toml",
		"setup.cfg",
		".flake8",
		"mypy.ini",
		".mypy.ini",
		".pylintrc",
		"ruff.toml",
		".ruff.toml",
	}

	out := make([]Mount, 0, len(files))
	for _, f := range files {
		out = append(out, ROTry(filepath.Join(workDir, f)))
	}

	return out
}
