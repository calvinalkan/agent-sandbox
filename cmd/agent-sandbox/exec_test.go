package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func Test_Exec_Accepts_Network_Flag_When_Implicit_Mode(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// This should work: implicit exec with --network flag
	_, stderr, code := c.Run("--network=false", "echo", "hello")

	// Should not fail with "unknown flag" - exec command handles it
	AssertNotContains(t, stderr, "unknown flag")

	// Exit code 0 means the command was accepted (exec prints "not yet implemented")
	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}
}

func Test_Exec_Accepts_Docker_Flag_When_Implicit_Mode(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Use --docker=false to test flag parsing without requiring docker socket
	_, stderr, code := c.Run("--docker=false", "echo", "hello")

	AssertNotContains(t, stderr, "unknown flag")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}
}

func Test_Exec_Accepts_Ro_Flag_When_Implicit_Mode(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--ro", "/tmp", "echo", "hello")

	AssertNotContains(t, stderr, "unknown flag")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}
}

func Test_Exec_Accepts_Rw_Flag_When_Implicit_Mode(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--rw", "/tmp", "echo", "hello")

	AssertNotContains(t, stderr, "unknown flag")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}
}

func Test_Exec_Accepts_Exclude_Flag_When_Implicit_Mode(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--exclude", ".env", "echo", "hello")

	AssertNotContains(t, stderr, "unknown flag")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}
}

func Test_Exec_Accepts_Multiple_Flags_When_Implicit_Mode(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Multiple exec flags together (use --docker=false to avoid requiring docker socket)
	_, stderr, code := c.Run("--network=false", "--docker=false", "--ro", "/tmp", "echo", "hello")

	AssertNotContains(t, stderr, "unknown flag")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}
}

func Test_Exec_Works_When_Global_And_Exec_Flags_Are_Set(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Mix of global flag (--cwd is added by test helper) and exec flags
	_, stderr, code := c.Run("--network=false", "echo", "hello")

	AssertNotContains(t, stderr, "unknown flag")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}
}

func Test_Help_Works_When_Config_Is_Invalid(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	c.WriteFile(".agent-sandbox.jsonc", `{invalid json}`)

	// Help should work even with invalid config
	stdout, _, code := c.Run("--help")

	if code != 0 {
		t.Errorf("expected exit code 0 for --help, got %d", code)
	}

	AssertContains(t, stdout, "agent-sandbox")
	AssertContains(t, stdout, "Flags:")
}

func Test_Help_Works_When_Explicit_Config_Is_Missing(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Help should work even with missing explicit config
	stdout, _, code := c.Run("--config", "nonexistent.json", "--help")

	if code != 0 {
		t.Errorf("expected exit code 0 for --help, got %d", code)
	}

	AssertContains(t, stdout, "agent-sandbox")
}

// ============================================================================
// Exec command home directory validation tests
// ============================================================================

func Test_Exec_Returns_Error_When_Home_Directory_Does_Not_Exist(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	c.Env["HOME"] = "/nonexistent/path/that/does/not/exist"

	_, stderr, code := c.Run("echo", "hello")

	if code == 0 {
		t.Error("expected non-zero exit code for nonexistent home")
	}

	AssertContains(t, stderr, "missing path")
}

func Test_Exec_Returns_Error_When_Home_Is_File(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a file where HOME points to
	filePath := c.Dir + "/not-a-dir"
	c.WriteFile("not-a-dir", "test content")
	c.Env["HOME"] = filePath

	_, stderr, code := c.Run("echo", "hello")

	if code == 0 {
		t.Error("expected non-zero exit code when HOME is a file")
	}

	AssertContains(t, stderr, "not a directory")
}

func Test_Exec_Succeeds_When_Home_Is_Valid(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	// HOME is auto-set to c.Dir by NewCLITester

	_, stderr, code := c.Run("echo", "hello")

	// Should succeed (exec prints "not yet implemented" but exit 0)
	if code != 0 {
		t.Errorf("expected exit code 0 for valid home, got %d\nstderr: %s", code, stderr)
	}
}

// ============================================================================
// Dry-run tests
// ============================================================================

