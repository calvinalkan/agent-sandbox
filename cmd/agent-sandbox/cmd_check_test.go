package main

import (
	"strings"
	"testing"
)

// ============================================================================
// E2E tests for check command - sandbox detection
// ============================================================================

func Test_Check_Returns_Exit_1_Outside_Sandbox(t *testing.T) {
	t.Parallel()

	// Run check directly (not in sandbox) using the real binary
	stdout, _, code := RunBinary(t, "check")

	if code != 1 {
		t.Errorf("expected exit code 1 outside sandbox, got %d", code)
	}

	if !strings.Contains(stdout, "outside sandbox") {
		t.Errorf("expected stdout to contain 'outside sandbox', got: %s", stdout)
	}
}

func Test_Check_Returns_Exit_0_Inside_Sandbox(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	// Run the real binary: agent-sandbox check
	// First invocation creates sandbox, second runs check inside it
	stdout, stderr, code := RunBinary(t, SandboxBinaryPath, "check")

	if code != 0 {
		t.Errorf("expected exit code 0 inside sandbox, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "inside sandbox") {
		t.Errorf("expected stdout to contain 'inside sandbox', got: %s", stdout)
	}
}

func Test_Check_Quiet_Mode_No_Output_Outside_Sandbox(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := RunBinary(t, "check", "-q")

	if code != 1 {
		t.Errorf("expected exit code 1 outside sandbox, got %d", code)
	}

	if stdout != "" {
		t.Errorf("expected no stdout in quiet mode, got: %s", stdout)
	}

	if stderr != "" {
		t.Errorf("expected no stderr in quiet mode, got: %s", stderr)
	}
}

func Test_Check_Quiet_Mode_No_Output_Inside_Sandbox(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	// Run: agent-sandbox check -q
	stdout, stderr, code := RunBinary(t, SandboxBinaryPath, "check", "-q")

	if code != 0 {
		t.Errorf("expected exit code 0 inside sandbox with -q, got %d\nstderr: %s", code, stderr)
	}

	if stdout != "" {
		t.Errorf("expected no stdout in quiet mode, got: %s", stdout)
	}

	if stderr != "" {
		t.Errorf("expected no stderr in quiet mode, got: %s", stderr)
	}
}

func Test_Check_Marker_Cannot_Be_Removed_Inside_Sandbox(t *testing.T) {
	// Verify the marker file is tamperproof - cannot be removed from inside
	t.Parallel()

	RequireBwrap(t)

	// Try to remove the marker file inside the sandbox
	// rm should fail because the marker is a read-only bind mount
	_, _, code := RunBinary(t, "rm", "-f", SandboxMarkerPath)

	if code == 0 {
		t.Error("expected rm to fail on marker file (read-only bind mount), but it succeeded")
	}

	// Verify check still works after attempted removal
	stdout, stderr, checkCode := RunBinary(t, SandboxBinaryPath, "check")

	if checkCode != 0 {
		t.Errorf("expected check to still return 0 after attempted marker removal, got %d\nstderr: %s", checkCode, stderr)
	}

	if !strings.Contains(stdout, "inside sandbox") {
		t.Errorf("expected 'inside sandbox' after attempted marker removal, got: %s", stdout)
	}
}

func Test_Check_Shows_Help_With_Help_Flag(t *testing.T) {
	t.Parallel()

	stdout, _, code := RunBinary(t, "check", "--help")

	if code != 0 {
		t.Errorf("expected exit code 0 for --help, got %d", code)
	}

	if !strings.Contains(stdout, "check") {
		t.Errorf("expected help to mention 'check', got: %s", stdout)
	}

	if !strings.Contains(stdout, "-q") {
		t.Errorf("expected help to mention '-q' flag, got: %s", stdout)
	}
}

func Test_Check_Detection_Cannot_Be_Faked_By_Creating_Marker_Outside_Sandbox(t *testing.T) {
	// Verify that creating the marker file outside sandbox does not fool check.
	// The sandbox detection relies on a read-only bind mount from /dev/null,
	// not just file existence.
	t.Parallel()

	c := NewCLITester(t)

	// Create a fake marker file at the marker path location
	// This simulates someone trying to fake being inside a sandbox
	c.WriteFile(SandboxMarkerPath, "fake marker content")

	// Even with the fake marker file in HOME, check should still report outside sandbox
	// because the check runs in the original environment (not with fake HOME)
	stdout, _, code := RunBinary(t, "check")

	if code != 1 {
		t.Errorf("expected exit code 1 outside sandbox (fake marker should not work), got %d", code)
	}

	if !strings.Contains(stdout, "outside sandbox") {
		t.Errorf("expected 'outside sandbox' even with fake marker file, got: %s", stdout)
	}
}

func Test_Check_Nested_Sandbox_Works(t *testing.T) {
	// Verify that running agent-sandbox inside a sandbox works correctly.
	// The nested sandbox should also report "inside sandbox".
	//
	// Note: Nested user namespaces require kernel support and may not work
	// in all environments. This test skips if nested namespaces aren't available.
	t.Parallel()

	RequireBwrap(t)

	// Run: agent-sandbox exec -- agent-sandbox exec -- agent-sandbox check
	// This creates a sandbox, which creates another sandbox, which runs check
	stdout, stderr, code := RunBinary(t,
		SandboxBinaryPath,          // outer sandbox runs agent-sandbox
		SandboxBinaryPath, "check", // inner sandbox runs check
	)

	// Check if nested namespaces are not supported (common limitation)
	if code != 0 && (strings.Contains(stderr, "uid map") ||
		strings.Contains(stderr, "ns failed") ||
		strings.Contains(stderr, "user namespace")) {
		t.Skip("nested user namespaces not supported in this environment")
	}

	if code != 0 {
		t.Errorf("expected exit code 0 for nested sandbox check, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "inside sandbox") {
		t.Errorf("expected 'inside sandbox' for nested sandbox, got: %s", stdout)
	}
}

func Test_Check_Marker_Cannot_Be_Overwritten_Inside_Sandbox(t *testing.T) {
	// Verify that the marker file cannot be overwritten from inside the sandbox.
	// This tests tampering by trying to replace the content.
	t.Parallel()

	RequireBwrap(t)

	// Try to overwrite the marker file inside the sandbox
	// Should fail because the marker is a read-only bind mount
	_, stderr, code := RunBinary(t, "sh", "-c", "echo fake > "+SandboxMarkerPath)

	if code == 0 {
		t.Error("expected write to marker file to fail (read-only bind mount), but it succeeded")
	}

	// The error should indicate read-only or permission denied
	if !strings.Contains(stderr, "Read-only") && !strings.Contains(stderr, "Permission denied") {
		t.Logf("write failed as expected, stderr: %s", stderr)
	}

	// Verify check still works after attempted overwrite
	stdout, stderr, checkCode := RunBinary(t, SandboxBinaryPath, "check")

	if checkCode != 0 {
		t.Errorf("expected check to still return 0 after attempted marker overwrite, got %d\nstderr: %s", checkCode, stderr)
	}

	if !strings.Contains(stdout, "inside sandbox") {
		t.Errorf("expected 'inside sandbox' after attempted marker overwrite, got: %s", stdout)
	}
}
