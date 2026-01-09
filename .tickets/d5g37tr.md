---
schema_version: 1
id: d5g37tr
status: closed
closed: 2026-01-09T04:59:30Z
blocked-by: [d5g36rg, d5g37yg]
created: 2026-01-08T22:47:07Z
type: task
priority: 1
---
# Check Command: Sandbox Detection via Marker File

## Background & Context

Per SPEC.md:
> agent-sandbox check [-q]
> Checks if the current process is running inside an agent-sandbox.

The detection must be tamperproof:
> The detection mechanism is tamperproof — it cannot be faked or disabled from 
> inside the sandbox.

Per TECHNICAL_STEERING.md:
> Tamperproof detection via marker file:
> 1. Mount a read-only marker file at a known path (e.g., /.agent-sandbox)
> 2. check command simply tests if the marker exists
>
> Since agent-sandbox is a compiled Go binary, it can be mounted read-only.
> The marker file cannot be created or removed from inside the sandbox.

## Rationale

The check command enables scripts to detect if they're sandboxed:
```bash
if agent-sandbox check -q; then
    # Inside sandbox - adapt behavior
else
    # Outside sandbox - maybe refuse to run
fi
```

This is useful for:
- Safety scripts that should only run unsandboxed
- Tools that need to know their security context
- Testing sandbox behavior programmatically

## Implementation Details

**Marker file approach using /dev/null:**

Mount a read-only marker at a path that is *not user-writable on the host* so the
result cannot be faked when running outside the sandbox.

**Important:** because we mount the host root filesystem read-only (`--ro-bind / /`),
we cannot mount a marker at a brand-new root path like `/.agent-sandbox` (bwrap would
need to create the mountpoint file on a read-only filesystem). Instead, place the
marker under `/run`, and mount `/run` as tmpfs inside the sandbox.

```go
const sandboxMarkerPath = "/run/agent-sandbox/.marker"

func isInsideSandbox() bool {
    _, err := os.Stat(sandboxMarkerPath)
    return err == nil
}

// CheckCmd implementation
func CheckCmd() *Command {
    // ...
    Exec: func(...) error {
        quiet, _ := flags.GetBool("quiet")
        inside := isInsideSandbox()
        
        if !quiet {
            if inside {
                fprintln(stdout, "inside sandbox")
            } else {
                fprintln(stdout, "outside sandbox")
            }
        }
        
        if inside {
            return nil // exit 0
        }
        return ErrSilentExit // exit 1
    }
}
```

**bwrap args for marker (no temp file needed):**
```go
bwrapArgs = append(bwrapArgs,
    "--tmpfs", "/run",
    "--ro-bind", "/dev/null", "/run/agent-sandbox/.marker",
)
```

**Why this works:**
- /dev/null always exists, no temp file creation needed
- Marker lives at a path that a normal user cannot create on the host (`/run/...`)
- --ro-bind mounts /dev/null as the marker (read-only)
- Cannot be removed from inside the sandbox (read-only bind mount)
- Check is simple: does /run/agent-sandbox/.marker exist?

## Files to Modify
- cmd/agent-sandbox/cmd_check.go - implement detection
- cmd/agent-sandbox/bwrap.go - add marker file mounting
- cmd/agent-sandbox/cmd_check_test.go - tests (requires actual sandbox)

## Key Invariants
- Uses /dev/null as marker source (no temp file needed)
- Marker at /run/agent-sandbox/.marker
- Detection is just os.Stat (no complex logic)
- Cannot be removed from inside sandbox (read-only bind mount)
- Works regardless of working directory

## Acceptance Criteria

1. check returns exit 0 inside sandbox
2. check returns exit 1 outside sandbox
3. Without -q: prints "inside sandbox" or "outside sandbox"
4. With -q: no output, just exit code
5. Marker mounted using `--tmpfs /run` and `--ro-bind /dev/null /run/agent-sandbox/.marker`
6. Cannot fake detection from inside sandbox
7. E2E test: run check inside actual sandbox
8. E2E test: run check outside sandbox
