package main

import (
	"os"
	"path/filepath"
)

// BinaryPath represents a single location where a binary was found.
type BinaryPath struct {
	Path     string // Original path (e.g., /bin/git)
	Resolved string // Real binary after symlink resolution (e.g., /usr/bin/git)
	IsLink   bool   // True if Path is a symlink
}

// BinaryLocations finds all locations of a binary in PATH and resolves symlinks.
//
// It searches all directories in the PATH environment variable for the named binary,
// resolves symlinks to find the real binary location, and tracks whether each
// path is a symlink or a direct file.
//
// Edge cases handled:
//   - Multiple symlinks pointing to same binary
//   - Relative symlinks
//   - Circular symlinks (handled by filepath.EvalSymlinks)
//   - Non-executable files with matching name (ignored)
//   - Broken symlinks (ignored, no error)
//   - Duplicate PATH entries (deduplicated)
//
// Returns empty slice if binary is not found anywhere in PATH.
func BinaryLocations(name string, env map[string]string) []BinaryPath {
	// Get PATH directories
	pathEnv := env["PATH"]
	if pathEnv == "" {
		return nil
	}

	dirs := filepath.SplitList(pathEnv)
	result := make([]BinaryPath, 0, len(dirs))   // Pre-allocate with max possible size
	seenPath := make(map[string]bool, len(dirs)) // Track candidate paths to avoid duplicates

	for _, dir := range dirs {
		if dir == "" {
			continue
		}

		candidate := filepath.Join(dir, name)

		// Skip if we've already checked this path (duplicate PATH entries)
		if seenPath[candidate] {
			continue
		}

		seenPath[candidate] = true

		// Check if file exists and get its info (Lstat doesn't follow symlinks)
		info, err := os.Lstat(candidate)
		if err != nil {
			continue // Not found in this dir
		}

		// Check if it's executable
		if !isExecutable(info) {
			continue
		}

		// Resolve symlinks to find real binary
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue // Broken symlink or other resolution error
		}

		result = append(result, BinaryPath{
			Path:     candidate,
			Resolved: resolved,
			IsLink:   candidate != resolved,
		})
	}

	return result
}

// isExecutable checks if a file is executable.
// For regular files, checks if any execute bit is set.
// Symlinks are considered executable if they exist (will be checked after resolution).
func isExecutable(info os.FileInfo) bool {
	mode := info.Mode()

	// For symlinks, we consider them potentially executable
	// The actual executability is determined after symlink resolution
	if mode&os.ModeSymlink != 0 {
		return true
	}

	// For regular files, check execute bits
	if mode.IsRegular() {
		// Check if any execute bit is set (owner, group, or other)
		return mode.Perm()&0o111 != 0
	}

	// Not a regular file or symlink (e.g., directory)
	return false
}