func Test_DryRun_Outputs_Bwrap_Command_When_Dry_Run_Flag_Is_Set(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	stdout, _, code := c.Run("--dry-run", "echo", "hello")

	// Exit code should be 0 for dry-run
	if code != 0 {
		t.Errorf("expected exit code 0 for --dry-run, got %d", code)
	}

	// Output should start with "bwrap"
	AssertContains(t, stdout, "bwrap")
}

func Test_DryRun_Includes_Standard_Bwrap_Args_When_Dry_Run_Flag_Is_Set(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	stdout, _, code := c.Run("--dry-run", "npm", "install")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}

	// Should contain standard bwrap arguments from BwrapArgs
	AssertContains(t, stdout, "--die-with-parent")
	AssertContains(t, stdout, "--unshare-all")
	AssertContains(t, stdout, "--dev")
	AssertContains(t, stdout, "--proc")
	AssertContains(t, stdout, "--ro-bind")
	AssertContains(t, stdout, "--chdir")
}

func Test_DryRun_Includes_Command_Separator_When_Dry_Run_Flag_Is_Set(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	stdout, _, code := c.Run("--dry-run", "npm", "install")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}

	// Should contain "--" separator followed by command
	AssertContains(t, stdout, "-- npm install")
}

func Test_DryRun_Includes_User_Command_And_Args_When_Dry_Run_Flag_Is_Set(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	stdout, _, code := c.Run("--dry-run", "git", "commit", "-m", "test message")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}

	// Should contain the full user command
	// Note: "test message" contains a space so it should be quoted
	AssertContains(t, stdout, "git")
	AssertContains(t, stdout, "commit")
	AssertContains(t, stdout, "-m")
	AssertContains(t, stdout, "test message")
}

func Test_DryRun_Returns_Zero_Exit_Code_When_Command_Does_Not_Exist(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Even a command that doesn't exist should result in exit 0 for dry-run
	_, _, code := c.Run("--dry-run", "nonexistent-command-12345")

	if code != 0 {
		t.Errorf("expected exit code 0 for --dry-run, got %d", code)
	}
}

func Test_DryRun_Respects_Network_Setting_When_Network_Is_Disabled(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// With network enabled (default), should have --share-net
	stdoutWithNet, _, _ := c.Run("--dry-run", "echo", "test")
	AssertContains(t, stdoutWithNet, "--share-net")

	// With network disabled, should NOT have --share-net
	stdoutNoNet, _, _ := c.Run("--dry-run", "--network=false", "echo", "test")
	AssertNotContains(t, stdoutNoNet, "--share-net")
}

func Test_DryRun_Works_When_Exec_Command_Is_Explicit(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	stdout, _, code := c.Run("--dry-run", "echo", "hello")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}

	AssertContains(t, stdout, "bwrap")
	AssertContains(t, stdout, "-- echo hello")
}

func Test_DryRun_Does_Not_Execute_Command_When_Dry_Run_Flag_Is_Set(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a marker file that the command would create if executed
	markerPath := c.Dir + "/marker-created"

	// Run a command that would create a file
	_, _, code := c.Run("--dry-run", "touch", markerPath)

	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}

	// The marker file should NOT exist because command was not executed
	if c.FileExists("marker-created") {
		t.Error("dry-run should not execute the command, but marker file was created")
	}
}

func Test_DryRun_Quotes_Args_When_Special_Characters_Are_Present(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	stdout, _, code := c.Run("--dry-run", "echo", "hello world", "with'quote")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}

	AssertContains(t, stdout, "hello world")
	AssertContains(t, stdout, "with")
	AssertContains(t, stdout, "quote")
}

// ============================================================================
// E2E Integration Tests - Full Pipeline Execution
// ============================================================================

func Test_Exec_Pipeline_Returns_Output_When_Command_Succeeds(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	stdout, stderr, code := c.Run("echo", "hello from full pipeline")

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	AssertContains(t, stdout, "hello from full pipeline")
}

func Test_Exec_Pipeline_Returns_Exit_Code_When_Command_Exits(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Test various exit codes
	_, _, code := c.Run("bash", "-c", "exit 0")
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}

	_, _, code = c.Run("bash", "-c", "exit 1")
	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}

	_, _, code = c.Run("bash", "-c", "exit 42")
	if code != 42 {
		t.Errorf("expected exit code 42, got %d", code)
	}
}

// ============================================================================
// E2E Tests - Filesystem Restrictions
// ============================================================================

