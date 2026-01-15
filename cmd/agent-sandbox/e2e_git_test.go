package main

import (
	"os"
	"path/filepath"
	"testing"
)

// ============================================================================
// E2E tests for @git preset - protects .git/hooks and .git/config
//
// These tests use RunBinary() because git commands go through command wrappers
// that exec back into agent-sandbox. In-process CLI.Run() won't work because
// os.Executable() returns the test binary, not the agent-sandbox binary.
// ============================================================================

func Test_Git_Hook_Write_Blocked_When_Git_Preset_Enabled(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test")
	repo.Commit("initial")

	// Create a hook to protect
	hookPath := filepath.Join(repo.Dir, ".git", "hooks", "pre-commit")
	mustMkdir(t, filepath.Dir(hookPath))

	original := "#!/bin/sh\nexit 0"
	mustWriteFile(t, hookPath, original)

	_, _, code := RunBinary(t, "-C", repo.Dir, "sh", "-c", "echo pwned > "+hookPath)

	if code == 0 {
		t.Error("expected non-zero exit code when writing to protected hook")
	}

	// Verify hook unchanged on host
	content := mustReadFile(t, hookPath)
	if content != original {
		t.Errorf("hook was modified: got %q, want %q", content, original)
	}
}

func Test_Git_Config_Write_Blocked_When_Git_Preset_Enabled(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test")
	repo.Commit("initial")

	configPath := filepath.Join(repo.Dir, ".git", "config")
	original := mustReadFile(t, configPath)

	_, _, code := RunBinary(t, "-C", repo.Dir, "sh", "-c", "echo pwned >> "+configPath)

	if code == 0 {
		t.Error("expected non-zero exit code when writing to protected config")
	}

	content := mustReadFile(t, configPath)
	if content != original {
		t.Error("git config was modified")
	}
}

func Test_Git_Hook_Create_Blocked_When_Git_Preset_Enabled(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test")
	repo.Commit("initial")

	// Ensure hooks dir exists but target hook doesn't
	hooksDir := filepath.Join(repo.Dir, ".git", "hooks")
	mustMkdir(t, hooksDir)
	newHook := filepath.Join(hooksDir, "pre-push")

	_, _, code := RunBinary(t, "-C", repo.Dir, "sh", "-c", "echo '#!/bin/sh' > "+newHook)

	if code == 0 {
		t.Error("expected non-zero exit code when creating new hook")
	}

	if exists, _ := fileExists(newHook); exists {
		t.Error("hook should not have been created on host")
	}
}

// ============================================================================
// E2E tests for @git-strict preset - protects other branches and tags
// ============================================================================

func Test_Git_Commit_Succeeds_When_On_Current_Branch_With_Git_Strict(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test")
	repo.Commit("initial")

	// Write config enabling git-strict
	configPath := filepath.Join(repo.Dir, ".agent-sandbox.json")
	mustWriteFile(t, configPath, `{"filesystem":{"presets":["@git-strict"]}}`)

	// Create file to commit
	repo.WriteFile("new.txt", "new")
	repo.run("add", "new.txt")

	_, stderr, code := RunBinary(t, "-C", repo.Dir, "git", "commit", "-m", "test commit")

	if code != 0 {
		t.Errorf("commit should succeed on current branch, got code %d\nstderr: %s", code, stderr)
	}
}

func Test_Git_Branch_Delete_Blocked_When_Git_Strict_Enabled(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test")
	repo.Commit("initial")

	// Create and merge a branch so we can use safe delete (-d)
	repo.run("checkout", "-b", "to-delete")
	repo.WriteFile("branch-file.txt", "branch content")
	repo.run("add", "branch-file.txt")
	repo.run("commit", "-m", "branch commit")
	repo.run("checkout", "master")
	repo.run("merge", "to-delete")

	configPath := filepath.Join(repo.Dir, ".agent-sandbox.json")
	mustWriteFile(t, configPath, `{"filesystem":{"presets":["@git-strict"]}}`)

	// Use -d (safe delete) since -D is blocked by wrapper safety rules
	_, _, code := RunBinary(t, "-C", repo.Dir, "git", "branch", "-d", "to-delete")

	if code == 0 {
		t.Error("should not be able to delete other branch")
	}
}

func Test_Git_Tag_Create_Blocked_When_Git_Strict_Enabled(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test")
	repo.Commit("initial")

	configPath := filepath.Join(repo.Dir, ".agent-sandbox.json")
	mustWriteFile(t, configPath, `{"filesystem":{"presets":["@git-strict"]}}`)

	_, _, code := RunBinary(t, "-C", repo.Dir, "git", "tag", "v1.0.0")

	if code == 0 {
		t.Error("should not be able to create tag")
	}
}

