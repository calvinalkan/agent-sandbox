package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// E2E Tests: Command Wrapper Verification
//
// These tests verify command wrappers work correctly inside the sandbox:
// - Blocked commands fail with clear message
// - @git preset blocks dangerous operations
// - Raw mode (true) disables wrappers
// - Custom wrappers receive correct environment
//
// IMPORTANT: Tests that use @git preset or custom wrappers must use RunBinaryWithEnv
// because command wrappers invoke the sandbox's wrap-binary subcommand internally.
// Using Run() directly causes the test binary (with TestMain) to be mounted,
// which fails when wrap-binary is called.
// ============================================================================

// setupGitEnv creates a git repository in a temp directory and returns environment
// variables suitable for running tests with the compiled binary.
// HOME is set to a separate directory from workDir to avoid HOME (ro) conflicting
// with workDir (rw) at the same path level.
func setupGitEnv(t *testing.T) (map[string]string, func()) {
	t.Helper()
	RequireGit(t)

	// Create temp directories using t.TempDir() which auto-cleans up
	// HOME and workDir must be different to avoid ro/rw conflict (per SPEC specificity rules)
	homeDir := t.TempDir()
	workDir := t.TempDir()
	tmpDir := t.TempDir()

	// Initialize git repo in workDir (not homeDir)
	repo := NewGitRepoAt(t, workDir)
	repo.WriteFile("README.md", "initial content")
	repo.Commit("initial commit")

	env := map[string]string{
		"HOME":    homeDir,
		"PATH":    "/usr/local/bin:/usr/bin:/bin",
		"TMPDIR":  tmpDir,
		"WORKDIR": workDir, // Store workDir for use in tests
	}

	// t.TempDir() auto-cleans up, so return no-op cleanup
	return env, func() {}
}

// setupBasicEnv creates basic environment for tests that don't need git.
// HOME is set to a separate directory from workDir to avoid ro/rw conflicts.
func setupBasicEnv(t *testing.T) (string, map[string]string, func()) {
	t.Helper()

	// Create temp directories using t.TempDir() which auto-cleans up
	homeDir := t.TempDir()
	workDir := t.TempDir()
	tmpDir := t.TempDir()

	env := map[string]string{
		"HOME":   homeDir,
		"PATH":   "/usr/local/bin:/usr/bin:/bin",
		"TMPDIR": tmpDir,
	}

	// t.TempDir() auto-cleans up, so return no-op cleanup
	return workDir, env, func() {}
}

// ============================================================================
// Blocked Command Tests (commands: false)
// These use Run() since blocked commands don't invoke wrap-binary
// ============================================================================

func Test_Sandbox_Blocks_Command_When_Set_To_False(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	c := NewCLITester(t)

	// Block cat command and try to run it
	_, stderr, code := c.Run("--cmd", "cat=false", "cat", "/etc/hostname")

	if code == 0 {
		t.Error("expected non-zero exit code when running blocked command")
	}

	AssertContains(t, stderr, "blocked")
	AssertContains(t, stderr, "cat")
}

func Test_Sandbox_Blocked_Command_Shows_Command_Name_In_Message(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	c := NewCLITester(t)

	// Block echo command
	_, stderr, code := c.Run("--cmd", "echo=false", "echo", "test")

	if code == 0 {
		t.Error("expected non-zero exit code when running blocked command")
	}

	// The error message should include the blocked command name
	AssertContains(t, stderr, "echo")
	AssertContains(t, stderr, "blocked")
}

func Test_Sandbox_Multiple_Blocked_Commands(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	c := NewCLITester(t)

	// Block multiple commands
	_, stderr, code := c.Run("--cmd", "cat=false,touch=false", "cat", "/etc/hostname")

	if code == 0 {
		t.Error("expected non-zero exit code for cat")
	}

	AssertContains(t, stderr, "blocked")

	// Also verify touch is blocked
	_, stderr, code = c.Run("--cmd", "cat=false,touch=false", "touch", c.TempFile("testfile"))

	if code == 0 {
		t.Error("expected non-zero exit code for touch")
	}

	AssertContains(t, stderr, "blocked")
}

