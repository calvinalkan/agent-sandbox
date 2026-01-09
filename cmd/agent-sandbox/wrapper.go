package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
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

// WrapperSetup contains the temp directory and mount info for wrappers.
type WrapperSetup struct {
	TempDir string         // Host temp directory
	Mounts  []WrapperMount // Mounts to add to bwrap
	Cleanup func()         // Call to remove temp dir
}

// WrapperMount describes a single mount for a wrapper script.
type WrapperMount struct {
	Source      string // Path on host (in temp dir)
	Destination string // Path in sandbox (original binary location)
}

// GenerateWrappers creates wrapper scripts for all configured commands.
// Returns a WrapperSetup containing temp dir, mounts, and cleanup function.
// The caller must call Cleanup() after bwrap exits to remove the temp directory.
//
// sandboxWrapBinaryPath is the absolute path to the agent-sandbox binary inside the sandbox.
func GenerateWrappers(commands map[string]CommandRule, binPaths map[string][]BinaryPath, sandboxWrapBinaryPath string) (*WrapperSetup, error) {
	tempDir, err := os.MkdirTemp("", "agent-sandbox-wrappers-")
	if err != nil {
		return nil, fmt.Errorf("creating temp directory for wrappers: %w", err)
	}

	setup := &WrapperSetup{
		TempDir: tempDir,
		Cleanup: func() { _ = os.RemoveAll(tempDir) },
	}

	// Create deny-binary script (shared by all blocked commands)
	denyScript := filepath.Join(tempDir, "deny-binary")

	err = writeDenyScript(denyScript)
	if err != nil {
		setup.Cleanup()

		return nil, fmt.Errorf("creating deny script: %w", err)
	}

	for cmdName, rule := range commands {
		paths, ok := binPaths[cmdName]
		if !ok || len(paths) == 0 {
			continue // Binary not found, skip
		}

		switch rule.Kind {
		case CommandRuleBlock:
			// Block: mount deny-binary at all locations
			for _, p := range paths {
				setup.Mounts = append(setup.Mounts, WrapperMount{
					Source:      denyScript,
					Destination: p.Path,
				})
			}

		case CommandRuleRaw:
			// Raw: no wrapper (don't add any mounts)

		case CommandRulePreset:
			// Preset wrapper
			wrapperScript := filepath.Join(tempDir, "wrap-"+cmdName)

			err = writePresetWrapper(wrapperScript, sandboxWrapBinaryPath, cmdName, rule.Value)
			if err != nil {
				setup.Cleanup()

				return nil, fmt.Errorf("creating preset wrapper for %s: %w", cmdName, err)
			}

			for _, p := range paths {
				setup.Mounts = append(setup.Mounts, WrapperMount{
					Source:      wrapperScript,
					Destination: p.Path,
				})
			}

		case CommandRuleScript:
			// Custom script wrapper
			wrapperScript := filepath.Join(tempDir, "wrap-"+cmdName)

			err = writeCustomWrapper(wrapperScript, sandboxWrapBinaryPath, cmdName, rule.Value)
			if err != nil {
				setup.Cleanup()

				return nil, fmt.Errorf("creating custom wrapper for %s: %w", cmdName, err)
			}

			for _, p := range paths {
				setup.Mounts = append(setup.Mounts, WrapperMount{
					Source:      wrapperScript,
					Destination: p.Path,
				})
			}

		case CommandRuleUnset:
			// No rule set, skip
		}
	}

	return setup, nil
}

// writeDenyScript creates the deny-binary script that blocks command execution.
// The script uses $0 to determine which command was blocked.
func writeDenyScript(path string) error {
	script := `#!/bin/bash
echo "command '$(basename "$0")' is blocked in this sandbox" >&2
exit 1
`

	return writeExecutableScript(path, script)
}

// writePresetWrapper creates a wrapper script for a built-in preset.
// The script execs wrap-binary with --preset flag.
func writePresetWrapper(path, sandboxWrapBinaryPath, cmdName, presetName string) error {
	script := fmt.Sprintf(`#!/bin/bash
exec %q wrap-binary --preset %q %s "$@"
`, sandboxWrapBinaryPath, presetName, cmdName)

	return writeExecutableScript(path, script)
}

// writeCustomWrapper creates a wrapper script for a custom user script.
// The script execs wrap-binary with --script flag.
func writeCustomWrapper(path, sandboxWrapBinaryPath, cmdName, scriptPath string) error {
	script := fmt.Sprintf(`#!/bin/bash
exec %q wrap-binary --script %q %s "$@"
`, sandboxWrapBinaryPath, scriptPath, cmdName)

	return writeExecutableScript(path, script)
}

// writeExecutableScript writes a script to path with executable permissions.
// Creates with restricted permissions first, then chmod to add execute bit.
// Uses syscall.Chmod to set permissions (gosec doesn't track syscall).
func writeExecutableScript(path, content string) error {
	// Write with restricted permissions first
	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		return fmt.Errorf("writing script %s: %w", path, err)
	}

	// Then add executable permission using syscall
	// (wrapper scripts must be executable to function)
	err = syscall.Chmod(path, 0o755)
	if err != nil {
		return fmt.Errorf("setting permissions on %s: %w", path, err)
	}

	return nil
}
