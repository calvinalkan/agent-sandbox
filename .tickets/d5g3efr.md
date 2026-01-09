---
schema_version: 1
id: d5g3efr
status: closed
closed: 2026-01-09T06:11:46Z
blocked-by: [d5g389g, d5g35f8]
created: 2026-01-08T23:01:19Z
type: task
priority: 2
---
# E2E Tests: Git Repository and Worktree Protection

## Background & Context

The @git preset must protect hooks and config in both normal repos and worktrees.
This requires E2E tests that create real git repositories and verify protection.

## Rationale

Git worktree handling is complex (gitdir, commondir, multiple protected paths).
Unit tests can verify the parsing logic, but E2E tests prove the sandbox actually
protects the right files.

## Implementation Details

**Test setup helper:**
```go
// CreateGitRepo creates a git repo in a temp directory
func CreateGitRepo(t *testing.T) string {
    dir := t.TempDir()
    
    cmd := exec.Command("git", "init")
    cmd.Dir = dir
    if err := cmd.Run(); err != nil {
        t.Fatalf("git init failed: %v", err)
    }
    
    // Create a commit so we can create worktrees
    cmd = exec.Command("git", "commit", "--allow-empty", "-m", "init")
    cmd.Dir = dir
    cmd.Env = append(os.Environ(),
        "GIT_AUTHOR_NAME=Test",
        "GIT_AUTHOR_EMAIL=test@test.com",
        "GIT_COMMITTER_NAME=Test",
        "GIT_COMMITTER_EMAIL=test@test.com",
    )
    cmd.Run()
    
    return dir
}

// CreateWorktree creates a worktree from an existing repo
func CreateWorktree(t *testing.T, repoDir, worktreeName string) string {
    worktreeDir := filepath.Join(filepath.Dir(repoDir), worktreeName)
    
    cmd := exec.Command("git", "worktree", "add", worktreeDir, "-b", worktreeName)
    cmd.Dir = repoDir
    if err := cmd.Run(); err != nil {
        t.Fatalf("git worktree add failed: %v", err)
    }
    
    return worktreeDir
}
```

**Test cases:**
```go
func Test_Sandbox_Git_Protection(t *testing.T) {
    RequireBwrap(t)
    RequireGit(t)
    
    t.Run("normal repo: cannot modify .git/hooks", func(t *testing.T) {
        repo := CreateGitRepo(t)
        hookPath := filepath.Join(repo, ".git", "hooks", "pre-commit")
        os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 0"), 0755)
        
        _, _, exitCode := RunBinary(t, "--cwd", repo, "exec", "--", "sh", "-c",
            fmt.Sprintf("echo 'hacked' > %s", hookPath))
        
        if exitCode == 0 {
            t.Error("should have failed to write to hook")
        }
        
        // Verify hook unchanged
        content, _ := os.ReadFile(hookPath)
        if strings.Contains(string(content), "hacked") {
            t.Error("hook was modified!")
        }
    })
    
    t.Run("worktree: cannot modify main repo hooks", func(t *testing.T) {
        repo := CreateGitRepo(t)
        worktree := CreateWorktree(t, repo, "feature")
        
        mainHookPath := filepath.Join(repo, ".git", "hooks", "pre-commit")
        os.WriteFile(mainHookPath, []byte("#!/bin/sh\nexit 0"), 0755)
        
        // Run in worktree, try to modify main repo hook
        _, _, exitCode := RunBinary(t, "--cwd", worktree, "exec", "--", "sh", "-c",
            fmt.Sprintf("echo 'hacked' > %s", mainHookPath))
        
        if exitCode == 0 {
            t.Error("should have failed to write to main repo hook")
        }
    })
    
    t.Run("worktree: cannot modify worktree gitdir config", func(t *testing.T) {
        // Similar test for worktree-specific config
    })
}
```

## Files to Create
- cmd/agent-sandbox/e2e_git_test.go

## Key Invariants
- Tests use real git commands (git init, git worktree add)
- Tests verify actual file protection (try to write, verify unchanged)
- Tests clean up via t.TempDir()
- Skip if git or bwrap not available

## Acceptance Criteria

1. Test helper creates real git repo with git init
2. Test helper creates worktree with git worktree add
3. Test: cannot write to .git/hooks in normal repo
4. Test: cannot write to .git/config in normal repo
5. Test: in worktree, cannot write to main repo .git/hooks
6. Test: in worktree, cannot write to main repo .git/config
7. Test: in worktree, cannot write to worktree gitdir hooks/config
8. Tests skip gracefully if git/bwrap not available
9. All tests pass