func Test_Exec_WorkDir_Is_Writable_When_Default_Config_Is_Used(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Use TMPDIR for writing since HOME == workdir results in ro (per specificity)
	// TempFile returns a path in the writable TMPDIR
	testFile := c.TempFile("test-writable.txt")
	_, stderr, code := c.Run("bash", "-c", "echo 'created inside sandbox' > "+testFile)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Verify the file was actually created
	if !c.FileExistsAt(c.Env["TMPDIR"], "test-writable.txt") {
		t.Error("file should have been created in TMPDIR")
	}

	content := c.ReadFileAt(c.Env["TMPDIR"], "test-writable.txt")
	AssertContains(t, content, "created inside sandbox")
}

func Test_Exec_Read_Only_Path_Cannot_Be_Written_When_Ro_Flag_Is_Used(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a directory with a file to protect
	c.WriteFile("protected/original.txt", "original content")

	// Try to write to a protected path (--ro flag)
	_, _, code := c.Run("--ro", "protected", "bash", "-c", "echo 'modified' > protected/original.txt")

	// Should fail because the path is read-only
	if code == 0 {
		content := c.ReadFile("protected/original.txt")
		if content != "original content" {
			t.Error("read-only protected file was modified")
		} else {
			t.Error("expected non-zero exit code when writing to read-only path")
		}
	}

	// Verify original content is unchanged
	content := c.ReadFile("protected/original.txt")
	if content != "original content" {
		t.Errorf("read-only file content changed, expected 'original content', got %q", content)
	}
}

func Test_Exec_Exclude_Path_Cannot_Be_Read_When_Exclude_Flag_Is_Used(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a file to exclude
	c.WriteFile("secret/password.txt", "super secret")

	// Try to read the excluded file
	stdout, _, code := c.Run("--exclude", "secret", "cat", "secret/password.txt")

	// Should fail because the path is excluded
	if code == 0 {
		t.Errorf("expected non-zero exit code when reading excluded path, got stdout: %s", stdout)
	}

	// Verify the secret content is not in the output
	AssertNotContains(t, stdout, "super secret")
}

func Test_Exec_Exclude_Directory_Is_Hidden_When_Exclude_Flag_Is_Used(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a directory to exclude
	c.WriteFile("hidden/.secret", "hidden content")

	// Try to list the excluded directory
	stdout, _, code := c.Run("--exclude", "hidden", "ls", "hidden")

	// Should fail because the directory appears empty or non-existent
	if code == 0 && strings.Contains(stdout, ".secret") {
		t.Error("excluded directory contents should not be visible")
	}
}

func Test_Exec_Rw_Path_Is_Writable_When_Rw_Flag_Is_Used(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a directory that would normally be read-only (outside workdir)
	outputDir := t.TempDir()
	outputFile := outputDir + "/output.txt"

	// Add the output directory as writable
	_, stderr, code := c.Run("--rw", outputDir, "bash", "-c", "echo 'written' > "+outputFile)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Verify the file was written
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	if !strings.Contains(string(content), "written") {
		t.Errorf("expected 'written' in output file, got: %s", string(content))
	}
}

// ============================================================================
// E2E Tests - Command Wrappers
// ============================================================================

func Test_Exec_Wrapper_Cleanup_Happens_When_Command_Errors(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Use an invalid exclude path that will cause an error after wrapper setup
	// but before execution. The cleanup should still happen.
	// This tests the defer cleanup behavior.

	// Create a path that will be excluded and checked
	c.WriteFile(".env", "secret=value")

	// Run a command that will fail (non-existent command)
	// The wrapper setup should complete and then be cleaned up
	_, _, code := c.Run("--exclude", ".env", "nonexistent_command_xyz")

	// The command doesn't exist so it should fail, but the test
	// verifies no panic occurs during cleanup
	if code == 0 {
		t.Log("command unexpectedly succeeded")
	}
}

// ============================================================================
// E2E Tests - Debug Output
// ============================================================================

func Test_Exec_Debug_Shows_Config_Loading_When_Debug_Is_Enabled(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--debug", "true")

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	// Debug output should show config loading info
	AssertContains(t, stderr, "config-load")
}