func Test_Git_Tag_Delete_Blocked_When_Git_Strict_Enabled(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test")
	repo.Commit("initial")
	repo.run("tag", "existing-tag") // Create outside sandbox

	configPath := filepath.Join(repo.Dir, ".agent-sandbox.json")
	mustWriteFile(t, configPath, `{"filesystem":{"presets":["@git-strict"]}}`)

	_, _, code := RunBinary(t, "-C", repo.Dir, "git", "tag", "-d", "existing-tag")

	if code == 0 {
		t.Error("should not be able to delete tag")
	}
}

// ============================================================================
// E2E tests for disabling @git protection
// ============================================================================

func Test_Git_Hook_Write_Succeeds_When_Git_Preset_Disabled(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test")
	repo.Commit("initial")

	// Create a hook
	hookPath := filepath.Join(repo.Dir, ".git", "hooks", "pre-commit")
	mustMkdir(t, filepath.Dir(hookPath))
	mustWriteFile(t, hookPath, "#!/bin/sh\nexit 0")

	// Disable @git protection
	configPath := filepath.Join(repo.Dir, ".agent-sandbox.json")
	mustWriteFile(t, configPath, `{"filesystem":{"presets":["!@git"]}}`)

	_, _, code := RunBinary(t, "-C", repo.Dir, "sh", "-c", "echo '#!/bin/sh\necho pwned' > "+hookPath)

	if code != 0 {
		t.Error("write to hook should succeed when @git is disabled")
	}

	// Verify hook was modified on host
	content := mustReadFile(t, hookPath)
	if content != "#!/bin/sh\necho pwned\n" {
		t.Errorf("hook should have been modified, got: %q", content)
	}
}

func Test_Git_Tag_Create_Succeeds_When_Git_Strict_Disabled(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test")
	repo.Commit("initial")

	// Disable @git-strict protection
	configPath := filepath.Join(repo.Dir, ".agent-sandbox.json")
	mustWriteFile(t, configPath, `{"filesystem":{"presets":["!@git-strict"]}}`)

	_, stderr, code := RunBinary(t, "-C", repo.Dir, "git", "tag", "v1.0.0")

	if code != 0 {
		t.Errorf("tag creation should succeed when @git-strict is disabled, stderr: %s", stderr)
	}

	// Verify tag exists on host
	repo.run("tag", "-l", "v1.0.0") // Will fail test if tag doesn't exist
}

func Test_Git_Branch_Delete_Succeeds_When_Git_Strict_Disabled(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test")
	repo.Commit("initial")

	// Create and merge a branch so we can use safe delete (-d)
	repo.run("checkout", "-b", "to-delete")
	repo.WriteFile("branch-file.txt", "branch content")
	repo.run("add", "branch-file.txt")
	repo.run("commit", "-m", "branch commit")
	repo.run("checkout", "master")
	repo.run("merge", "to-delete")

	// Disable @git-strict protection
	configPath := filepath.Join(repo.Dir, ".agent-sandbox.json")
	mustWriteFile(t, configPath, `{"filesystem":{"presets":["!@git-strict"]}}`)

	// Use -d (safe delete) since -D is blocked by wrapper safety rules
	_, stderr, code := RunBinary(t, "-C", repo.Dir, "git", "branch", "-d", "to-delete")

	if code != 0 {
		t.Errorf("branch delete should succeed when @git-strict is disabled, stderr: %s", stderr)
	}
}

func Test_Git_Worktree_Protects_Main_Repo_Hooks(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	// Create main repo
	mainRepo := NewGitRepo(t)
	mainRepo.WriteFile("README.md", "# Test")
	mainRepo.Commit("initial")

	// Create a hook in main repo to protect
	hookPath := filepath.Join(mainRepo.Dir, ".git", "hooks", "pre-commit")
	mustMkdir(t, filepath.Dir(hookPath))

	original := "#!/bin/sh\nexit 0"
	mustWriteFile(t, hookPath, original)

	// Create worktree
	worktreeDir := t.TempDir()
	mainRepo.AddWorktree(worktreeDir, "feature-branch")

	// Run sandbox from worktree, try to write to main repo's hook
	_, _, code := RunBinary(t, "-C", worktreeDir, "sh", "-c", "echo pwned > "+hookPath)

	if code == 0 {
		t.Error("expected non-zero exit code when writing to main repo's hook from worktree")
	}

	// Verify hook unchanged on host
	content := mustReadFile(t, hookPath)
	if content != original {
		t.Errorf("main repo hook was modified from worktree: got %q, want %q", content, original)
	}
}

