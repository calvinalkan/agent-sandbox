package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testHookContent is the content for test hook files.
const testHookContent = "#!/bin/sh\nexit 0\n"

// ============================================================================
// E2E tests for Git repository and worktree protection
// These tests verify that the @git preset properly protects git hooks and config
// in both normal repositories and worktrees.
// ============================================================================

// Test_Sandbox_Git_Cannot_Write_To_Hooks_In_Normal_Repo verifies that
// .git/hooks cannot be modified inside the sandbox for a normal git repository.
func Test_Sandbox_Git_Cannot_Write_To_Hooks_In_Normal_Repo(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	// Create a real git repo
	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test Repo\n")
	repo.Commit("initial commit")

	// Create a hook file to protect
	hookPath := filepath.Join(repo.Dir, ".git", "hooks", "pre-commit")

	err := os.MkdirAll(filepath.Dir(hookPath), 0o750)
	if err != nil {
		t.Fatalf("failed to create hooks dir: %v", err)
	}

	originalContent := testHookContent

	err = os.WriteFile(hookPath, []byte(originalContent), 0o755)
	if err != nil {
		t.Fatalf("failed to create hook file: %v", err)
	}

	// Create CLI tester pointing to the repo
	c := NewCLITesterAt(t, repo.Dir)
	c.Env["HOME"] = filepath.Dir(repo.Dir) // Parent dir as home

	// Try to modify the hook inside the sandbox
	_, _, code := c.Run("bash", "-c", "echo 'hacked' > "+hookPath)

	// Should fail because .git/hooks is read-only
	if code == 0 {
		t.Error("expected non-zero exit code when writing to .git/hooks")
	}

	// Verify the hook was not modified
	content, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}

	if strings.Contains(string(content), "hacked") {
		t.Error(".git/hooks/pre-commit was modified despite protection")
	}

	if string(content) != originalContent {
		t.Errorf("hook content changed, expected %q, got %q", originalContent, string(content))
	}
}

// Test_Sandbox_Git_Cannot_Write_To_Config_In_Normal_Repo verifies that
// .git/config cannot be modified inside the sandbox for a normal git repository.
func Test_Sandbox_Git_Cannot_Write_To_Config_In_Normal_Repo(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	// Create a real git repo
	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test Repo\n")
	repo.Commit("initial commit")

	// Get the config file path
	configPath := filepath.Join(repo.Dir, ".git", "config")

	// Read original config
	originalContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	// Create CLI tester pointing to the repo
	c := NewCLITesterAt(t, repo.Dir)
	c.Env["HOME"] = filepath.Dir(repo.Dir) // Parent dir as home

	// Try to modify the config inside the sandbox
	_, _, code := c.Run("bash", "-c", "echo 'hacked = true' >> "+configPath)

	// Should fail because .git/config is read-only
	if code == 0 {
		t.Error("expected non-zero exit code when writing to .git/config")
	}

	// Verify the config was not modified
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	if strings.Contains(string(content), "hacked") {
		t.Error(".git/config was modified despite protection")
	}

	if !bytes.Equal(content, originalContent) {
		t.Errorf("config content changed unexpectedly")
	}
}

// Test_Sandbox_Git_Cannot_Create_New_Hook_In_Normal_Repo verifies that
// new hooks cannot be created in .git/hooks inside the sandbox.
func Test_Sandbox_Git_Cannot_Create_New_Hook_In_Normal_Repo(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	// Create a real git repo
	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test Repo\n")
	repo.Commit("initial commit")

	// Ensure hooks directory exists
	hooksDir := filepath.Join(repo.Dir, ".git", "hooks")

	err := os.MkdirAll(hooksDir, 0o750)
	if err != nil {
		t.Fatalf("failed to create hooks dir: %v", err)
	}

	newHookPath := filepath.Join(hooksDir, "post-commit")

	// Create CLI tester pointing to the repo
	c := NewCLITesterAt(t, repo.Dir)
	c.Env["HOME"] = filepath.Dir(repo.Dir) // Parent dir as home

	// Try to create a new hook inside the sandbox
	_, _, code := c.Run("bash", "-c", "echo '#!/bin/sh\necho malicious' > "+newHookPath)

	// Should fail because .git/hooks is read-only
	if code == 0 {
		// Check if the file was actually created
		_, statErr := os.Stat(newHookPath)
		if statErr == nil {
			t.Error("new hook was created despite protection")
		}
	}

	// Verify the hook was not created
	_, statErr := os.Stat(newHookPath)
	if statErr == nil {
		content, _ := os.ReadFile(newHookPath)
		t.Errorf("hook file should not exist, but contains: %s", string(content))
	}
}

