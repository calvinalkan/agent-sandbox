---
schema_version: 1
id: d5g3ek0
status: closed
closed: 2026-01-09T06:10:22Z
blocked-by: [d5g389g, d5g36mg]
created: 2026-01-08T23:01:32Z
type: task
priority: 2
---
# E2E Tests: Docker Socket Access

## Background & Context

The --docker flag enables Docker socket access inside the sandbox. We need E2E
tests that verify Docker commands actually work when enabled.

## Rationale

Docker socket handling has edge cases:
- Socket may be a symlink (needs resolution)
- Socket permissions may vary
- Docker daemon must be running

E2E tests prove the integration actually works.

## Implementation Details

```go
func Test_Sandbox_Docker(t *testing.T) {
    RequireBwrap(t)
    RequireDocker(t)  // Skip if docker not available/running
    
    t.Run("docker works when enabled", func(t *testing.T) {
        dir := t.TempDir()
        
        // Run docker ps inside sandbox with --docker
        _, stderr, exitCode := RunBinary(t, "--cwd", dir, "exec", "--docker", "--", "docker", "ps")
        
        if exitCode != 0 {
            t.Errorf("docker ps failed with exit %d: %s", exitCode, stderr)
        }
    })
    
    t.Run("docker fails when disabled", func(t *testing.T) {
        dir := t.TempDir()
        
        // Run docker ps without --docker flag
        _, _, exitCode := RunBinary(t, "--cwd", dir, "exec", "--", "docker", "ps")
        
        if exitCode == 0 {
            t.Error("docker ps should have failed without --docker flag")
        }
    })
    
    t.Run("docker info works", func(t *testing.T) {
        dir := t.TempDir()
        
        _, stderr, exitCode := RunBinary(t, "--cwd", dir, "exec", "--docker", "--", "docker", "info")
        
        if exitCode != 0 {
            t.Errorf("docker info failed: %s", stderr)
        }
    })
}

// RequireDocker skips test if docker not available
func RequireDocker(t *testing.T) {
    t.Helper()
    
    // Check docker command exists
    if _, err := exec.LookPath("docker"); err != nil {
        t.Skip("docker not available")
    }
    
    // Check docker daemon is running
    cmd := exec.Command("docker", "info")
    if err := cmd.Run(); err != nil {
        t.Skip("docker daemon not running")
    }
}
```

## Files to Create
- cmd/agent-sandbox/e2e_docker_test.go

## Key Invariants
- Tests skip if docker not installed or daemon not running
- Tests verify both enabled and disabled cases
- Simple commands (docker ps, docker info) sufficient for verification

## Acceptance Criteria

1. RequireDocker helper skips if docker unavailable or daemon not running
2. Test: docker ps succeeds with --docker flag
3. Test: docker ps fails without --docker flag
4. Test: docker info succeeds with --docker flag
5. Tests skip gracefully if docker not available
6. All tests pass when docker is available
