---
schema_version: 1
id: d5g38ng
status: closed
closed: 2026-01-09T06:03:58Z
blocked-by: [d5g389g, d5g37tr]
created: 2026-01-08T22:48:54Z
type: task
priority: 2
---
# E2E Tests: Check Command and Sandbox Detection

## Background & Context

The check command detects whether we're inside a sandbox. E2E tests must verify:
- Detection works correctly
- Detection is tamperproof
- Nested sandbox scenarios work

## Rationale

The check command is the primary way for scripts to detect sandbox status.
We must verify it works correctly in real sandbox scenarios.

## Implementation Details

**Important:** These tests need the compiled binary (not just `Run()`) because
we're testing `agent-sandbox check` running INSIDE a sandbox, which requires
the binary to be mounted. Use `RunBinary()` from test helpers (see d5g39g8).

```go
func Test_Check_Command_E2E(t *testing.T) {
    RequireBwrap(t)
    
    // Test: check outside sandbox (can use Run() or RunBinary())
    t.Run("outside sandbox returns exit 1", func(t *testing.T) {
        stdout, _, exit := RunBinary(t, "check")
        
        if exit != 1 {
            t.Errorf("expected exit 1 outside sandbox, got %d", exit)
        }
        if !strings.Contains(stdout, "outside sandbox") {
            t.Errorf("unexpected output: %q", stdout)
        }
    })
    
    // Test: check inside sandbox - MUST use binary path
    t.Run("inside sandbox returns exit 0", func(t *testing.T) {
        // Run: agent-sandbox exec -- /usr/bin/agent-sandbox check
        stdout, stderr, exit := RunBinary(t, "exec", "--", "/usr/bin/agent-sandbox", "check")
        
        if exit != 0 {
            t.Errorf("expected exit 0 inside sandbox, got %d: %s", exit, stderr)
        }
        if !strings.Contains(stdout, "inside sandbox") {
            t.Errorf("unexpected output: %q", stdout)
        }
    })
    
    // Test: quiet mode
    t.Run("quiet mode no output", func(t *testing.T) {
        stdout, _, exit := RunBinary(t, "exec", "--", "/usr/bin/agent-sandbox", "check", "-q")
        
        if stdout != "" {
            t.Errorf("expected no output with -q, got %q", stdout)
        }
        if exit != 0 {
            t.Errorf("expected exit 0, got %d", exit)
        }
    })
    
    // Test: cannot fake detection
    t.Run("detection cannot be faked", func(t *testing.T) {
        // Try to remove/replace the marker inside sandbox, verify check still works
        stdout, _, exit := RunBinary(t, "exec", "--", "sh", "-c",
            "rm -f /run/agent-sandbox/.marker 2>/dev/null || true; /usr/bin/agent-sandbox check")
        // Should still report inside sandbox (marker is a read-only bind mount)
        if exit != 0 || !strings.Contains(stdout, "inside sandbox") {
            t.Error("detection should not be fakeable")
        }
    })
}
```

## Files to Create
- cmd/agent-sandbox/e2e_check_test.go - E2E check command tests

## Key Invariants
- check works outside sandbox (exit 1)
- check works inside sandbox (exit 0)
- Quiet mode suppresses output
- Detection cannot be faked from inside

## Acceptance Criteria

1. check returns exit 1 outside sandbox with "outside sandbox" output
2. check returns exit 0 inside sandbox with "inside sandbox" output
3. check -q returns no output (just exit code)
4. Cannot fake detection by creating marker file from inside
5. Nested sandbox calls work (sandbox inside sandbox)
6. Tests skip gracefully if bwrap not available
