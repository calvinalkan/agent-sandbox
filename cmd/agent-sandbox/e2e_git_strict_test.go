package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// E2E tests for @git-strict preset
// These tests verify that the @git-strict preset properly locks down branches
// and tags while still allowing commits on the current branch.
//
// IMPORTANT: Tests that run git commands must use RunBinaryWithEnv because
// the default @git command wrapper invokes the sandbox's wrap-binary subcommand.
// Using Run() directly causes the test binary (with TestMain) to be mounted,
// which fails when wrap-binary is called.
// ============================================================================

// gitStrictConfig returns a config JSON that enables @git-strict preset.
// Note: We also set git: true to disable the @git command wrapper, since these
// tests focus on filesystem protection, not command wrapper behavior.
const gitStrictConfig = `{
	"filesystem": {
		"presets": ["@git-strict"]
	},
	"commands": {
		"git": true
	}
}`

// setupGitStrictEnv creates a git repository with a config that enables @git-strict.
// Returns env map suitable for RunBinaryWithEnv, with workDir, homeDir, configPath stored in env.
func setupGitStrictEnv(t *testing.T) map[string]string {
	t.Helper()

	RequireGit(t)

	// Create separate directories for HOME, workDir, and config
	homeDir := t.TempDir()
	workDir := t.TempDir()
	configDir := t.TempDir()
	tmpDir := t.TempDir()

	// Initialize git repo
	repo := NewGitRepoAt(t, workDir)
	repo.WriteFile("README.md", "initial content")
	repo.Commit("initial commit")

	// Create config file that enables @git-strict
	configPath := filepath.Join(configDir, "config.json")

	err := os.WriteFile(configPath, []byte(gitStrictConfig), 0o644)
	if err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	return map[string]string{
		"HOME":       homeDir,
		"PATH":       "/usr/local/bin:/usr/bin:/bin",
		"TMPDIR":     tmpDir,
		"WORKDIR":    workDir,
		"CONFIGPATH": configPath,
	}
}

// Test_Sandbox_GitStrict_Allows_Commit_On_Current_Branch verifies that
// commits work on the current branch with @git-strict.
func Test_Sandbox_GitStrict_Allows_Commit_On_Current_Branch(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	env := setupGitStrictEnv(t)
	workDir := env["WORKDIR"]
	configPath := env["CONFIGPATH"]

	// Create a new file and commit it inside the sandbox
	script := `
		cd "` + workDir + `" &&
		echo "new content" > newfile.txt &&
		git add newfile.txt &&
		git commit -m "add newfile"
	`

	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "-c", configPath, "bash", "-c", script)

	if code != 0 {
		t.Errorf("expected git commit to succeed on current branch, got exit code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
}

// Test_Sandbox_GitStrict_Cannot_Modify_Other_Branch_Ref verifies that
// other branch refs cannot be modified with git branch -f.
func Test_Sandbox_GitStrict_Cannot_Modify_Other_Branch_Ref(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	env := setupGitStrictEnv(t)
	workDir := env["WORKDIR"]
	configPath := env["CONFIGPATH"]

	// Create another branch outside the sandbox
	repo := &GitRepo{t: t, Dir: workDir}
	repo.run("branch", "other-branch")

	// Create a new commit to make HEAD differ from other-branch
	repo.WriteFile("new-file.txt", "new content")
	repo.run("add", "-A")
	repo.run("commit", "-m", "new commit")

	// Verify the branch exists and record original commit hash
	otherBranchRef := filepath.Join(workDir, ".git", "refs", "heads", "other-branch")

	originalHash, err := os.ReadFile(otherBranchRef)
	if err != nil {
		t.Fatalf("failed to read other-branch ref: %v", err)
	}

	// Try to force-move another branch to HEAD (which is now a different commit)
	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "-c", configPath, "git", "branch", "-f", "other-branch", "HEAD")

	// Git may report success even if the write fails silently due to ro mount
	// So we check if the ref was actually modified
	newHash, err := os.ReadFile(otherBranchRef)
	if err != nil {
		t.Fatalf("failed to read other-branch ref after test: %v", err)
	}

	// The ref should NOT have been modified
	if !bytes.Equal(originalHash, newHash) {
		t.Errorf("other-branch ref was modified despite @git-strict protection\noriginal: %s\nnew: %s\nstdout: %s\nstderr: %s\nexit code: %d",
			strings.TrimSpace(string(originalHash)),
			strings.TrimSpace(string(newHash)),
			stdout, stderr, code)
	}
}

