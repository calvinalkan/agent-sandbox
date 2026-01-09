package main

import (
	"errors"
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
