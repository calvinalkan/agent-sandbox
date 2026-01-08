---
schema_version: 1
id: d5g36wg
status: open
blocked-by: [d5g36gr]
created: 2026-01-08T22:45:06Z
type: task
priority: 1
---
# bwrap: Dry Run Mode Implementation

## Background & Context

Per SPEC.md:
> --dry-run: Print bwrap command without executing

This is crucial for:
1. Debugging sandbox configuration
2. Understanding what bwrap sees
3. Testing argument generation without actual execution

Per TECHNICAL_STEERING.md Testing section:
> --dry-run for complex argument verification. When testing intricate bwrap 
> argument generation, use --dry-run and assert on the output.

## Rationale

Users need visibility into what agent-sandbox actually does. Dry run shows the
exact bwrap command that would be executed, allowing:
- Verification of mount points
- Debugging access issues
- Sharing configurations (reproducible)

## Implementation Details

```go
// In cmd_exec.go
func execCommand(ctx context.Context, ...) error {
    // ... config loading and resolution ...
    
    bwrapArgs, err := BwrapArgs(resolvedPaths, cfg)
    if err != nil {
        return err
    }
    
    dryRun, _ := flags.GetBool("dry-run")
    if dryRun {
        // Print the command that would be executed
        fmt.Fprintln(stdout, "bwrap", strings.Join(bwrapArgs, " \\\n  "))
        fmt.Fprintln(stdout, "  --", strings.Join(command, " "))
        return nil
    }
    
    // Actually execute
    return ExecuteSandbox(ctx, bwrapArgs, command, env, stdin, stdout, stderr)
}
```

**Output format:**
Should be copy-pasteable to terminal. Use line continuation for readability:
```
bwrap \
  --ro-bind / / \
  --dev /dev \
  --proc /proc \
  --bind /home/user/project /home/user/project \
  --ro-bind /home/user /home/user \
  --tmpfs /home/user/.ssh \
  --chdir /home/user/project \
  --share-net \
  -- npm install
```

## Files to Modify
- cmd/agent-sandbox/cmd_exec.go - implement dry-run path
- cmd/agent-sandbox/cmd_exec_test.go - test dry-run output

## Key Invariants
- No sandbox execution when --dry-run is set
- Output is valid shell command (copy-pasteable)
- All arguments visible including --
- Exit code 0 for successful dry-run

## Acceptance Criteria

1. --dry-run prints bwrap command without executing
2. Output includes all bwrap arguments
3. Output includes -- separator and user command
4. Output format is shell-compatible (can copy-paste)
5. Exit code 0 for dry-run (even if command would fail)
6. Works with all config combinations
7. Tests verify dry-run output format