// Test_Sandbox_GitStrict_Cannot_Delete_Other_Branch verifies that
// other branches cannot be deleted.
func Test_Sandbox_GitStrict_Cannot_Delete_Other_Branch(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	env := setupGitStrictEnv(t)
	workDir := env["WORKDIR"]
	configPath := env["CONFIGPATH"]

	// Create another branch outside the sandbox
	repo := &GitRepo{t: t, Dir: workDir}
	repo.run("branch", "deleteme-branch")

	// Try to delete the other branch
	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "-c", configPath, "git", "branch", "-d", "deleteme-branch")

	// Should fail because refs/heads is read-only
	if code == 0 {
		t.Errorf("expected git branch -d to fail on other branch, but succeeded\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	// Verify the branch still exists
	branchRef := filepath.Join(workDir, ".git", "refs", "heads", "deleteme-branch")
	_, statErr := os.Stat(branchRef)

	if os.IsNotExist(statErr) {
		t.Error("branch was deleted despite @git-strict protection")
	}
}

// Test_Sandbox_GitStrict_Cannot_Create_Tag verifies that tags cannot be created.
func Test_Sandbox_GitStrict_Cannot_Create_Tag(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	env := setupGitStrictEnv(t)
	workDir := env["WORKDIR"]
	configPath := env["CONFIGPATH"]

	// Try to create a tag
	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "-c", configPath, "git", "tag", "v1.0")

	// Should fail because refs/tags is read-only
	if code == 0 {
		t.Errorf("expected git tag to fail, but succeeded\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	// Verify no tag was created
	tagRef := filepath.Join(workDir, ".git", "refs", "tags", "v1.0")

	_, statErr := os.Stat(tagRef)
	if statErr == nil {
		t.Error("tag was created despite @git-strict protection")
	}
}

// Test_Sandbox_GitStrict_Cannot_Delete_Tag verifies that tags cannot be deleted.
func Test_Sandbox_GitStrict_Cannot_Delete_Tag(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	env := setupGitStrictEnv(t)
	workDir := env["WORKDIR"]
	configPath := env["CONFIGPATH"]

	// Create a tag outside the sandbox
	repo := &GitRepo{t: t, Dir: workDir}
	repo.run("tag", "v0.1")

	// Try to delete the tag
	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "-c", configPath, "git", "tag", "-d", "v0.1")

	// Should fail because refs/tags is read-only
	if code == 0 {
		t.Errorf("expected git tag -d to fail, but succeeded\nstdout: %s\nstderr: %s", stdout, stderr)
	}

	// Verify the tag still exists
	tagRef := filepath.Join(workDir, ".git", "refs", "tags", "v0.1")
	_, tagStatErr := os.Stat(tagRef)

	if os.IsNotExist(tagStatErr) {
		t.Error("tag was deleted despite @git-strict protection")
	}
}

// Test_Sandbox_GitStrict_Direct_Ref_Manipulation_Blocked verifies that
// direct manipulation of ref files is blocked.
func Test_Sandbox_GitStrict_Direct_Ref_Manipulation_Blocked(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	env := setupGitStrictEnv(t)
	workDir := env["WORKDIR"]
	configPath := env["CONFIGPATH"]

	// Create another branch outside the sandbox
	repo := &GitRepo{t: t, Dir: workDir}
	repo.run("branch", "protected-branch")

	otherBranchRef := filepath.Join(workDir, ".git", "refs", "heads", "protected-branch")

	originalContent, err := os.ReadFile(otherBranchRef)
	if err != nil {
		t.Fatalf("failed to read branch ref: %v", err)
	}

	// Try to directly modify the ref file
	_, _, code := RunBinaryWithEnv(t, env, "-C", workDir, "-c", configPath, "bash", "-c", "echo 'hacked' > "+otherBranchRef)

	// Should fail because refs/heads is read-only
	if code == 0 {
		t.Error("expected direct ref manipulation to fail")
	}

	// Verify the ref was not modified
	content, err := os.ReadFile(otherBranchRef)
	if err != nil {
		t.Fatalf("failed to read branch ref after test: %v", err)
	}

	if !bytes.Equal(content, originalContent) {
		t.Errorf("branch ref was modified despite protection: got %q, want %q", string(content), string(originalContent))
	}
}

// Test_Sandbox_GitStrict_Stash_Works verifies that git stash still works.
// Stashing uses refs/stash which should not be protected by @git-strict.
func Test_Sandbox_GitStrict_Stash_Works(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	env := setupGitStrictEnv(t)
	workDir := env["WORKDIR"]
	configPath := env["CONFIGPATH"]

	// Create an uncommitted change outside the sandbox
	err := os.WriteFile(filepath.Join(workDir, "changed.txt"), []byte("changed"), 0o644)
	if err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Stash the change inside the sandbox
	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "-c", configPath, "git", "stash")

	if code != 0 {
		t.Errorf("expected git stash to succeed, got exit code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
}

// Test_Sandbox_GitStrict_Cherry_Pick_On_Current_Branch verifies that
// cherry-pick onto the current branch works.
func Test_Sandbox_GitStrict_Cherry_Pick_On_Current_Branch(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	env := setupGitStrictEnv(t)
	workDir := env["WORKDIR"]
	configPath := env["CONFIGPATH"]

	repo := &GitRepo{t: t, Dir: workDir}

	// Create another branch with a commit
	repo.run("branch", "feature")
	repo.run("switch", "feature")
	repo.WriteFile("feature.txt", "feature content")
	repo.run("add", "-A")
	repo.run("commit", "-m", "feature commit")

	// Get the commit hash
	commitHash := strings.TrimSpace(runGitOutput(t, workDir, "rev-parse", "HEAD"))

	// Switch back to main/master
	repo.run("switch", "-")

	// Cherry-pick inside the sandbox
	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "-c", configPath, "git", "cherry-pick", commitHash)

	if code != 0 {
		t.Errorf("expected git cherry-pick to succeed, got exit code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
}

// Test_Sandbox_GitStrict_Merge_Into_Current_Branch verifies that
// merging into the current branch works.
func Test_Sandbox_GitStrict_Merge_Into_Current_Branch(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	env := setupGitStrictEnv(t)
	workDir := env["WORKDIR"]
	configPath := env["CONFIGPATH"]

	repo := &GitRepo{t: t, Dir: workDir}

	// Create another branch with a commit
	repo.run("branch", "mergeable")
	repo.run("switch", "mergeable")
	repo.WriteFile("mergeable.txt", "mergeable content")
	repo.run("add", "-A")
	repo.run("commit", "-m", "mergeable commit")

	// Switch back to main/master
	repo.run("switch", "-")

	// Merge inside the sandbox
	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "-c", configPath, "git", "merge", "mergeable", "--no-edit")

	if code != 0 {
		t.Errorf("expected git merge to succeed, got exit code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
}

// Test_Sandbox_GitStrict_Rebase_Current_Branch verifies that
// rebasing the current branch works.
func Test_Sandbox_GitStrict_Rebase_Current_Branch(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	env := setupGitStrictEnv(t)
	workDir := env["WORKDIR"]
	configPath := env["CONFIGPATH"]

	repo := &GitRepo{t: t, Dir: workDir}

	// Create a commit to rebase
	repo.WriteFile("rebase.txt", "rebase content")
	repo.run("add", "-A")
	repo.run("commit", "-m", "commit to rebase")

	// Get the parent commit
	parentHash := strings.TrimSpace(runGitOutput(t, workDir, "rev-parse", "HEAD~1"))

	// Rebase inside the sandbox (reword the last commit message)
	// Use --onto for a simple rebase that doesn't require interactivity
	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "-c", configPath, "git", "rebase", "--onto", parentHash, parentHash, "HEAD")

	if code != 0 {
		t.Errorf("expected git rebase to succeed, got exit code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
}

// Test_Sandbox_GitStrict_Worktree_Protects_Main_Repo_Refs verifies that
// in a worktree, the main repo's refs are protected.
func Test_Sandbox_GitStrict_Worktree_Protects_Main_Repo_Refs(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	// Create main repo with initial commit
	mainRepoDir := t.TempDir()
	homeDir := t.TempDir()
	configDir := t.TempDir()
	tmpDir := t.TempDir()

	repo := NewGitRepoAt(t, mainRepoDir)
	repo.WriteFile("README.md", "initial")
	repo.Commit("initial commit")

	// Create another branch in main repo
	repo.run("branch", "main-branch-to-protect")

	// Create worktree
	worktreeDir := filepath.Join(t.TempDir(), "worktree")
	repo.AddWorktree(worktreeDir, "feature")

	// Create config file
	configPath := filepath.Join(configDir, "config.json")

	err := os.WriteFile(configPath, []byte(gitStrictConfig), 0o644)
	if err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	env := map[string]string{
		"HOME":   homeDir,
		"PATH":   "/usr/local/bin:/usr/bin:/bin",
		"TMPDIR": tmpDir,
	}

	// Try to modify the protected branch in main repo from worktree
	mainBranchRef := filepath.Join(mainRepoDir, ".git", "refs", "heads", "main-branch-to-protect")

	_, _, code := RunBinaryWithEnv(t, env, "-C", worktreeDir, "-c", configPath, "bash", "-c", "echo 'hacked' > "+mainBranchRef)

	// Should fail because refs/heads is read-only
	if code == 0 {
		t.Error("expected direct ref manipulation in main repo from worktree to fail")
	}
}

// Test_Sandbox_GitStrict_Worktree_Allows_Commit verifies that
// commits work in a worktree with @git-strict.
func Test_Sandbox_GitStrict_Worktree_Allows_Commit(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)
	RequireGit(t)

	// Create main repo with initial commit
	mainRepoDir := t.TempDir()
	homeDir := t.TempDir()
	configDir := t.TempDir()
	tmpDir := t.TempDir()

	repo := NewGitRepoAt(t, mainRepoDir)
	repo.WriteFile("README.md", "initial")
	repo.Commit("initial commit")

	// Create worktree
	worktreeDir := filepath.Join(t.TempDir(), "worktree")
	repo.AddWorktree(worktreeDir, "feature")

	// Create config file
	configPath := filepath.Join(configDir, "config.json")

	err := os.WriteFile(configPath, []byte(gitStrictConfig), 0o644)
	if err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	env := map[string]string{
		"HOME":   homeDir,
		"PATH":   "/usr/local/bin:/usr/bin:/bin",
		"TMPDIR": tmpDir,
	}

	// Create a new file and commit it inside the sandbox
	script := `
		cd "` + worktreeDir + `" &&
		echo "new content" > newfile.txt &&
		git add newfile.txt &&
		git commit -m "add newfile from worktree"
	`

	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", worktreeDir, "-c", configPath, "bash", "-c", script)

	if code != 0 {
		t.Errorf("expected git commit to succeed in worktree, got exit code %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
}

// runGitOutput runs git with the given args and returns stdout.
func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	var outBuf strings.Builder

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = &outBuf
	cmd.Env = cleanGitEnv()

	err := cmd.Run()
	if err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}

	return outBuf.String()
}
