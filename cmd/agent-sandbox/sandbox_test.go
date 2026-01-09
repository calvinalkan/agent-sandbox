package main

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// ============================================================================
// E2E tests for ExecuteSandbox - environment, exit codes, I/O
// ============================================================================

func Test_Sandbox_Passes_All_Environment_Variables(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Set some custom env vars
	c.Env["MY_CUSTOM_VAR"] = "custom_value_123"
	c.Env["ANOTHER_VAR"] = "another_value"

	// Run printenv inside the sandbox
	stdout, stderr, code := c.Run("printenv")

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Verify custom env vars are present
	if !strings.Contains(stdout, "MY_CUSTOM_VAR=custom_value_123") {
		t.Errorf("expected MY_CUSTOM_VAR in output, got:\n%s", stdout)
	}

	if !strings.Contains(stdout, "ANOTHER_VAR=another_value") {
		t.Errorf("expected ANOTHER_VAR in output, got:\n%s", stdout)
	}

	// Verify standard env vars are present
	if !strings.Contains(stdout, "PATH=") {
		t.Errorf("expected PATH in output, got:\n%s", stdout)
	}

	if !strings.Contains(stdout, "HOME=") {
		t.Errorf("expected HOME in output, got:\n%s", stdout)
	}
}

func Test_Sandbox_Returns_Exit_Code_Zero_On_Success(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	_, stderr, code := c.Run("true")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}
}

func Test_Sandbox_Returns_Exit_Code_One_On_Failure(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	_, _, code := c.Run("false")

	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}
}

func Test_Sandbox_Returns_Custom_Exit_Code(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Use bash to exit with a specific code
	_, _, code := c.Run("bash", "-c", "exit 42")

	if code != 42 {
		t.Errorf("expected exit code 42, got %d", code)
	}
}

func Test_Sandbox_Captures_Stdout(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	stdout, _, code := c.Run("echo", "hello from sandbox")

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	if !strings.Contains(stdout, "hello from sandbox") {
		t.Errorf("expected 'hello from sandbox' in stdout, got: %s", stdout)
	}
}

func Test_Sandbox_Captures_Stderr(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Use bash to write to stderr
	_, stderr, code := c.Run("bash", "-c", "echo 'error message' >&2")

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	if !strings.Contains(stderr, "error message") {
		t.Errorf("expected 'error message' in stderr, got: %s", stderr)
	}
}

func Test_Sandbox_Receives_Stdin(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Use cat to read from stdin and echo it
	stdout, _, code := c.RunWithInput([]string{"input from stdin"}, "cat")

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	if !strings.Contains(stdout, "input from stdin") {
		t.Errorf("expected stdin to be echoed, got: %s", stdout)
	}
}

func Test_Sandbox_Passes_Multiple_Arguments(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Use echo with multiple arguments
	stdout, _, code := c.Run("echo", "arg1", "arg2", "arg3")

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	expected := "arg1 arg2 arg3"
	if !strings.Contains(stdout, expected) {
		t.Errorf("expected %q in stdout, got: %s", expected, stdout)
	}
}

func Test_Sandbox_Passes_Arguments_With_Spaces(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Use echo with an argument containing spaces
	stdout, _, code := c.Run("echo", "hello world with spaces")

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	expected := "hello world with spaces"
	if !strings.Contains(stdout, expected) {
		t.Errorf("expected %q in stdout, got: %s", expected, stdout)
	}
}

func Test_Sandbox_Passes_Arguments_With_Special_Characters(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Use echo with special characters
	stdout, _, code := c.Run("echo", "$VAR", "$(cmd)", "`backticks`")

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	// These should be passed literally, not interpreted
	if !strings.Contains(stdout, "$VAR") {
		t.Errorf("expected $VAR to be passed literally, got: %s", stdout)
	}

	if !strings.Contains(stdout, "$(cmd)") {
		t.Errorf("expected $(cmd) to be passed literally, got: %s", stdout)
	}

	if !strings.Contains(stdout, "`backticks`") {
		t.Errorf("expected `backticks` to be passed literally, got: %s", stdout)
	}
}

func Test_Sandbox_Preserves_Path_Environment(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Verify that PATH is available and common commands work
	stdout, stderr, code := c.Run("which", "ls")

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "ls") {
		t.Errorf("expected path to ls in stdout, got: %s", stdout)
	}
}

func Test_Sandbox_Can_Run_Commands_In_Working_Directory(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a test file in the working directory
	c.WriteFile("test.txt", "test content")

	// Read it from inside the sandbox
	stdout, stderr, code := c.Run("cat", "test.txt")

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "test content") {
		t.Errorf("expected 'test content' in stdout, got: %s", stdout)
	}
}

func Test_Sandbox_Returns_Error_Exit_Code_For_Invalid_Command(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Try to run a non-existent command
	_, _, code := c.Run("nonexistent_command_xyz")

	// Should fail with non-zero exit code (typically 127 for command not found)
	if code == 0 {
		t.Errorf("expected non-zero exit code for invalid command, got %d", code)
	}
}

func Test_Sandbox_Terminates_On_Signal(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a signal channel
	sigCh := make(chan os.Signal, 1)

	// Create a script that traps SIGTERM and exits cleanly
	c.WriteExecutable("trap_script.sh", `#!/bin/bash
trap 'exit 0' SIGTERM
while true; do sleep 1; done
`)

	// Start the script inside the sandbox
	done := c.RunWithSignal(sigCh, "bash", "trap_script.sh")

	// Give it a moment to start
	time.Sleep(200 * time.Millisecond)

	// Send interrupt signal
	sigCh <- syscall.SIGINT

	// Wait for exit with timeout - agent-sandbox exits with 130 on interrupt
	select {
	case code := <-done:
		// Should exit with code 130 (interrupted)
		if code != 130 {
			t.Errorf("expected exit code 130, got %d", code)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for command to terminate after signal")
	}
}