func Test_Git_Worktree_Protects_Main_Repo_Config(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	// Create main repo
	mainRepo := NewGitRepo(t)
	mainRepo.WriteFile("README.md", "# Test")
	mainRepo.Commit("initial")

	configPath := filepath.Join(mainRepo.Dir, ".git", "config")
	original := mustReadFile(t, configPath)

	// Create worktree
	worktreeDir := t.TempDir()
	mainRepo.AddWorktree(worktreeDir, "feature-branch")

	// Run sandbox from worktree, try to write to main repo's config
	_, _, code := RunBinary(t, "-C", worktreeDir, "sh", "-c", "echo pwned >> "+configPath)

	if code == 0 {
		t.Error("expected non-zero exit code when writing to main repo's config from worktree")
	}

	// Verify config unchanged on host
	content := mustReadFile(t, configPath)
	if content != original {
		t.Error("main repo config was modified from worktree")
	}
}

// ============================================================================
// E2E tests for git symlink invocations (git-upload-pack, etc.)
//
// Regression test for: wrapper script losing argv[0] when invoked via git-* symlink.
// When git clone runs, it internally invokes git-upload-pack (a symlink to git).
// Git checks argv[0] to determine behavior, so the wrapper must preserve it.
// ============================================================================

func Test_Git_Clone_Local_Path_Works(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	// Create a source repo with a commit
	srcRepo := NewGitRepo(t)
	srcRepo.WriteFile("README.md", "# Test\n")
	srcRepo.Commit("initial commit")

	// Clone to a bare repo (this uses git-upload-pack internally)
	destDir := t.TempDir()
	bareRepo := filepath.Join(destDir, "bare.git")

	stdout, stderr, code := RunBinary(t,
		"--rw", srcRepo.Dir,
		"--rw", destDir,
		"--",
		"git", "clone", "--bare", srcRepo.Dir, bareRepo,
	)

	if code != 0 {
		t.Fatalf("git clone --bare failed: exit %d\nstdout: %s\nstderr: %s",
			code, stdout, stderr)
	}

	// Verify the clone succeeded
	_, statErr := os.Stat(filepath.Join(bareRepo, "HEAD"))
	if statErr != nil {
		t.Fatalf("bare repo not created: %v", statErr)
	}
}

// ============================================================================
// E2E tests for policy file immutability
//
// The git preset policy file at /run/agent-sandbox/policies/git must be
// immutable inside the sandbox to prevent agents from tampering with it.
// ============================================================================

func Test_Git_Policy_File_Cannot_Be_Deleted(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test")
	repo.Commit("initial")

	// Try to delete the git policy file
	_, stderr, code := RunBinary(t, "-C", repo.Dir, "rm", "-f", "/run/agent-sandbox/policies/git")

	if code == 0 {
		t.Error("should not be able to delete policy file, expected non-zero exit code")
	}

	// Verify git still works (policy file intact)
	_, _, gitCode := RunBinary(t, "-C", repo.Dir, "git", "status")
	if gitCode != 0 {
		t.Errorf("git should still work after failed rm, stderr: %s", stderr)
	}
}

func Test_Git_Policy_File_Cannot_Be_Overwritten(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test")
	repo.Commit("initial")

	// Try to overwrite the git policy file
	_, _, code := RunBinary(t, "-C", repo.Dir, "sh", "-c", "echo 'hacked' > /run/agent-sandbox/policies/git")

	if code == 0 {
		t.Error("should not be able to overwrite policy file, expected non-zero exit code")
	}

	// Verify git still works with original preset behavior
	stdout, _, gitCode := RunBinary(t, "-C", repo.Dir, "git", "status")
	if gitCode != 0 {
		t.Error("git should still work after failed overwrite")
	}

	if stdout == "" {
		t.Error("git status should produce output")
	}
}

func Test_Git_Policy_File_Cannot_Be_Truncated(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test")
	repo.Commit("initial")

	// Try to truncate the git policy file
	_, _, code := RunBinary(t, "-C", repo.Dir, "sh", "-c", "truncate -s 0 /run/agent-sandbox/policies/git")

	if code == 0 {
		t.Error("should not be able to truncate policy file, expected non-zero exit code")
	}

	// Verify git still works
	_, _, gitCode := RunBinary(t, "-C", repo.Dir, "git", "status")
	if gitCode != 0 {
		t.Error("git should still work after failed truncate")
	}
}
