---
schema_version: 1
id: d5g3tgg
status: closed
closed: 2026-01-09T05:08:36Z
blocked-by: [d5g36gr]
created: 2026-01-08T23:26:58Z
type: task
priority: 1
---
# bwrap: Exclude Path Implementation (Directories and Files)

## Background & Context

Excluded paths should be inaccessible inside the sandbox. Based on bwrap testing,
we have two approaches depending on whether the target is a directory or file:

- **Directories:** `--tmpfs /path` - creates empty dir, contents return ENOENT
- **Files:** `--ro-bind /tmp/empty-000 /path` - file exists but returns EACCES

## Implementation Details

**Temp directory setup (created once before bwrap):**
```go
func setupTempDir() (string, func(), error) {
    dir, err := os.MkdirTemp("", "agent-sandbox-")
    if err != nil {
        return "", nil, err
    }
    
    // Create empty unreadable file for file exclusions
    emptyFile := filepath.Join(dir, "empty-unreadable")
    if err := os.WriteFile(emptyFile, nil, 0000); err != nil {
        os.RemoveAll(dir)
        return "", nil, err
    }
    
    cleanup := func() { os.RemoveAll(dir) }
    return dir, cleanup, nil
}
```

**Generating exclude mounts:**
```go
func generateExcludeMounts(excludePaths []PathEntry, tempDir string) []string {
    var args []string
    emptyFile := filepath.Join(tempDir, "empty-unreadable")
    
    for _, p := range excludePaths {
        info, err := os.Stat(p.Resolved)
        if err != nil {
            continue // path doesn't exist, skip
        }
        
        if info.IsDir() {
            // Directory: use tmpfs (empty dir, contents ENOENT)
            args = append(args, "--tmpfs", p.Resolved)
        } else {
            // File: bind unreadable file (exists, EACCES on read)
            args = append(args, "--ro-bind", emptyFile, p.Resolved)
        }
    }
    
    return args
}
```

**Behavior summary:**

| Target | bwrap args | exists? | isFile/isDir? | read/list error |
|--------|-----------|---------|---------------|-----------------|
| Directory | `--tmpfs /dir` | true | isDir=true | ENOENT (empty) |
| File | `--ro-bind /empty-000 /file` | true | isFile=true | EACCES |

## Files to Modify
- cmd/agent-sandbox/bwrap.go - add generateExcludeMounts function
- cmd/agent-sandbox/bwrap_test.go - unit tests

## Key Invariants
- Temp directory created before bwrap, cleaned up after
- Empty file has mode 000 (no permissions)
- Directory exclusion uses --tmpfs
- File exclusion uses --ro-bind with empty-000 file
- Both approaches give clear, standard errors

## Acceptance Criteria

1. Temp directory created with empty mode-000 file
2. Directory exclusion generates --tmpfs argument
3. File exclusion generates --ro-bind with empty-000 file
4. Cleanup function removes temp directory
5. Non-existent exclude paths are skipped (no error)

**Unit tests (in bwrap_test.go):**
6. generateExcludeMounts() returns --tmpfs for directories
7. generateExcludeMounts() returns --ro-bind for files
8. setupTempDir() creates mode-000 file

**E2E tests (deferred to d5g3tq8):**
- Actual ENOENT behavior for excluded directories
- Actual EACCES behavior for excluded files
- 18 comprehensive test scenarios
