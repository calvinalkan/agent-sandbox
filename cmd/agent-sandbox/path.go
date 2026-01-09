package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PathSource identifies where a path rule originated.
type PathSource string

const (
	// PathSourcePreset indicates the path came from a built-in preset.
	PathSourcePreset PathSource = "preset"
	// PathSourceGlobal indicates the path came from global config.
	PathSourceGlobal PathSource = "global"
	// PathSourceProject indicates the path came from project config.
	PathSourceProject PathSource = "project"
	// PathSourceCLI indicates the path came from CLI flags.
	PathSourceCLI PathSource = "cli"
)

// PathAccess represents the access level for a resolved path.
type PathAccess string

const (
	// PathAccessRo indicates read-only access.
	PathAccessRo PathAccess = "ro"
	// PathAccessRw indicates read-write access.
	PathAccessRw PathAccess = "rw"
	// PathAccessExclude indicates the path is hidden/excluded.
	PathAccessExclude PathAccess = "exclude"
)

// ResolvedPath represents a path ready for bwrap mounting.
type ResolvedPath struct {
	Original string     // Original pattern from config (e.g., "~/code/*")
	Resolved string     // Absolute, symlink-resolved path
	Access   PathAccess // "ro", "rw", or "exclude"
	Source   PathSource // "preset", "global", "project", "cli"
}

// PathLayerInput holds paths from a single config layer.
type PathLayerInput struct {
	Ro      []string
	Rw      []string
	Exclude []string
}

// ErrEmptyPathPattern is returned when an empty path pattern is provided.
var ErrEmptyPathPattern = errors.New("empty path pattern")

// ResolvePath converts a path pattern to an absolute path.
//
// Resolution rules:
//   - ~ at start expands to homeDir
//   - Absolute paths (starting with /) resolve as-is
//   - Relative paths resolve against workDir
//   - Resulting paths are always cleaned (no .., .)
//   - Environment variables ($HOME, $USER, etc.) are NOT expanded (treated as literal)
func ResolvePath(pattern, homeDir, workDir string) (string, error) {
	if pattern == "" {
		return "", ErrEmptyPathPattern
	}

	var resolved string

	switch {
	case pattern == "~":
		// Lone tilde expands to home directory
		resolved = homeDir
	case strings.HasPrefix(pattern, "~/"):
		// Home directory prefix
		resolved = filepath.Join(homeDir, pattern[2:])
	case filepath.IsAbs(pattern):
		// Absolute path - use as-is
		resolved = pattern
	default:
		// Relative path - resolve against workDir
		resolved = filepath.Join(workDir, pattern)
	}

	// Clean the path (removes .., ., trailing slashes, etc.)
	resolved = filepath.Clean(resolved)

	return resolved, nil
}

// ExpandGlob expands a path pattern with wildcards to matching files.
// If the pattern contains glob metacharacters (*, ?, []), it uses filepath.Glob
// to find matching paths. NOTE: This function does NOT resolve symlinks -
// that is done by the full resolution pipeline (resolveOnePath).
//
// Returns:
//   - Non-glob patterns: returned as-is (single-element slice)
//   - Glob patterns matching files: list of matching paths (sorted)
//   - Glob patterns matching nothing: nil slice (no error, per SPEC)
//   - Invalid glob patterns (e.g., malformed brackets): error
func ExpandGlob(pattern string) ([]string, error) {
	// Check if pattern contains glob metacharacters
	if !strings.ContainsAny(pattern, "*?[") {
		// Not a glob - return as-is (existence checked separately)
		return []string{pattern}, nil
	}

	// Expand the glob pattern
	matches, err := filepath.Glob(pattern)
	if err != nil {
		// Invalid glob pattern (e.g., malformed bracket expression like "[")
		return nil, fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
	}

	// Empty matches = pattern is valid but matches nothing
	// Per SPEC: "Glob matches nothing → Skip silently"
	if len(matches) == 0 {
		return nil, nil
	}

	return matches, nil
}

// ResolvePathsInput holds all path sources for the full resolution pipeline.
type ResolvePathsInput struct {
	Preset  PathLayerInput
	Global  PathLayerInput
	Project PathLayerInput
	CLI     PathLayerInput
	HomeDir string
	WorkDir string
}

// ResolvePaths processes all paths from config/presets into mount-ready paths.
// It applies the full pipeline: expand ~ and relative → glob expansion →
// existence check → symlink resolution.
//
// Per SPEC error conditions:
//   - Path doesn't exist (non-glob) → Skip silently
//   - Glob matches nothing → Skip silently
//   - Permission errors → Returned as errors
//   - Invalid glob patterns → Returned as errors
func ResolvePaths(input *ResolvePathsInput) ([]ResolvedPath, error) {
	var result []ResolvedPath

	// Process each layer in order (later layers override earlier for same paths)
	layers := []struct {
		paths  PathLayerInput
		source PathSource
	}{
		{input.Preset, PathSourcePreset},
		{input.Global, PathSourceGlobal},
		{input.Project, PathSourceProject},
		{input.CLI, PathSourceCLI},
	}

	for _, layer := range layers {
		// Process each access level within the layer
		accessLevels := []struct {
			paths  []string
			access PathAccess
		}{
			{layer.paths.Ro, PathAccessRo},
			{layer.paths.Rw, PathAccessRw},
			{layer.paths.Exclude, PathAccessExclude},
		}

		for _, al := range accessLevels {
			for _, pattern := range al.paths {
				resolved, err := resolveOnePath(pattern, al.access, layer.source, input.HomeDir, input.WorkDir)
				if err != nil {
					return nil, err
				}

				result = append(result, resolved...)
			}
		}
	}

	return result, nil
}

// resolveOnePath applies the full path resolution pipeline to a single pattern:
// 1. Expand ~ and relative paths to absolute
// 2. Expand glob patterns to matching files
// 3. Check existence (skip non-existent silently)
// 4. Resolve symlinks to real paths.
func resolveOnePath(pattern string, access PathAccess, source PathSource, homeDir, workDir string) ([]ResolvedPath, error) {
	// Step 1: Expand ~ and resolve relative paths
	expanded, err := ResolvePath(pattern, homeDir, workDir)
	if err != nil {
		return nil, fmt.Errorf("resolving pattern %q: %w", pattern, err)
	}

	// Step 2: Expand globs
	paths, err := ExpandGlob(expanded)
	if err != nil {
		return nil, err // Invalid glob patterns are real errors
	}

	// If glob matched nothing, return empty (per SPEC: skip silently)
	if len(paths) == 0 {
		return nil, nil
	}

	// Step 3 & 4: Check existence and resolve symlinks for each path
	result := make([]ResolvedPath, 0, len(paths))

	for _, path := range paths {
		// Check if path exists using Lstat (doesn't follow symlinks for existence check)
		_, err := os.Lstat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// Per SPEC: "Path doesn't exist (non-glob) → Skip silently"
				continue
			}
			// Permission denied and other errors are real errors
			return nil, fmt.Errorf("checking path %q: %w", path, err)
		}

		// Resolve symlinks to get real path
		// Per SPEC hardcoded behavior: "Symlink resolution: Paths are resolved before mounting"
		realPath, err := filepath.EvalSymlinks(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// Dangling symlink - skip silently (target doesn't exist)
				continue
			}
			// Other errors (permission denied on symlink target, etc.) are real errors
			return nil, fmt.Errorf("resolving symlinks for %q: %w", path, err)
		}

		result = append(result, ResolvedPath{
			Original: pattern,
			Resolved: realPath,
			Access:   access,
			Source:   source,
		})
	}

	return result, nil
}