// ============================================================================
// @git Preset Tests - Blocked Operations
// These use RunBinaryWithEnv since @git preset invokes wrap-binary
// ============================================================================

func Test_Sandbox_Git_Preset_Blocks_Checkout(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	// Default config includes git: "@git"
	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "checkout", "main")

	if code == 0 {
		t.Error("expected git checkout to be blocked")
	}

	// Should suggest using git switch
	AssertContains(t, stderr, "checkout")
	AssertContains(t, stderr, "switch")
}

func Test_Sandbox_Git_Preset_Blocks_Restore(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "restore", "README.md")

	if code == 0 {
		t.Error("expected git restore to be blocked")
	}

	AssertContains(t, stderr, "restore")
}

func Test_Sandbox_Git_Preset_Blocks_Reset_Hard(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "reset", "--hard", "HEAD")

	if code == 0 {
		t.Error("expected git reset --hard to be blocked")
	}

	AssertContains(t, stderr, "reset")
	AssertContains(t, stderr, "--hard")
}

func Test_Sandbox_Git_Preset_Blocks_Clean_Force(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "clean", "-f")

	if code == 0 {
		t.Error("expected git clean -f to be blocked")
	}

	AssertContains(t, stderr, "clean")
}

func Test_Sandbox_Git_Preset_Blocks_Commit_NoVerify(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	// Create a new file to commit
	err := os.WriteFile(filepath.Join(env["WORKDIR"], "newfile.txt"), []byte("content"), 0o644)
	if err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// git add should work
	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "add", ".")
	if code != 0 {
		t.Fatalf("git add failed: %s", stderr)
	}

	// git commit --no-verify should be blocked
	_, stderr, code = RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "commit", "--no-verify", "-m", "test")

	if code == 0 {
		t.Error("expected git commit --no-verify to be blocked")
	}

	AssertContains(t, stderr, "--no-verify")
}

func Test_Sandbox_Git_Preset_Blocks_Stash_Drop(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "stash", "drop")

	if code == 0 {
		t.Error("expected git stash drop to be blocked")
	}

	AssertContains(t, stderr, "stash")
	AssertContains(t, stderr, "drop")
}

func Test_Sandbox_Git_Preset_Blocks_Stash_Clear(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "stash", "clear")

	if code == 0 {
		t.Error("expected git stash clear to be blocked")
	}

	AssertContains(t, stderr, "stash")
	AssertContains(t, stderr, "clear")
}

func Test_Sandbox_Git_Preset_Blocks_Stash_Pop(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "stash", "pop")

	if code == 0 {
		t.Error("expected git stash pop to be blocked")
	}

	AssertContains(t, stderr, "stash")
	AssertContains(t, stderr, "pop")
}

func Test_Sandbox_Git_Preset_Blocks_Branch_ForceDelete(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "branch", "-D", "nonexistent")

	if code == 0 {
		t.Error("expected git branch -D to be blocked")
	}

	AssertContains(t, stderr, "branch")
	AssertContains(t, stderr, "-D")
}

func Test_Sandbox_Git_Preset_Blocks_Push_Force(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	// push --force should be blocked even without a remote
	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "push", "--force", "origin", "main")

	if code == 0 {
		t.Error("expected git push --force to be blocked")
	}

	AssertContains(t, stderr, "push")
	AssertContains(t, stderr, "--force")
	AssertContains(t, stderr, "force-with-lease")
}

// ============================================================================
// @git Preset Tests - Allowed Operations
// ============================================================================

func Test_Sandbox_Git_Preset_Allows_Status(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "status")

	if code != 0 {
		t.Errorf("expected git status to succeed, got exit %d\nstderr: %s", code, stderr)
	}

	// Should show status output
	if !strings.Contains(stdout, "branch") && !strings.Contains(stdout, "clean") {
		t.Errorf("expected git status output, got: %s", stdout)
	}
}

func Test_Sandbox_Git_Preset_Allows_Log(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "log", "--oneline")

	if code != 0 {
		t.Errorf("expected git log to succeed, got exit %d\nstderr: %s", code, stderr)
	}

	// Should show commit message
	AssertContains(t, stdout, "initial commit")
}

