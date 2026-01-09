---
schema_version: 1
id: d5g37yg
status: closed
closed: 2026-01-09T04:32:04Z
blocked-by: [d5g36rg]
created: 2026-01-08T22:47:22Z
type: task
priority: 1
---
# bwrap: Mount agent-sandbox Binary Into Sandbox

## Background & Context

Per TECHNICAL_STEERING.md:
> Mount agent-sandbox itself into the sandbox:
> 1. Use os.Executable() to find our own binary
> 2. Mount it into the sandbox (e.g., /usr/bin/agent-sandbox) with --ro-bind

This is required for:
1. The wrap-binary command to work (needs agent-sandbox available)
2. Nested sandbox calls to work
3. Check command to work inside sandbox

Per SPEC hardcoded behavior:
> Nested sandboxes: Running agent-sandbox inside a sandbox works without 
> special handling

## Rationale

The agent-sandbox binary must be accessible inside the sandbox for:
- Command wrappers (wrapper scripts exec the sandbox runtime `wrap-binary` subcommand; no `$PATH` lookup)
- Users running `agent-sandbox check` inside sandbox
- Potential nested sandbox scenarios

By mounting our own binary read-only, we ensure it's available but can't be
modified.

## Implementation Details

```go
// In bwrap.go, during arg generation

func mountSelfBinary() ([]string, error) {
    // Find our own executable
    self, err := os.Executable()
    if err != nil {
        return nil, fmt.Errorf("finding agent-sandbox binary: %w", err)
    }
    
    // Resolve any symlinks
    self, err = filepath.EvalSymlinks(self)
    if err != nil {
        return nil, fmt.Errorf("resolving agent-sandbox binary path: %w", err)
    }
    
    // Mount at standard location
    const sandboxBinaryPath = "/usr/bin/agent-sandbox"
    
    return []string{
        "--ro-bind", self, sandboxBinaryPath,
    }, nil
}
```

**Wrapper integration note (d5g39b0):**
The wrapper system also mounts a copy of the agent-sandbox binary at a per-sandbox
runtime path (e.g., `/run/<random>/agent-sandbox/binaries/wrap-binary`) so the
hidden `wrap-binary` subcommand can locate real binaries at `../real/<name>` via
the TECHNICAL_STEERING.md path convention.

## Files to Modify
- cmd/agent-sandbox/bwrap.go - add self-mounting
- cmd/agent-sandbox/bwrap_test.go - verify self-mount args

## Key Invariants
- Binary mounted read-only
- Path is consistent (/usr/bin/agent-sandbox)
- Works even if original path has spaces
- Symlinks resolved to get real binary path

## Acceptance Criteria

1. agent-sandbox binary mounted into sandbox at /usr/bin/agent-sandbox
2. Mount is read-only (--ro-bind)
3. Works when agent-sandbox is invoked via symlink
4. Works when agent-sandbox path contains spaces
5. agent-sandbox check works inside sandbox
6. Nested agent-sandbox exec works inside sandbox
7. Tests verify bwrap args include self-mount
