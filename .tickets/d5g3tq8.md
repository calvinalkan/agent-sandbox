---
schema_version: 1
id: d5g3tq8
status: closed
closed: 2026-01-09T06:08:05Z
blocked-by: [d5g3tgg, d5g389g]
created: 2026-01-08T23:27:25Z
type: task
priority: 1
---
# E2E Tests: Exclude Path Behavior (Directories and Files)

## Background & Context

Exclude paths have specific behavior that must be verified with real bwrap:
- Directories: empty but exist, contents return ENOENT
- Files: exist but return EACCES on read

This needs extensive E2E testing with various scenarios.

## Test Scenarios

### Directory Exclusion Tests

```go
func Test_Exclude_Directory(t *testing.T) {
    tests := []struct {
        name    string
        setup   func(t *testing.T, dir string)
        exclude string
        checks  []check
    }{
        {
            name: "excluded dir exists but is empty",
            setup: func(t *testing.T, dir string) {
                os.MkdirAll(filepath.Join(dir, "secrets"), 0755)
                os.WriteFile(filepath.Join(dir, "secrets", "key.txt"), []byte("SECRET"), 0644)
            },
            exclude: "secrets",
            checks: []check{
                {cmd: "test -d secrets", wantExit: 0},           // dir exists
                {cmd: "ls secrets", wantStdout: ""},              // but empty
                {cmd: "cat secrets/key.txt", wantExit: 1, wantStderr: "No such file"},
            },
        },
        {
            name: "excluded nested dir",
            setup: func(t *testing.T, dir string) {
                os.MkdirAll(filepath.Join(dir, "config", "secrets"), 0755)
                os.WriteFile(filepath.Join(dir, "config", "secrets", "api.key"), []byte("KEY"), 0644)
                os.WriteFile(filepath.Join(dir, "config", "settings.json"), []byte("{}"), 0644)
            },
            exclude: "config/secrets",
            checks: []check{
                {cmd: "cat config/settings.json", wantExit: 0},   // sibling accessible
                {cmd: "test -d config/secrets", wantExit: 0},     // excluded dir exists
                {cmd: "ls config/secrets", wantStdout: ""},       // but empty
                {cmd: "cat config/secrets/api.key", wantExit: 1}, // contents ENOENT
            },
        },
        {
            name: "excluded dir with subdirectories",
            setup: func(t *testing.T, dir string) {
                os.MkdirAll(filepath.Join(dir, "secrets", "aws"), 0755)
                os.MkdirAll(filepath.Join(dir, "secrets", "ssh"), 0755)
                os.WriteFile(filepath.Join(dir, "secrets", "aws", "creds"), []byte("x"), 0644)
                os.WriteFile(filepath.Join(dir, "secrets", "ssh", "id_rsa"), []byte("x"), 0644)
            },
            exclude: "secrets",
            checks: []check{
                {cmd: "test -d secrets", wantExit: 0},
                {cmd: "ls secrets", wantStdout: ""},
                {cmd: "test -d secrets/aws", wantExit: 1},        // subdir gone
                {cmd: "cat secrets/ssh/id_rsa", wantExit: 1},
            },
        },
    }
}
```

### File Exclusion Tests

```go
func Test_Exclude_File(t *testing.T) {
    tests := []struct {
        name    string
        setup   func(t *testing.T, dir string)
        exclude string
        checks  []check
    }{
        {
            name: "excluded file exists but unreadable",
            setup: func(t *testing.T, dir string) {
                os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=x"), 0644)
            },
            exclude: ".env",
            checks: []check{
                {cmd: "test -e .env", wantExit: 0},               // exists
                {cmd: "test -f .env", wantExit: 0},               // is regular file
                {cmd: "cat .env", wantExit: 1, wantStderr: "Permission denied"},
                {cmd: "ls -la .env", wantStdout: "----------"},   // mode 000
            },
        },
        {
            name: "excluded file sibling accessible",
            setup: func(t *testing.T, dir string) {
                os.WriteFile(filepath.Join(dir, ".env"), []byte("SECRET=x"), 0644)
                os.WriteFile(filepath.Join(dir, ".env.example"), []byte("KEY="), 0644)
            },
            exclude: ".env",
            checks: []check{
                {cmd: "cat .env", wantExit: 1, wantStderr: "Permission denied"},
                {cmd: "cat .env.example", wantExit: 0, wantStdout: "KEY="},
            },
        },
        {
            name: "excluded file in subdirectory",
            setup: func(t *testing.T, dir string) {
                os.MkdirAll(filepath.Join(dir, "config"), 0755)
                os.WriteFile(filepath.Join(dir, "config", "secrets.json"), []byte(`{"key":"x"}`), 0644)
                os.WriteFile(filepath.Join(dir, "config", "settings.json"), []byte(`{}`), 0644)
            },
            exclude: "config/secrets.json",
            checks: []check{
                {cmd: "cat config/secrets.json", wantExit: 1, wantStderr: "Permission denied"},
                {cmd: "cat config/settings.json", wantExit: 0},
            },
        },
    }
}
```

### Language-Specific Tests

```go
func Test_Exclude_Node(t *testing.T) {
    // Skip if node isn't available on this machine/CI.
    // (Exclude semantics are already covered by shell-level tests.)
    // Test with Node.js fs module
    // - fs.existsSync() 
    // - fs.readFileSync()
    // - fs.readdirSync()
}

func Test_Exclude_Python(t *testing.T) {
    // Skip if python isn't available on this machine/CI.
    // Test with Python
    // - os.path.exists()
    // - open().read()
    // - os.listdir()
}
```

### Edge Cases

```go
func Test_Exclude_EdgeCases(t *testing.T) {
    tests := []struct{...}{
        {"symlink to excluded dir", ...},
        {"symlink to excluded file", ...},
        {"exclude path that doesn't exist", ...},
        {"exclude with glob pattern", ...},
        {"multiple excludes in same parent", ...},
    }
}
```

## Files to Create
- cmd/agent-sandbox/e2e_exclude_test.go

## Key Invariants
- All tests use real bwrap (not mocked)
- Tests create real files/dirs in t.TempDir()
- Tests verify both shell commands and language runtimes
- Tests clean up automatically

## Acceptance Criteria

**Directory exclusion:**
1. Excluded dir exists (test -d returns 0)
2. Excluded dir is empty (ls returns nothing)
3. Files inside return ENOENT
4. Subdirs inside don't exist
5. Sibling dirs unaffected

**File exclusion:**
6. Excluded file exists (test -e returns 0)
7. Excluded file is regular file (test -f returns 0)
8. Excluded file has mode 000
9. Read returns EACCES "Permission denied"
10. Sibling files unaffected

**Language tests:**
11. Node.js: existsSync, readFileSync, readdirSync
12. Python: os.path.exists, open().read(), os.listdir

**Edge cases:**
13. Symlinks to excluded paths
14. Non-existent exclude paths (no error)
15. Multiple excludes in same directory
16. Glob patterns that match excludes

**Minimum 20 test cases total**