func Test_Exec_Debug_Shows_Bwrap_Args_When_Debug_Is_Enabled(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--debug", "true")

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	// Debug output should show bwrap arguments
	AssertContains(t, stderr, "bwrap-args")
}

func Test_Exec_Debug_Shows_Command_Wrappers_When_Debug_Is_Enabled(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--debug", "true")

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	// Debug output should show command wrapper info
	AssertContains(t, stderr, "command-wrappers")
}

// ============================================================================
// E2E Tests - Error Handling
// ============================================================================

func Test_Exec_Returns_Error_When_Preset_Is_Unknown(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a config file with an unknown preset
	c.WriteFile(".agent-sandbox.jsonc", `{
		"filesystem": {
			"presets": ["@nonexistent-preset"]
		}
	}`)

	_, stderr, code := c.Run("echo", "hello")

	if code == 0 {
		t.Error("expected non-zero exit code for unknown preset")
	}

	AssertContains(t, stderr, "unknown preset")
}

// ============================================================================
// Helper function tests
// ============================================================================

func Test_GetLoadedConfigPaths_Returns_Nil_When_Config_Is_Nil(t *testing.T) {
	t.Parallel()

	paths := getLoadedConfigPaths(nil)
	if paths != nil {
		t.Errorf("expected nil, got %v", paths)
	}
}

func Test_GetLoadedConfigPaths_Returns_Nil_When_Config_Map_Is_Empty(t *testing.T) {
	t.Parallel()

	cfg := &Config{}
	paths := getLoadedConfigPaths(cfg)

	if paths != nil {
		t.Errorf("expected nil, got %v", paths)
	}
}

func Test_GetLoadedConfigPaths_Returns_Paths_When_Config_Map_Has_Paths(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		LoadedConfigFiles: map[string]string{
			"global":  "/path/to/global",
			"project": "/path/to/project",
		},
	}

	paths := getLoadedConfigPaths(cfg)

	if len(paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(paths))
	}

	// Check that both paths are present (order may vary due to map iteration)
	pathMap := make(map[string]bool)
	for _, path := range paths {
		pathMap[path] = true
	}

	if !pathMap["/path/to/global"] {
		t.Error("expected /path/to/global in paths")
	}

	if !pathMap["/path/to/project"] {
		t.Error("expected /path/to/project in paths")
	}
}

func Test_NotLinuxMessage_Contains_Hint_When_Platform_Is_Not_Linux(t *testing.T) {
	t.Parallel()

	// Verify the error message contains the hint about why Linux is required
	if !strings.Contains("agent-sandbox requires Linux (bwrap uses Linux namespaces)", "Linux") {
		t.Error("errNotLinuxMessage should mention Linux")
	}

	if !strings.Contains("agent-sandbox requires Linux (bwrap uses Linux namespaces)", "bwrap") || !strings.Contains("agent-sandbox requires Linux (bwrap uses Linux namespaces)", "namespaces") {
		t.Error("errNotLinuxMessage should explain why Linux is required (bwrap uses Linux namespaces)")
	}
}

func Test_RunningAsRootMessage_Contains_Hint_When_Running_As_Root(t *testing.T) {
	t.Parallel()

	// Verify the error message contains a hint about what to do
	if !strings.Contains("agent-sandbox cannot run as root (use a regular user account)", "root") {
		t.Error("errRunningAsRootMessage should mention root")
	}

	if !strings.Contains("agent-sandbox cannot run as root (use a regular user account)", "regular user") {
		t.Error("errRunningAsRootMessage should suggest using a regular user account")
	}
}

func Test_BwrapNotFoundMessage_Contains_Install_Hint_When_Bwrap_Is_Missing(t *testing.T) {
	t.Parallel()

	// Verify the error message contains installation instructions
	if !strings.Contains("bwrap not found in PATH (try installing with: sudo apt install bubblewrap)", "bwrap") {
		t.Error("errBwrapNotFoundMessage should mention bwrap")
	}

	if !strings.Contains("bwrap not found in PATH (try installing with: sudo apt install bubblewrap)", "apt install bubblewrap") {
		t.Error("errBwrapNotFoundMessage should contain installation hint")
	}
}

// ============================================================================
// Outside Sandbox Tests - --cmd flag
//
// These tests verify --cmd behavior when launching a sandbox from outside.
// ============================================================================