func Test_Sandbox_Git_Preset_Allows_Diff(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "diff", "HEAD")

	// May return 0 or 1 depending on changes, but shouldn't be blocked
	if strings.Contains(stderr, "blocked") {
		t.Errorf("git diff should not be blocked: %s", stderr)
	}

	_ = code // Exit code doesn't matter for this test
}

func Test_Sandbox_Git_Preset_Allows_Switch(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	// Try to create a new branch with switch
	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "switch", "-c", "new-branch")

	// Should not be blocked by wrapper
	if strings.Contains(stderr, "blocked") {
		t.Errorf("git switch should not be blocked: %s", stderr)
	}

	// Should succeed in creating a new branch
	if code != 0 {
		t.Logf("git switch output: %s", stderr)
	}
}

func Test_Sandbox_Git_Preset_Allows_Reset_Soft(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	// Create a second commit so we can reset
	err := os.WriteFile(filepath.Join(env["WORKDIR"], "file2.txt"), []byte("content"), 0o644)
	if err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "add", ".")
	if code != 0 {
		t.Fatalf("git add failed: %s", stderr)
	}

	_, stderr, code = RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "commit", "-m", "second commit")
	if code != 0 {
		t.Fatalf("git commit failed: %s", stderr)
	}

	_, stderr, code = RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "reset", "--soft", "HEAD~1")

	// Should not be blocked
	if strings.Contains(stderr, "blocked") {
		t.Errorf("git reset --soft should not be blocked: %s", stderr)
	}

	if code != 0 {
		t.Errorf("expected git reset --soft to succeed, got exit %d\nstderr: %s", code, stderr)
	}
}

func Test_Sandbox_Git_Preset_Allows_Stash_Apply(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	// stash apply without stash should fail but not be blocked
	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "stash", "apply")

	// Should not be blocked by wrapper
	if strings.Contains(stderr, "blocked") {
		t.Errorf("git stash apply should not be blocked: %s", stderr)
	}

	_ = code // May fail with "No stash entries found" which is fine
}

func Test_Sandbox_Git_Preset_Allows_Branch_SafeDelete(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	// -d (safe delete) should be allowed, but may fail if branch doesn't exist
	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "branch", "-d", "nonexistent")

	// Should not be blocked by wrapper
	if strings.Contains(stderr, "blocked") {
		t.Errorf("git branch -d should not be blocked: %s", stderr)
	}

	_ = code // May fail with "branch not found" which is fine
}

func Test_Sandbox_Git_Preset_Allows_Push_ForceWithLease(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	// --force-with-lease should be allowed
	_, stderr, _ := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "push", "--force-with-lease", "origin", "main")

	// Should not be blocked by wrapper (will fail for no remote but that's ok)
	if strings.Contains(stderr, "blocked") {
		t.Errorf("git push --force-with-lease should not be blocked: %s", stderr)
	}
}

func Test_Sandbox_Git_Preset_Allows_Commit_Normal(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	// Create a change to commit
	err := os.WriteFile(filepath.Join(env["WORKDIR"], "newfile.txt"), []byte("content"), 0o644)
	if err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "add", ".")
	if code != 0 {
		t.Fatalf("git add failed: %s", stderr)
	}

	// Normal commit should work
	_, stderr, code = RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "commit", "-m", "normal commit")

	if code != 0 {
		t.Errorf("expected normal git commit to succeed, got exit %d\nstderr: %s", code, stderr)
	}

	// Should not be blocked
	AssertNotContains(t, stderr, "blocked")
}

// ============================================================================
// Raw Command (true) Tests - Disables Wrapper
// ============================================================================

func Test_Sandbox_Raw_Command_Bypasses_Wrapper(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	// With git=true, the wrapper is disabled and checkout should go through
	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "--cmd", "git=true", "git", "checkout", "HEAD")

	// If it fails, it should NOT be because of the wrapper blocking it
	if strings.Contains(stderr, "blocked") || strings.Contains(stderr, "switch") {
		t.Errorf("git checkout should not be blocked when git=true: %s", stderr)
	}

	// With detached HEAD checkout, it should actually succeed
	if code != 0 {
		t.Logf("git checkout output: %s", stderr)
	}
}

