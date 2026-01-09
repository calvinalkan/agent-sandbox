---
schema_version: 1
id: d5g36mg
status: closed
closed: 2026-01-09T03:57:11Z
blocked-by: [d5g36gr]
created: 2026-01-08T22:44:34Z
type: task
priority: 1
---
# bwrap: Network and Docker Socket Handling

## Background & Context

Per SPEC.md:
- --network flag (default: on) controls network access
- --docker flag (default: off) controls Docker socket access

Per SPEC hardcoded behavior:
> Docker socket resolution: Symlinks auto-resolved when --docker enabled

## Rationale

Network isolation prevents:
- Exfiltrating data to external servers
- Downloading malicious payloads
- Accessing internal services

Docker socket access is dangerous because:
- Docker daemon runs as root
- Container escape is trivial with socket access
- Default off is the safe choice

## Implementation Details

**Network:**
```go
// In BwrapArgs
if *cfg.Network {
    args = append(args, "--share-net")
} else {
    // Network is NOT shared by default after --unshare-all
    // No additional args needed
}
```

**Docker:**
```go
// Note: because we bind-mount / (read-only) as the base filesystem, the docker socket
// would otherwise be visible inside the sandbox by default. We must actively *mask* it
// unless --docker is enabled.
const socketPath = "/var/run/docker.sock"

if *cfg.Docker {
    // Resolve to the real socket path (may be a symlink).
    resolved, err := filepath.EvalSymlinks(socketPath)
    if err != nil {
        return nil, fmt.Errorf("docker socket not found at %s: %w", socketPath, err)
    }

    // Overlay the standard path so clients can always use /var/run/docker.sock.
    args = append(args, "--bind", resolved, socketPath)
} else {
    // Mask the socket so docker isn't usable without --docker.
    // Reuse the same unreadable-empty-file mechanism as exclude-file handling (d5g3tgg).
    args = append(args, "--ro-bind", emptyUnreadableFilePath, socketPath)
}
```

**Alternative Docker socket locations:**
- /var/run/docker.sock (standard)
- /run/docker.sock (symlink often)
- User-specific: $XDG_RUNTIME_DIR/docker.sock (rootless Docker)

We should check the standard location first. Rootless docker support can be a follow-up.

## Files to Modify
- cmd/agent-sandbox/bwrap.go - add network and docker handling
- cmd/agent-sandbox/bwrap_test.go - tests

## Key Invariants
- Network off by default means no --share-net (relies on --unshare-all)
- Docker socket symlinks must be resolved
- Docker socket is masked unless enabled
- Docker socket error if enabled but not found

## Acceptance Criteria

**Unit tests (argument generation):**
1. --network=true adds --share-net to bwrap args
2. --network=false omits --share-net (network isolated)
3. --docker=false masks /var/run/docker.sock
4. --docker=true overlays /var/run/docker.sock with the real socket
5. Docker socket symlink resolved before generating mount args
6. Missing docker socket returns clear error
7. Tests verify network and docker arg generation

**E2E tests (deferred to d5g3ek0):**
8. Docker commands actually work inside sandbox with --docker
9. Docker fails without --docker flag