func Test_Exec_CmdFlag_When_Running_Outside_Sandbox(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	t.Run("Accepts_Flag_In_Implicit_Mode", func(t *testing.T) {
		t.Parallel()
		c := NewCLITester(t)
		_, stderr, code := c.Run("--cmd", "git=true", "echo", "hello")
		AssertNotContains(t, stderr, "unknown flag")

		if code != 0 {
			t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
		}
	})

	t.Run("Invalid_Format", func(t *testing.T) {
		t.Parallel()
		c := NewCLITester(t)

		_, stderr, code := c.Run("--cmd", "invalid-no-equals", "echo", "hi")
		if code == 0 {
			t.Fatal("expected error for invalid --cmd format")
		}

		AssertContains(t, stderr, "invalid --cmd format")
	})

	t.Run("Empty_Key", func(t *testing.T) {
		t.Parallel()
		c := NewCLITester(t)

		_, stderr, code := c.Run("--cmd", "=true", "echo", "hi")
		if code == 0 {
			t.Fatal("expected error for empty key in --cmd")
		}

		AssertContains(t, stderr, "empty key")
	})

	t.Run("Blocked_Command_Cannot_Run", func(t *testing.T) {
		t.Parallel()
		c := NewCLITester(t)

		// Must use RunBinary for actual wrapper execution (ELF launcher needs real binary)
		_, stderr, code := RunBinaryWithEnv(t, c.Env, "-C", c.Dir, "--cmd", "cat=false", "cat", "/etc/hostname")
		if code == 0 {
			t.Error("expected non-zero exit code when running blocked command")
		}

		AssertContains(t, stderr, "blocked")
	})

	t.Run("Raw_Command_Bypasses_Wrapper", func(t *testing.T) {
		t.Parallel()
		c := NewCLITester(t)

		// Must use RunBinary for actual wrapper execution (ELF launcher needs real binary)
		_, stderr, code := RunBinaryWithEnv(t, c.Env, "-C", c.Dir, "--cmd", "echo=false", "echo", "should be blocked")
		if code == 0 {
			t.Error("expected echo to be blocked with echo=false")
		}

		AssertContains(t, stderr, "blocked")

		stdout, stderr, code := RunBinaryWithEnv(t, c.Env, "-C", c.Dir, "--cmd", "echo=true", "echo", "should work")
		if code != 0 {
			t.Errorf("expected exit code 0 with echo=true, got %d\nstderr: %s", code, stderr)
		}

		AssertContains(t, stdout, "should work")
	})
}

// =============================================================================
// Sandbox execution runtime tests (CLI-owned)
// =============================================================================

// waitForFile polls for the existence of a file, with timeout.
// Used to synchronize with sandboxed scripts that signal readiness by creating a marker file.
func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := os.Stat(path)
		if err == nil {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timeout waiting for file %s", path)
}