func Test_Sandbox_Raw_Command_Allows_All_Subcommands(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	// With git=true, all normally blocked subcommands should be allowed
	blockedCmds := [][]string{
		{"checkout", "HEAD"},
		{"restore", "--staged", "README.md"},
		{"reset", "--hard", "HEAD"},
		{"clean", "-n"}, // -n is dry run, safe
		{"stash", "list"},
		{"branch", "-a"},
	}

	for _, cmd := range blockedCmds {
		args := append([]string{"-C", env["WORKDIR"], "--cmd", "git=true", "git"}, cmd...)
		_, stderr, _ := RunBinaryWithEnv(t, env, args...)

		// Should never see "blocked" in error
		if strings.Contains(stderr, "blocked") {
			t.Errorf("git %v should not be blocked when git=true: %s", cmd, stderr)
		}
	}
}

// ============================================================================
// Git with Global Flags Tests
// ============================================================================

func Test_Sandbox_Git_Preset_Handles_Global_Flags_Before_Subcommand(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	// -C is a global flag, checkout is still the subcommand
	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "-C", env["WORKDIR"], "checkout", "main")

	if code == 0 {
		t.Error("expected git checkout to be blocked even with -C flag")
	}

	AssertContains(t, stderr, "checkout")
	AssertContains(t, stderr, "switch")
}

func Test_Sandbox_Git_Preset_Handles_NoPager_Flag(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	// --no-pager is a global flag
	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "--no-pager", "checkout", "main")

	if code == 0 {
		t.Error("expected git checkout to be blocked even with --no-pager flag")
	}

	AssertContains(t, stderr, "checkout")
}

func Test_Sandbox_Git_Preset_Handles_Config_Flag(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	// -c is a global flag that takes a value
	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "-c", "core.editor=vim", "checkout", "main")

	if code == 0 {
		t.Error("expected git checkout to be blocked even with -c flag")
	}

	AssertContains(t, stderr, "checkout")
}

// ============================================================================
// Custom Wrapper Script Tests
// ============================================================================

func Test_Sandbox_Custom_Wrapper_Receives_Env_Variable(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	workDir, env, cleanup := setupBasicEnv(t)
	defer cleanup()

	// Create a custom wrapper script that prints the env var
	wrapperScript := filepath.Join(workDir, "wrapper.sh")

	err := os.WriteFile(wrapperScript, []byte(`#!/bin/bash
if [ -n "$AGENT_SANDBOX_ECHO" ]; then
    echo "ENV_VAR_SET:$AGENT_SANDBOX_ECHO"
    exec "$AGENT_SANDBOX_ECHO" "$@"
else
    echo "ENV_VAR_NOT_SET" >&2
    exit 1
fi
`), 0o755)
	if err != nil {
		t.Fatalf("failed to write wrapper script: %v", err)
	}

	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "--cmd", "echo="+wrapperScript, "echo", "test message")

	if code != 0 {
		t.Errorf("expected custom wrapper to succeed, got exit %d\nstderr: %s", code, stderr)
	}

	// Should see our env var print
	AssertContains(t, stdout, "ENV_VAR_SET:")

	// Should also see the actual echo output
	AssertContains(t, stdout, "test message")
}

func Test_Sandbox_Custom_Wrapper_Can_Block_Commands(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	workDir, env, cleanup := setupBasicEnv(t)
	defer cleanup()

	// Create a custom wrapper that blocks certain arguments
	wrapperScript := filepath.Join(workDir, "wrapper.sh")

	err := os.WriteFile(wrapperScript, []byte(`#!/bin/bash
# Block any argument containing "forbidden"
for arg in "$@"; do
    if [[ "$arg" == *"forbidden"* ]]; then
        echo "custom wrapper: argument containing 'forbidden' is blocked" >&2
        exit 1
    fi
done
exec "$AGENT_SANDBOX_ECHO" "$@"
`), 0o755)
	if err != nil {
		t.Fatalf("failed to write wrapper script: %v", err)
	}

	// Should work normally
	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "--cmd", "echo="+wrapperScript, "echo", "hello")
	if code != 0 {
		t.Errorf("expected echo hello to succeed: %s", stderr)
	}

	AssertContains(t, stdout, "hello")

	// Should be blocked by custom wrapper
	_, stderr, code = RunBinaryWithEnv(t, env, "-C", workDir, "--cmd", "echo="+wrapperScript, "echo", "forbidden-word")
	if code == 0 {
		t.Error("expected custom wrapper to block forbidden argument")
	}

	AssertContains(t, stderr, "custom wrapper")
	AssertContains(t, stderr, "forbidden")
}

