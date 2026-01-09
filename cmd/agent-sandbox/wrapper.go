package main

import (
	"fmt"
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

// additionalBinaryPaths returns known non-PATH locations for specific commands.
// These are paths that should also be wrapped/blocked but are not typically in PATH.
//
// For example, git is commonly found at /usr/lib/git-core/git which is not in PATH,
// but calling it directly would bypass the @git wrapper.
var additionalBinaryPaths = map[string][]string{
	"git": {
		"/usr/lib/git-core/git",
		"/usr/libexec/git-core/git", // Some distros use libexec instead of lib
	},
}

// AdditionalBinaryPaths checks known non-PATH locations for the named binary.
// Returns any additional paths that exist and are executable.
// These paths are not typically in PATH but should be wrapped/blocked.
func AdditionalBinaryPaths(name string) []BinaryPath {
	knownPaths, ok := additionalBinaryPaths[name]
	if !ok {
		return nil
	}

	result := make([]BinaryPath, 0, len(knownPaths))

	for _, candidate := range knownPaths {
		// Check if file exists and get its info (Lstat doesn't follow symlinks)
		info, err := os.Lstat(candidate)
		if err != nil {
			continue // Not found
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

// WrapperContent describes a wrapper script to be injected via FD.
type WrapperContent struct {
	Script       string   // Bash script content
	Destinations []string // Mount destinations (all binary locations)
}

// WrapperSetup contains generated wrapper content for FD-based injection.
// No temp files are created - scripts are injected via --ro-bind-data.
type WrapperSetup struct {
	Wrappers     []WrapperContent        // All wrapper scripts to inject
	RealBinaries map[string][]BinaryPath // Command name -> binary locations for mounting real binaries
}

// GenerateWrappers creates wrapper content for all configured commands.
// Returns nil if no wrappers are needed.
//
// The returned WrapperSetup contains script content that will be injected
// via bwrap's --ro-bind-data option. No temp files are created.
//
// sandboxWrapBinaryPath is the absolute path to the agent-sandbox binary inside the sandbox.
func GenerateWrappers(commands map[string]CommandRule, binPaths map[string][]BinaryPath, sandboxWrapBinaryPath string) *WrapperSetup {
	setup := &WrapperSetup{
		RealBinaries: make(map[string][]BinaryPath),
	}

	// Collect destinations for deny script (all blocked commands share one script)
	var denyDestinations []string

	for cmdName, rule := range commands {
		paths, ok := binPaths[cmdName]
		if !ok || len(paths) == 0 {
			continue // Binary not found, skip
		}

		switch rule.Kind {
		case CommandRuleBlock:
			// Block: collect destinations for shared deny script
			for _, p := range paths {
				denyDestinations = append(denyDestinations, p.Path)
			}

		case CommandRuleRaw:
			// Raw: no wrapper (don't add any mounts)

		case CommandRulePreset:
			// Preset wrapper
			script := generatePresetWrapper(sandboxWrapBinaryPath, cmdName, rule.Value)
			destinations := make([]string, 0, len(paths))

			for _, p := range paths {
				destinations = append(destinations, p.Path)
			}

			setup.Wrappers = append(setup.Wrappers, WrapperContent{
				Script:       script,
				Destinations: destinations,
			})

			// Track real binary locations for mounting
			setup.RealBinaries[cmdName] = paths

		case CommandRuleScript:
			// Custom script wrapper
			script := generateCustomWrapper(sandboxWrapBinaryPath, cmdName, rule.Value)
			destinations := make([]string, 0, len(paths))

			for _, p := range paths {
				destinations = append(destinations, p.Path)
			}

			setup.Wrappers = append(setup.Wrappers, WrapperContent{
				Script:       script,
				Destinations: destinations,
			})

			// Track real binary locations for mounting
			setup.RealBinaries[cmdName] = paths

		case CommandRuleUnset:
			// No rule set, skip
		}
	}

	// Add deny script if any commands are blocked
	if len(denyDestinations) > 0 {
		setup.Wrappers = append(setup.Wrappers, WrapperContent{
			Script:       generateDenyScript(),
			Destinations: denyDestinations,
		})
	}

	// Return nil if no wrappers needed
	if len(setup.Wrappers) == 0 && len(setup.RealBinaries) == 0 {
		return nil
	}

	return setup
}

// generateDenyScript creates the deny-binary script content.
// The script uses $0 to determine which command was blocked.
func generateDenyScript() string {
	return `#!/bin/bash
echo "command '$(basename "$0")' is blocked in this sandbox" >&2
exit 1
`
}

// generatePresetWrapper creates a wrapper script for a built-in preset.
// The script execs wrap-binary with --preset flag.
func generatePresetWrapper(sandboxWrapBinaryPath, cmdName, presetName string) string {
	return fmt.Sprintf(`#!/bin/bash
exec %q wrap-binary --preset %q %s "$@"
`, sandboxWrapBinaryPath, presetName, cmdName)
}

// generateCustomWrapper creates a wrapper script for a custom user script.
// The script execs wrap-binary with --script flag.
func generateCustomWrapper(sandboxWrapBinaryPath, cmdName, scriptPath string) string {
	return fmt.Sprintf(`#!/bin/bash
exec %q wrap-binary --script %q %s "$@"
`, sandboxWrapBinaryPath, scriptPath, cmdName)
}
