---
schema_version: 1
id: d5g36rg
status: closed
closed: 2026-01-09T04:15:23Z
blocked-by: [d5g36mg]
created: 2026-01-08T22:44:50Z
type: task
priority: 1
---
# bwrap: Command Execution and Environment Passthrough

## Background & Context

Per SPEC.md:
> **Environment Variables:** All environment variables from the parent process 
> are passed through to the sandboxed process unchanged.

And the exit code semantics:
> 0 = Success
> 1 = Error
> 130 = Interrupted

## Rationale

Environment passthrough is essential for:
- PATH (finding executables)
- HOME, USER (user identity)
- TERM (terminal capabilities)
- Language/locale settings
- Tool-specific configs (EDITOR, PAGER, etc.)

The sandboxed process should feel like normal execution, just with filesystem
restrictions.

## Implementation Details

```go
// ExecuteSandbox runs bwrap with the generated arguments
func ExecuteSandbox(
    ctx context.Context,
    bwrapArgs []string,
    command []string,
    env map[string]string,
    stdin io.Reader,
    stdout, stderr io.Writer,
) (int, error) {
    // Build full bwrap command
    args := append(bwrapArgs, "--")
    args = append(args, command...)
    
    cmd := exec.CommandContext(ctx, "bwrap", args...)
    cmd.Stdin = stdin
    cmd.Stdout = stdout
    cmd.Stderr = stderr
    
    // Pass all environment variables
    cmd.Env = make([]string, 0, len(env))
    for k, v := range env {
        cmd.Env = append(cmd.Env, k+"="+v)
    }
    
    err := cmd.Run()
    if err != nil {
        var exitErr *exec.ExitError
        if errors.As(err, &exitErr) {
            return exitErr.ExitCode(), nil
        }
        return 1, err
    }
    
    return 0, nil
}
```

**Signal handling:**
The parent signal handling in Run() already cancels the context. We need to ensure
the context cancellation is forwarded to bwrap properly. exec.CommandContext
handles SIGKILL on context cancel, but we want graceful SIGTERM first.

This is handled by the existing signal handling in run.go, but we may need to
send SIGTERM to the child process explicitly.

## Files to Modify
- cmd/agent-sandbox/bwrap.go - add ExecuteSandbox function
- cmd/agent-sandbox/bwrap_test.go - E2E tests with real bwrap

## Key Invariants
- All environment variables passed through
- Command and args passed correctly after --
- Exit code from sandboxed process returned
- Context cancellation stops the sandbox
- stdin/stdout/stderr connected correctly

## Acceptance Criteria

1. All parent env vars available in sandbox
2. Command receives all its arguments
3. Exit code from sandboxed command returned
4. stdin connected to sandboxed process
5. stdout/stderr from sandbox reaches parent
6. Context cancellation terminates sandbox
7. E2E test: run simple command in sandbox
8. E2E test: verify env var passthrough
