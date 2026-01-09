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
