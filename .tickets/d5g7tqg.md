---
schema_version: 1
id: d5g7tqg
status: open
blocked-by: [d5g389g, d5g36mg]
created: 2026-01-09T04:00:30Z
type: task
priority: 2
---
# E2E Tests: Network Isolation

## Background & Context

The --network flag (default: true) controls network access inside the sandbox.
When disabled, the sandbox should have no network connectivity. We need E2E
tests that verify network requests actually fail when network is disabled.

## Rationale

Unit tests verify that `--share-net` is omitted from bwrap args when network
is disabled, but they don't prove the sandbox actually blocks network access.
E2E tests prove the isolation works in practice.

## Implementation Details

```go
func Test_Sandbox_Network_Isolation(t *testing.T) {
    RequireBwrap(t)

    t.Run("network works when enabled", func(t *testing.T) {
        dir := t.TempDir()

        // Attempt to fetch a reliable endpoint
        stdout, stderr, exitCode := RunBinary(t, "--cwd", dir, "--network=true",
            "curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "https://example.com")

        if exitCode != 0 {
            t.Errorf("curl should succeed with network enabled: %s", stderr)
        }
        if strings.TrimSpace(stdout) != "200" {
            t.Errorf("expected HTTP 200, got %s", stdout)
        }
    })

    t.Run("network fails when disabled via CLI", func(t *testing.T) {
        dir := t.TempDir()

        // With --network=false, network requests should fail
        _, _, exitCode := RunBinary(t, "--cwd", dir, "--network=false",
            "curl", "-s", "--connect-timeout", "2", "https://example.com")

        if exitCode == 0 {
            t.Error("curl should have failed with network disabled")
        }
    })

    t.Run("network fails when disabled via config", func(t *testing.T) {
        dir := t.TempDir()
        configPath := filepath.Join(dir, ".agent-sandbox.json")
        os.WriteFile(configPath, []byte(`{"network": false}`), 0644)

        // Config disables network
        _, _, exitCode := RunBinary(t, "--cwd", dir,
            "curl", "-s", "--connect-timeout", "2", "https://example.com")

        if exitCode == 0 {
            t.Error("curl should have failed with network disabled via config")
        }
    })
}
```

**Alternative: Use a simple Go program instead of curl**

If curl isn't available, write a minimal Go test binary:

```go
// testdata/netcheck/main.go
package main

import (
    "net/http"
    "os"
    "time"
)

func main() {
    client := &http.Client{Timeout: 2 * time.Second}
    _, err := client.Get("https://example.com")
    if err != nil {
        os.Exit(1)
    }
    os.Exit(0)
}
```

Then the test builds and uses this binary inside the sandbox.

## Files to Create
- cmd/agent-sandbox/e2e_network_test.go

## Key Invariants
- Tests skip if bwrap not available
- Use short timeouts to fail fast when network is blocked
- Test both CLI flag and config file paths
- Network enabled (default) should work
- Network disabled should fail immediately (not timeout)

## Acceptance Criteria

1. Test: network request succeeds with --network=true
2. Test: network request fails with --network=false (CLI flag)
3. Test: network request fails when config has network: false
4. Tests use short timeouts (2s) to fail fast
5. Tests skip gracefully if bwrap not available
6. All tests pass when run with bwrap available