func Test_Sandbox_Custom_Wrapper_Passes_All_Arguments(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	workDir, env, cleanup := setupBasicEnv(t)
	defer cleanup()

	// Create a wrapper that counts arguments
	wrapperScript := filepath.Join(workDir, "wrapper.sh")

	err := os.WriteFile(wrapperScript, []byte(`#!/bin/bash
echo "ARGC:$#"
exec "$AGENT_SANDBOX_ECHO" "$@"
`), 0o755)
	if err != nil {
		t.Fatalf("failed to write wrapper script: %v", err)
	}

	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "--cmd", "echo="+wrapperScript, "echo", "one", "two", "three")

	if code != 0 {
		t.Errorf("expected success, got exit %d\nstderr: %s", code, stderr)
	}

	// Wrapper receives 3 arguments
	AssertContains(t, stdout, "ARGC:3")

	// Echo outputs all arguments
	AssertContains(t, stdout, "one two three")
}

// ============================================================================
// Error Handling Tests
// ============================================================================

func Test_Sandbox_Returns_Error_When_Blocked_Command_Not_Found(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	c := NewCLITester(t)

	// Block a non-existent command - should not cause setup error
	// but running it should fail (command not found)
	_, stderr, code := c.Run("--cmd", "nonexistent_cmd=false", "nonexistent_cmd", "arg")

	// Should fail because command doesn't exist (not blocked message)
	if code == 0 {
		t.Error("expected non-zero exit code for non-existent command")
	}

	// Might say "not found" or "No such file" but shouldn't crash
	_ = stderr
}

func Test_Sandbox_Wrapper_Works_With_Complex_Arguments(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	env, cleanup := setupGitEnv(t)
	defer cleanup()

	// Create a new file
	err := os.WriteFile(filepath.Join(env["WORKDIR"], "newfile.txt"), []byte("content"), 0o644)
	if err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "add", ".")
	if code != 0 {
		t.Fatalf("git add failed: %s", stderr)
	}

	// Commit with special characters in message
	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", env["WORKDIR"], "git", "commit", "-m", "test commit with 'quotes' and \"double quotes\"")

	if code != 0 {
		t.Errorf("expected commit to succeed: %s", stderr)
	}

	_ = stdout
}

// ============================================================================
// Config File Tests
// ============================================================================

func Test_Sandbox_Config_File_Command_Rules_Work(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	c := NewCLITester(t)

	// Create config file with blocked command
	c.WriteFile(".agent-sandbox.json", `{
		"commands": {
			"cat": false
		}
	}`)

	_, stderr, code := c.Run("cat", "/etc/hostname")

	if code == 0 {
		t.Error("expected cat to be blocked via config file")
	}

	AssertContains(t, stderr, "blocked")
}

func Test_Sandbox_CLI_Flag_Overrides_Config_File_Command(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	c := NewCLITester(t)

	// Create config file with blocked command
	c.WriteFile(".agent-sandbox.json", `{
		"commands": {
			"echo": false
		}
	}`)

	// Verify it's blocked via config
	_, stderr, code := c.Run("echo", "test")
	if code == 0 {
		t.Error("expected echo to be blocked via config")
	}

	AssertContains(t, stderr, "blocked")

	// Override with CLI flag
	stdout, stderr, code := c.Run("--cmd", "echo=true", "echo", "hello")
	if code != 0 {
		t.Errorf("expected echo to work with --cmd override: %s", stderr)
	}

	AssertContains(t, stdout, "hello")
}