// Test_Sandbox_Git_Worktree_Cannot_Write_To_Main_Repo_Hooks verifies that
// when running in a worktree, the main repository's hooks cannot be modified.
func Test_Sandbox_Git_Worktree_Cannot_Write_To_Main_Repo_Hooks(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	// Create main repo
	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Main Repo\n")
	repo.Commit("initial commit")

	// Create a hook in the main repo
	mainHookPath := filepath.Join(repo.Dir, ".git", "hooks", "pre-commit")

	err := os.MkdirAll(filepath.Dir(mainHookPath), 0o750)
	if err != nil {
		t.Fatalf("failed to create hooks dir: %v", err)
	}

	originalContent := testHookContent

	err = os.WriteFile(mainHookPath, []byte(originalContent), 0o755)
	if err != nil {
		t.Fatalf("failed to create hook file: %v", err)
	}

	// Create a worktree
	worktreeDir := filepath.Join(filepath.Dir(repo.Dir), "feature-worktree")
	repo.AddWorktree(worktreeDir, "feature")

	// Create CLI tester pointing to the worktree
	c := NewCLITesterAt(t, worktreeDir)
	c.Env["HOME"] = filepath.Dir(repo.Dir) // Parent dir as home

	// Try to modify the main repo's hook from inside the worktree sandbox
	_, _, code := c.Run("bash", "-c", "echo 'hacked from worktree' > "+mainHookPath)

	// Should fail because main repo's .git/hooks is read-only
	if code == 0 {
		t.Error("expected non-zero exit code when writing to main repo hooks from worktree")
	}

	// Verify the hook was not modified
	content, err := os.ReadFile(mainHookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}

	if strings.Contains(string(content), "hacked") {
		t.Error("main repo hook was modified from worktree despite protection")
	}

	if string(content) != originalContent {
		t.Errorf("hook content changed, expected %q, got %q", originalContent, string(content))
	}
}

