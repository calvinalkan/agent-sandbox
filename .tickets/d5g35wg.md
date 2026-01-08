---
schema_version: 1
id: d5g35wg
status: open
blocked-by: []
created: 2026-01-08T22:42:58Z
type: task
priority: 1
---
# Path Resolution: Home Directory and Relative Path Expansion

## Background & Context

Per SPEC.md Path Patterns section:

> **Path resolution:**
> - ~ at start expands to home directory
> - Absolute paths (/, ~) resolve as-is
> - Relative paths resolve relative to effective pwd (respects --cwd)

All paths in config and from presets must be resolved to absolute paths before
being passed to bwrap.

## Rationale

Path resolution is a foundational piece of the sandbox:
1. Config files use ~, relative paths, and absolute paths
2. Presets return paths like "~/.ssh" and ".git/hooks"
3. bwrap requires absolute paths

This must happen AFTER preset expansion but BEFORE specificity resolution.

## Implementation Details

```go
// ResolvePath converts a path pattern to an absolute path
func ResolvePath(pattern string, homeDir, workDir string) (string, error) {
    // Handle empty pattern
    if pattern == "" {
        return "", errors.New("empty path pattern")
    }
    
    var resolved string
    
    if strings.HasPrefix(pattern, "~/") {
        // Home directory prefix
        resolved = filepath.Join(homeDir, pattern[2:])
    } else if pattern == "~" {
        resolved = homeDir
    } else if filepath.IsAbs(pattern) {
        // Absolute path
        resolved = pattern
    } else {
        // Relative path - resolve against workDir
        resolved = filepath.Join(workDir, pattern)
    }
    
    // Clean the path (removes .., ., etc.)
    resolved = filepath.Clean(resolved)
    
    return resolved, nil
}
```

**Environment Variables:** Per SPEC:
> Environment variables: Not expanded. $HOME, $PWD etc. are treated as literal strings.

So we do NOT expand $HOME or other env vars - only ~.

## Files to Create
- cmd/agent-sandbox/path.go - path resolution functions
- cmd/agent-sandbox/path_test.go - tests

## Key Invariants
- ~ expands to home directory (only at start of path)
- Relative paths resolve against workDir (not os.Getwd())
- $HOME, $USER etc. are NOT expanded (treated as literal)
- Resulting paths are always absolute
- Resulting paths are always cleaned (no .., .)

## Acceptance Criteria

1. ~/foo expands to /home/user/foo
2. ~ alone expands to /home/user
3. ./foo resolves to workDir/foo
4. foo resolves to workDir/foo
5. /absolute/path stays as /absolute/path
6. $HOME/foo is treated as a literal relative path (no env expansion), i.e. resolves to workDir/$HOME/foo
7. Paths are cleaned (foo/../bar becomes bar)
8. Empty pattern returns error
9. Tests cover all expansion scenarios
