package main

import (
	"testing"
)

// ============================================================================
// E2E Tests: Docker Socket Access
// ============================================================================

func Test_Docker_Ps_Works_When_Docker_Flag_Enabled(t *testing.T) {
	t.Parallel()

	RequireDocker(t)

	c := NewCLITester(t)

	// Run docker ps inside sandbox with --docker
	_, stderr, code := c.Run("--docker", "docker", "ps")
	if code != 0 {
		t.Errorf("docker ps failed with exit %d: %s", code, stderr)
	}
}

func Test_Docker_Ps_Fails_When_Docker_Flag_Disabled(t *testing.T) {
	t.Parallel()

	RequireDocker(t)

	c := NewCLITester(t)

	// Run docker ps without --docker flag
	_, _, code := c.Run("docker", "ps")
	if code == 0 {
		t.Error("docker ps should have failed without --docker flag")
	}
}

func Test_Docker_Info_Works_When_Docker_Flag_Enabled(t *testing.T) {
	t.Parallel()

	RequireDocker(t)

	c := NewCLITester(t)

	// Run docker info inside sandbox with --docker
	_, stderr, code := c.Run("--docker", "docker", "info")
	if code != 0 {
		t.Errorf("docker info failed with exit %d: %s", code, stderr)
	}
}

func Test_Docker_Fails_Without_Flag_Even_When_Socket_Exists(t *testing.T) {
	t.Parallel()

	RequireDocker(t)

	c := NewCLITester(t)

	// Verify the socket is masked (file exists but is not a socket)
	// Docker clients will get "not a socket" or connection refused error
	_, stderr, code := c.Run("docker", "version")
	if code == 0 {
		t.Error("docker version should have failed without --docker flag")
	}

	// The error should indicate the socket is not accessible
	// (could be "permission denied", "not a socket", "connection refused", etc.)
	if stderr == "" {
		t.Error("expected error message from docker")
	}
}

func Test_Docker_Version_Works_When_Docker_Flag_Enabled(t *testing.T) {
	t.Parallel()

	RequireDocker(t)

	c := NewCLITester(t)

	// Run docker version inside sandbox with --docker
	stdout, stderr, code := c.Run("--docker", "docker", "version")
	if code != 0 {
		t.Errorf("docker version failed with exit %d: %s", code, stderr)
	}

	// Docker version should output version info
	AssertContains(t, stdout, "Version")
}
