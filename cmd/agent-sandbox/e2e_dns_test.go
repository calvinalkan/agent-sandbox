package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// E2E Tests: DNS Resolution
//
// These tests verify that DNS resolution works inside the sandbox.
// This is a regression test for the issue where /etc/resolv.conf is a symlink
// to /run/systemd/resolve/stub-resolv.conf, but /run is mounted as tmpfs,
// breaking the symlink and causing DNS failures.
// ============================================================================

// Test_Sandbox_DNS_ResolvConf_Accessible verifies that /etc/resolv.conf
// (or its symlink target) is accessible inside the sandbox.
func Test_Sandbox_DNS_ResolvConf_Accessible(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Check if /etc/resolv.conf exists and is readable
	stdout, stderr, code := c.Run("cat", "/etc/resolv.conf")

	if code != 0 {
		t.Errorf("/etc/resolv.conf should be accessible inside sandbox\nstderr: %s", stderr)
	}

	// Should contain nameserver directive
	if !strings.Contains(stdout, "nameserver") {
		t.Errorf("expected 'nameserver' in /etc/resolv.conf, got: %s", stdout)
	}
}

// Test_Sandbox_DNS_Resolution_Works verifies that DNS resolution works
// when network is enabled (default).
func Test_Sandbox_DNS_Resolution_Works(t *testing.T) {
	t.Parallel()

	// Skip if getent is not available
	_, err := os.Stat("/usr/bin/getent")
	if os.IsNotExist(err) {
		t.Skip("getent not available")
	}

	c := NewCLITester(t)

	// Use getent to resolve a well-known domain
	// This uses the system resolver (reads /etc/resolv.conf)
	stdout, stderr, code := c.Run("getent", "hosts", "google.com")

	if code != 0 {
		t.Errorf("DNS resolution should work inside sandbox with network enabled\nstderr: %s", stderr)
	}

	// Should contain an IP address
	if !strings.Contains(stdout, ".") {
		t.Errorf("expected IP address in getent output, got: %s", stdout)
	}
}

// Test_Sandbox_DNS_Python_Resolves_Domain verifies DNS works via Python.
func Test_Sandbox_DNS_Python_Resolves_Domain(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Use Python to resolve a domain - this exercises the full DNS stack
	pythonCmd := `python3 -c "import socket; print(socket.gethostbyname('google.com'))"`
	stdout, stderr, code := c.Run("bash", "-c", pythonCmd)

	if code != 0 {
		t.Errorf("Python DNS resolution should work inside sandbox\nstderr: %s", stderr)
	}

	// Should print an IP address
	stdout = strings.TrimSpace(stdout)
	if !strings.Contains(stdout, ".") || len(stdout) < 7 {
		t.Errorf("expected IP address, got: %q", stdout)
	}
}

// Test_Sandbox_DNS_Curl_Works verifies curl can resolve domains.
func Test_Sandbox_DNS_Curl_Works(t *testing.T) {
	t.Parallel()

	// Skip if curl is not available
	_, err := os.Stat("/usr/bin/curl")
	if os.IsNotExist(err) {
		t.Skip("curl not available")
	}

	c := NewCLITester(t)

	// Try to fetch headers from a well-known domain
	stdout, stderr, code := c.Run("curl", "-s", "-I", "--connect-timeout", "5", "https://google.com")

	if code != 0 {
		// Check for specific DNS failure
		if strings.Contains(stderr, "Could not resolve host") {
			t.Errorf("DNS resolution failed in sandbox - /etc/resolv.conf may be broken\nstderr: %s", stderr)
		} else {
			t.Errorf("curl failed inside sandbox\nstderr: %s", stderr)
		}
	}

	// Should contain HTTP response
	if !strings.Contains(stdout, "HTTP/") {
		t.Errorf("expected HTTP response, got: %s", stdout)
	}
}

// Test_Sandbox_DNS_SystemdResolved_Symlink_Preserved verifies that when
// /etc/resolv.conf is a symlink to /run/systemd/resolve/*, the target
// is accessible inside the sandbox.
func Test_Sandbox_DNS_SystemdResolved_Symlink_Preserved(t *testing.T) {
	t.Parallel()

	// Check if we're on a systemd-resolved system
	target, err := os.Readlink("/etc/resolv.conf")
	if err != nil {
		t.Skip("/etc/resolv.conf is not a symlink")
	}

	if !strings.Contains(target, "/run/systemd/resolve") {
		t.Skip("/etc/resolv.conf does not point to systemd-resolved")
	}

	// Resolve the full path
	resolved, err := filepath.EvalSymlinks("/etc/resolv.conf")
	if err != nil {
		t.Fatalf("failed to resolve /etc/resolv.conf symlink: %v", err)
	}

	c := NewCLITester(t)

	// The resolved path should be accessible inside the sandbox
	stdout, stderr, code := c.Run("cat", resolved)

	if code != 0 {
		t.Errorf("systemd-resolved config at %s should be accessible inside sandbox\nstderr: %s", resolved, stderr)
	}

	// Should contain nameserver
	if !strings.Contains(stdout, "nameserver") {
		t.Errorf("expected 'nameserver' in %s, got: %s", resolved, stdout)
	}
}

// Test_Sandbox_DNS_NSSwitch_Accessible verifies /etc/nsswitch.conf is accessible.
// This file controls how name resolution is performed.
func Test_Sandbox_DNS_NSSwitch_Accessible(t *testing.T) {
	t.Parallel()

	// Skip if nsswitch.conf doesn't exist
	_, err := os.Stat("/etc/nsswitch.conf")
	if os.IsNotExist(err) {
		t.Skip("/etc/nsswitch.conf not present")
	}

	c := NewCLITester(t)

	stdout, stderr, code := c.Run("cat", "/etc/nsswitch.conf")

	if code != 0 {
		t.Errorf("/etc/nsswitch.conf should be accessible inside sandbox\nstderr: %s", stderr)
	}

	// Should contain hosts line
	if !strings.Contains(stdout, "hosts:") {
		t.Errorf("expected 'hosts:' in /etc/nsswitch.conf, got: %s", stdout)
	}
}