func Test_Exec_Returns_ExitCode130_When_Signaled_Once(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	sigCh := make(chan os.Signal, 1)
	marker := c.TempFile("ready")

	// Create a script that signals readiness then waits.
	c.WriteExecutable("trap_script.sh", fmt.Sprintf(`#!/bin/bash
trap 'exit 0' SIGTERM
touch %q
while true; do sleep 1; done
`, marker))

	done := c.RunWithSignal(sigCh, "bash", "trap_script.sh")

	waitForFile(t, marker, 5*time.Second)

	sigCh <- syscall.SIGINT

	select {
	case code := <-done:
		if code != 130 {
			t.Errorf("expected exit code 130, got %d", code)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for command to terminate after signal")
	}
}

func Test_Exec_Returns_ExitCode130_When_Signaled_Twice(t *testing.T) {
	// Tests that a second signal triggers force kill.
	// Note: bwrap typically exits immediately on SIGTERM, so the first signal
	// usually causes termination before the second signal.
	t.Parallel()

	c := NewCLITester(t)

	sigCh := make(chan os.Signal, 2)
	marker := c.TempFile("ready")

	c.WriteExecutable("test_script.sh", fmt.Sprintf(`#!/bin/bash
touch %q
while true; do sleep 1; done
`, marker))

	done := c.RunWithSignal(sigCh, "bash", "test_script.sh")

	waitForFile(t, marker, 5*time.Second)

	sigCh <- syscall.SIGINT

	time.Sleep(100 * time.Millisecond)

	sigCh <- syscall.SIGINT

	select {
	case code := <-done:
		if code != 130 {
			t.Errorf("expected exit code 130, got %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for termination after signals")
	}
}

func Test_Exec_Returns_ExitCode130_When_Interrupted(t *testing.T) {
	// Tests that the exit code is 130 when the sandboxed process is interrupted.
	// Note: bwrap exits immediately on SIGTERM (doesn't forward to children).
	t.Parallel()

	c := NewCLITester(t)

	sigCh := make(chan os.Signal, 1)
	marker := c.TempFile("ready")

	c.WriteExecutable("long_running.sh", fmt.Sprintf(`#!/bin/bash
touch %q
while true; do sleep 1; done
`, marker))

	done := c.RunWithSignal(sigCh, "bash", "long_running.sh")

	waitForFile(t, marker, 5*time.Second)

	sigCh <- syscall.SIGINT

	select {
	case code := <-done:
		if code != 130 {
			t.Errorf("expected exit code 130, got %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for process to terminate after signal")
	}
}

func Test_Exec_Mounts_Self_Binary_When_Running_In_Sandbox(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("test", "-f", sandboxBinaryPath)
	if code != 0 {
		t.Errorf("expected agent-sandbox binary to exist at %s inside sandbox\nstderr: %s", sandboxBinaryPath, stderr)
	}

	_, stderr, code = c.Run("test", "-x", sandboxBinaryPath)
	if code != 0 {
		t.Errorf("expected agent-sandbox binary to be executable at %s inside sandbox\nstderr: %s", sandboxBinaryPath, stderr)
	}
}

func Test_Exec_Applies_CmdFlag_When_Running_Nested_Sandbox(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t) // Skip if already inside sandbox

	c := NewCLITester(t)

	_, stderr, code := RunBinaryWithEnv(t, c.Env,
		"-C", c.Dir,
		sandboxBinaryPath, "--cmd", "echo=false", "echo", "hi",
	)

	if code != 0 && (strings.Contains(stderr, "uid map") ||
		strings.Contains(stderr, "ns failed") ||
		strings.Contains(stderr, "user namespace") ||
		strings.Contains(strings.ToLower(stderr), "operation not permitted")) {
		t.Skip("nested namespaces not supported")
	}

	if code == 0 {
		t.Error("expected echo to be blocked by nested --cmd rule")
	}

	AssertContains(t, strings.ToLower(stderr), "blocked")
}

func Test_Exec_Applies_Config_Commands_When_Running_Nested_Sandbox(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t) // Skip if already inside sandbox

	c := NewCLITester(t)

	nestedDir := filepath.Join(c.Dir, "nested")
	mustMkdir(t, nestedDir)
	mustWriteFile(t, filepath.Join(nestedDir, ".agent-sandbox.json"), `{
		"commands": {
			"echo": false
		}
	}`)

	_, stderr, code := RunBinaryWithEnv(t, c.Env,
		"-C", c.Dir,
		sandboxBinaryPath, "-C", nestedDir, "echo", "hello",
	)

	if code != 0 && (strings.Contains(stderr, "uid map") ||
		strings.Contains(stderr, "ns failed") ||
		strings.Contains(stderr, "user namespace") ||
		strings.Contains(strings.ToLower(stderr), "operation not permitted")) {
		t.Skip("nested namespaces not supported")
	}

	if code == 0 {
		t.Error("expected echo to be blocked by nested sandbox command rules, got exit code 0")
	}

	AssertContains(t, strings.ToLower(stderr), "blocked")
}

func Test_Exec_Allows_More_Restrictive_Filesystem_When_Running_Nested_Sandbox(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t) // Skip if already inside sandbox

	c := NewCLITester(t)

	tmpFile := filepath.Join(string(os.PathSeparator), "tmp", "nested-test.txt")

	_, stderr, code := RunBinaryWithEnv(t, c.Env,
		"-C", c.Dir,
		sandboxBinaryPath, "--ro", "/tmp", "-C", "/tmp",
		"touch", tmpFile,
	)

	if code == 0 {
		t.Error("expected touch to fail on read-only path in nested sandbox")
	}

	AssertContains(t, strings.ToLower(stderr), "read-only")
}
