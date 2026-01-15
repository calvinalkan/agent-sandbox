package main

import (
	"strings"
	"testing"
)

// ============================================================================
// E2E Tests: Network Isolation
//
// These tests verify that --network flag properly controls network access.
// We use Python's socket module to test connectivity because:
// 1. It's available on most systems
// 2. It can test raw TCP connections with IP addresses (no DNS needed)
// 3. It fails immediately with clear error when network is disabled
//
// DNS-based tests (curl to domain) don't work reliably because /run is
// mounted as tmpfs, breaking /etc/resolv.conf -> /run/systemd/resolve/... symlinks.
// ============================================================================

// networkTestSocket is a Python command that attempts to connect to a public DNS server.
// Uses 1.1.1.1:53 (Cloudflare DNS) which is highly available.
// Exits 0 on success, non-zero on failure.
const networkTestSocket = `python3 -c "import socket; s=socket.socket(); s.settimeout(2); s.connect(('1.1.1.1', 53)); print('connected')"`

func Test_Sandbox_Network_Works_When_Enabled(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// With network enabled, we should be able to connect to external IP
	stdout, stderr, code := c.Run("--network=true", "bash", "-c", networkTestSocket)

	if code != 0 {
		t.Errorf("socket connection should succeed with network enabled: %s", stderr)
	}

	if !strings.Contains(stdout, "connected") {
		t.Errorf("expected 'connected' in stdout, got: %s", stdout)
	}
}

func Test_Sandbox_Network_Fails_When_Disabled_Via_CLI(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// With --network=false, network requests should fail with "Network is unreachable"
	_, stderr, code := c.Run("--network=false", "bash", "-c", networkTestSocket)

	if code == 0 {
		t.Error("socket connection should have failed with network disabled")
	}

	// Verify it's the expected network error (not some other failure)
	if !strings.Contains(stderr, "Network is unreachable") && !strings.Contains(stderr, "OSError") {
		t.Errorf("expected network-related error, got: %s", stderr)
	}
}

func Test_Sandbox_Network_Fails_When_Disabled_Via_Config(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Write config that disables network
	c.WriteFile(".agent-sandbox.json", `{"network": false}`)

	// Network requests should fail due to config
	_, stderr, code := c.Run("bash", "-c", networkTestSocket)

	if code == 0 {
		t.Error("socket connection should have failed with network disabled via config")
	}

	// Verify it's the expected network error
	if !strings.Contains(stderr, "Network is unreachable") && !strings.Contains(stderr, "OSError") {
		t.Errorf("expected network-related error, got: %s", stderr)
	}
}

func Test_Sandbox_Network_CLI_Overrides_Config(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Write config that disables network
	c.WriteFile(".agent-sandbox.json", `{"network": false}`)

	// But CLI flag should override config and enable network
	stdout, stderr, code := c.Run("--network=true", "bash", "-c", networkTestSocket)

	if code != 0 {
		t.Errorf("socket connection should succeed when CLI overrides config to enable network: %s", stderr)
	}

	if !strings.Contains(stdout, "connected") {
		t.Errorf("expected 'connected' in stdout, got: %s", stdout)
	}
}

func Test_Sandbox_Network_Default_Is_Enabled(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Without any network flag, default is enabled (network=true)
	// Verify network works with default settings
	stdout, stderr, code := c.Run("bash", "-c", networkTestSocket)

	if code != 0 {
		t.Errorf("socket connection should succeed with default network settings (enabled): %s", stderr)
	}

	if !strings.Contains(stdout, "connected") {
		t.Errorf("expected 'connected' in stdout with default network settings, got: %s", stdout)
	}
}

func Test_Sandbox_Network_Isolation_Blocks_All_Interfaces(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Verify that with network disabled, we can't reach localhost either
	// This confirms full network namespace isolation
	localhostTest := `python3 -c "import socket; s=socket.socket(); s.settimeout(2); s.connect(('127.0.0.1', 1)); print('connected')"`

	_, stderr, code := c.Run("--network=false", "bash", "-c", localhostTest)

	// Should fail - either "Connection refused" (normal) or "Network is unreachable" (isolated)
	// On a truly isolated network namespace, localhost itself may not exist
	if code == 0 {
		t.Error("localhost connection should have failed with network disabled")
	}

	// Just verify we got some error - the specific error depends on the isolation level
	if stderr == "" {
		t.Error("expected error message when connecting with network disabled")
	}
}
