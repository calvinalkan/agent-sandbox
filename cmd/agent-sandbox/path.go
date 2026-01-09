package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

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
// to find matching paths. Symlinks in results are resolved to their real paths.
//
// Returns:
//   - Non-glob patterns: returned as-is (single-element slice)
//   - Glob patterns matching files: list of resolved paths (sorted)
//   - Glob patterns matching nothing: empty slice (no error, per SPEC)
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

	// Resolve symlinks for each match
	// Per SPEC hardcoded behavior: "Symlink resolution: Paths are resolved before mounting"
	resolved := make([]string, 0, len(matches))
	for _, match := range matches {
		realPath, err := filepath.EvalSymlinks(match)
		if err != nil {
			// If symlink resolution fails (e.g., dangling symlink),
			// skip this match silently - it's similar to "path doesn't exist"
			continue
		}

		resolved = append(resolved, realPath)
	}

	return resolved, nil
}