// Test_Sandbox_Git_Worktree_Cannot_Write_To_Main_Repo_Config verifies that
// when running in a worktree, the main repository's config cannot be modified.
func Test_Sandbox_Git_Worktree_Cannot_Write_To_Main_Repo_Config(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	// Create main repo
	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Main Repo\n")
	repo.Commit("initial commit")

	// Get the main repo config file path
	mainConfigPath := filepath.Join(repo.Dir, ".git", "config")

	// Read original config
	originalContent, err := os.ReadFile(mainConfigPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	// Create a worktree
	worktreeDir := filepath.Join(filepath.Dir(repo.Dir), "feature-worktree")
	repo.AddWorktree(worktreeDir, "feature")

	// Create CLI tester pointing to the worktree
	c := NewCLITesterAt(t, worktreeDir)
	c.Env["HOME"] = filepath.Dir(repo.Dir) // Parent dir as home

	// Try to modify the main repo's config from inside the worktree sandbox
	_, _, code := c.Run("bash", "-c", "echo '[hacked]' >> "+mainConfigPath)

	// Should fail because main repo's .git/config is read-only
	if code == 0 {
		t.Error("expected non-zero exit code when writing to main repo config from worktree")
	}

	// Verify the config was not modified
	content, err := os.ReadFile(mainConfigPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	if strings.Contains(string(content), "hacked") {
		t.Error("main repo config was modified from worktree despite protection")
	}

	if !bytes.Equal(content, originalContent) {
		t.Errorf("config content changed unexpectedly")
	}
}

// Test_Sandbox_Git_Worktree_Cannot_Write_To_Worktree_Gitdir_Hooks verifies that
// the worktree-specific gitdir hooks cannot be modified.
func Test_Sandbox_Git_Worktree_Cannot_Write_To_Worktree_Gitdir_Hooks(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	// Create main repo
	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Main Repo\n")
	repo.Commit("initial commit")

	// Create a worktree
	worktreeDir := filepath.Join(filepath.Dir(repo.Dir), "feature-worktree")
	repo.AddWorktree(worktreeDir, "feature")

	// Find the worktree-specific gitdir by reading the .git file
	// The worktree gitdir is at .git/worktrees/<worktree-dir-basename>/
	worktreeGitDir := filepath.Join(repo.Dir, ".git", "worktrees", filepath.Base(worktreeDir))

	// Create a hook in the worktree gitdir
	hookPath := filepath.Join(worktreeGitDir, "hooks", "pre-commit")

	err := os.MkdirAll(filepath.Dir(hookPath), 0o750)
	if err != nil {
		t.Fatalf("failed to create hooks dir: %v", err)
	}

	originalContent := testHookContent

	err = os.WriteFile(hookPath, []byte(originalContent), 0o755)
	if err != nil {
		t.Fatalf("failed to create hook file: %v", err)
	}

	// Create CLI tester pointing to the worktree
	c := NewCLITesterAt(t, worktreeDir)
	c.Env["HOME"] = filepath.Dir(repo.Dir) // Parent dir as home

	// Try to modify the worktree gitdir hook
	_, _, code := c.Run("bash", "-c", "echo 'hacked worktree hook' > "+hookPath)

	// Should fail because worktree gitdir hooks are read-only
	if code == 0 {
		t.Error("expected non-zero exit code when writing to worktree gitdir hooks")
	}

	// Verify the hook was not modified
	content, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("failed to read hook file: %v", err)
	}

	if strings.Contains(string(content), "hacked") {
		t.Error("worktree gitdir hook was modified despite protection")
	}

	if string(content) != originalContent {
		t.Errorf("hook content changed, expected %q, got %q", originalContent, string(content))
	}
}

// Test_Sandbox_Git_Worktree_Cannot_Write_To_Worktree_Gitdir_Config verifies that
// the worktree-specific config cannot be modified.
func Test_Sandbox_Git_Worktree_Cannot_Write_To_Worktree_Gitdir_Config(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	// Create main repo
	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Main Repo\n")
	repo.Commit("initial commit")

	// Create a worktree
	worktreeDir := filepath.Join(filepath.Dir(repo.Dir), "feature-worktree")
	repo.AddWorktree(worktreeDir, "feature")

	// Find the worktree-specific gitdir
	worktreeGitDir := filepath.Join(repo.Dir, ".git", "worktrees", filepath.Base(worktreeDir))
	worktreeConfigPath := filepath.Join(worktreeGitDir, "config")

	// Create the worktree config file (the preset protects .../config)
	originalContent := "[core]\n\tworktree = true\n"

	err := os.WriteFile(worktreeConfigPath, []byte(originalContent), 0o644)
	if err != nil {
		t.Fatalf("failed to create worktree config: %v", err)
	}

	// Create CLI tester pointing to the worktree
	c := NewCLITesterAt(t, worktreeDir)
	c.Env["HOME"] = filepath.Dir(repo.Dir) // Parent dir as home

	// Try to modify the worktree config
	_, _, code := c.Run("bash", "-c", "echo '[hacked]' >> "+worktreeConfigPath)

	// Should fail because worktree config is read-only
	if code == 0 {
		t.Error("expected non-zero exit code when writing to worktree config")
	}

	// Verify the config was not modified
	content, err := os.ReadFile(worktreeConfigPath)
	if err != nil {
		t.Fatalf("failed to read worktree config file: %v", err)
	}

	if strings.Contains(string(content), "hacked") {
		t.Error("worktree config was modified despite protection")
	}

	if string(content) != originalContent {
		t.Errorf("config content changed unexpectedly")
	}
}

// Test_Sandbox_Git_Protection_Skips_Non_Git_Directory verifies that
// the sandbox works correctly in directories that are not git repositories.
// When no .git exists, @git preset has no effect and sandbox works normally.
func Test_Sandbox_Git_Protection_Skips_Non_Git_Directory(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Write a file in TMPDIR (always writable)
	// Note: When HOME==WorkDir, workdir becomes read-only due to specificity rules
	testFile := c.TempFile("test.txt")
	stdout, stderr, code := c.Run("bash", "-c", "echo 'hello' > "+testFile)

	if code != 0 {
		t.Fatalf("expected exit code 0 in non-git dir, got %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	// Verify the file was created
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
	}

	if !strings.Contains(string(content), "hello") {
		t.Errorf("expected 'hello' in test file, got: %s", string(content))
	}
}

// Test_Sandbox_Git_Can_Still_Read_Hooks verifies that hooks can be read
// even though they cannot be written.
func Test_Sandbox_Git_Can_Still_Read_Hooks(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	// Create a real git repo
	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test Repo\n")
	repo.Commit("initial commit")

	// Create a hook file
	hookPath := filepath.Join(repo.Dir, ".git", "hooks", "pre-commit")

	err := os.MkdirAll(filepath.Dir(hookPath), 0o750)
	if err != nil {
		t.Fatalf("failed to create hooks dir: %v", err)
	}

	hookContent := "#!/bin/sh\necho 'readable hook'\nexit 0\n"

	err = os.WriteFile(hookPath, []byte(hookContent), 0o755)
	if err != nil {
		t.Fatalf("failed to create hook file: %v", err)
	}

	// Create CLI tester pointing to the repo
	c := NewCLITesterAt(t, repo.Dir)
	c.Env["HOME"] = filepath.Dir(repo.Dir) // Parent dir as home

	// Read the hook inside the sandbox
	stdout, stderr, code := c.Run("cat", hookPath)

	if code != 0 {
		t.Fatalf("expected exit code 0 when reading hook, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "readable hook") {
		t.Errorf("expected hook content in output, got: %s", stdout)
	}
}

// Test_Sandbox_Git_Can_Still_Read_Config verifies that config can be read
// even though it cannot be written.
func Test_Sandbox_Git_Can_Still_Read_Config(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	// Create a real git repo
	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test Repo\n")
	repo.Commit("initial commit")

	// Get the config file path
	configPath := filepath.Join(repo.Dir, ".git", "config")

	// Create CLI tester pointing to the repo
	c := NewCLITesterAt(t, repo.Dir)
	c.Env["HOME"] = filepath.Dir(repo.Dir) // Parent dir as home

	// Read the config inside the sandbox
	stdout, stderr, code := c.Run("cat", configPath)

	if code != 0 {
		t.Fatalf("expected exit code 0 when reading config, got %d\nstderr: %s", code, stderr)
	}

	// Config should contain the repository format version or core section
	if !strings.Contains(stdout, "[core]") {
		t.Errorf("expected [core] in config output, got: %s", stdout)
	}
}

// Test_Sandbox_Git_Worktree_Protects_All_Four_Paths verifies that all four
// paths are protected in a worktree: main hooks, main config, worktree hooks,
// worktree config.
func Test_Sandbox_Git_Worktree_Protects_All_Four_Paths(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	// Create main repo
	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Main Repo\n")
	repo.Commit("initial commit")

	// Create a worktree
	worktreeDir := filepath.Join(filepath.Dir(repo.Dir), "feature-worktree")
	repo.AddWorktree(worktreeDir, "feature")

	// Setup all four paths
	mainHooksDir := filepath.Join(repo.Dir, ".git", "hooks")

	err := os.MkdirAll(mainHooksDir, 0o750)
	if err != nil {
		t.Fatalf("failed to create main hooks dir: %v", err)
	}

	// Worktree gitdir is named after the worktree directory, not the branch
	worktreeGitDir := filepath.Join(repo.Dir, ".git", "worktrees", filepath.Base(worktreeDir))
	worktreeHooksDir := filepath.Join(worktreeGitDir, "hooks")

	err = os.MkdirAll(worktreeHooksDir, 0o750)
	if err != nil {
		t.Fatalf("failed to create worktree hooks dir: %v", err)
	}

	// Create test files at all four paths
	paths := map[string]string{
		filepath.Join(mainHooksDir, "pre-commit"):     "main-hook",
		filepath.Join(repo.Dir, ".git", "config"):     "", // already exists
		filepath.Join(worktreeHooksDir, "pre-commit"): "worktree-hook",
		filepath.Join(worktreeGitDir, "config"):       "worktree-config",
	}

	for path, content := range paths {
		if content != "" { // Skip if we're using existing file
			err = os.WriteFile(path, []byte(content), 0o644)
			if err != nil {
				t.Fatalf("failed to create %s: %v", path, err)
			}
		}
	}

	// Create CLI tester pointing to the worktree
	c := NewCLITesterAt(t, worktreeDir)
	c.Env["HOME"] = filepath.Dir(repo.Dir)

	// Try to write to each path
	for path := range paths {
		_, _, code := c.Run("bash", "-c", "echo 'hacked' >> "+path)
		if code == 0 {
			// Verify content wasn't actually modified
			content, _ := os.ReadFile(path)
			if strings.Contains(string(content), "hacked") {
				t.Errorf("path %s was modified despite protection", path)
			}
		}
	}
}

// Test_Sandbox_Git_Cannot_Delete_Hooks verifies that hooks cannot be deleted.
func Test_Sandbox_Git_Cannot_Delete_Hooks(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	// Create a real git repo
	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test Repo\n")
	repo.Commit("initial commit")

	// Create a hook file
	hookPath := filepath.Join(repo.Dir, ".git", "hooks", "pre-commit")

	err := os.MkdirAll(filepath.Dir(hookPath), 0o750)
	if err != nil {
		t.Fatalf("failed to create hooks dir: %v", err)
	}

	err = os.WriteFile(hookPath, []byte(testHookContent), 0o755)
	if err != nil {
		t.Fatalf("failed to create hook file: %v", err)
	}

	// Create CLI tester pointing to the repo
	c := NewCLITesterAt(t, repo.Dir)
	c.Env["HOME"] = filepath.Dir(repo.Dir)

	// Try to delete the hook
	_, _, code := c.Run("rm", hookPath)

	// Should fail because .git/hooks is read-only
	if code == 0 {
		t.Error("expected non-zero exit code when deleting hook")
	}

	// Verify the hook still exists
	_, statErr := os.Stat(hookPath)
	if os.IsNotExist(statErr) {
		t.Error("hook was deleted despite protection")
	}
}
