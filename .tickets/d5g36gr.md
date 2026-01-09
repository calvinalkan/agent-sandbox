---
schema_version: 1
id: d5g36gr
status: closed
closed: 2026-01-09T03:36:10Z
blocked-by: [d5g36br]
created: 2026-01-08T22:44:19Z
type: task
priority: 1
---
# bwrap: Core Argument Generation for Filesystem Mounts

## Background & Context

This task converts resolved paths (with final access levels) into bwrap arguments.
Per TECHNICAL_STEERING.md, agent-sandbox is "a wrapper around bwrap" - the core
job is translating our config model into bwrap's mount arguments.

bwrap mount argument types:
- --ro-bind SRC DEST : Read-only bind mount
- --bind SRC DEST : Read-write bind mount  
- --tmpfs DEST : Ephemeral tmpfs (used for excluded directories)
- --dev DEST : Virtual /dev
- --proc DEST : Virtual /proc

## Rationale

The filesystem model must translate cleanly to bwrap:
- ro paths → --ro-bind
- rw paths → --bind
- exclude paths → special handling (see below)

**Exclude handling:**
Exclude is implemented via bwrap mounts, but *directories* and *files* need different handling
(see d5g3tgg). This ticket focuses on the core ro/rw mount generation and leaves
exclude handling to d5g3tgg.

## Implementation Details

```go
// BwrapArgs generates bwrap arguments from resolved paths
func BwrapArgs(paths []ResolvedPath, cfg *Config) ([]string, error) {
    var args []string
    
    // Process cleanup and namespace setup first
    args = append(args, "--die-with-parent")  // Auto-cleanup when parent dies
    args = append(args, "--unshare-all")
    if *cfg.Network {
        args = append(args, "--share-net")
    }
    
	// Always include these (per SPEC hardcoded behavior)
	args = append(args, "--dev", "/dev")
	args = append(args, "--proc", "/proc")
	
	// Root filesystem read-only (per SPEC security guarantees)
	// This is the base - everything else overlays on top
	args = append(args, "--ro-bind", "/", "/")

	// Isolated runtime for internal state (marker file, wrapper runtime tree).
	// Needed because we mount the host root filesystem read-only (`--ro-bind / /`), so
	// bwrap cannot create new mountpoints under /run unless we provide a writable overlay.
	args = append(args, "--tmpfs", "/run")
    
    // Process paths in order (bwrap processes args in order)
    // More specific paths should come AFTER less specific
    sortedPaths := sortBySpecificity(paths)
    
    for _, p := range sortedPaths {
        switch p.Access {
        case "ro":
            // Use --ro-bind-try for paths that may not exist
            // (e.g., lint configs that only exist in some projects)
            args = append(args, "--ro-bind-try", p.Resolved, p.Resolved)
        case "rw":
            // Use --bind-try for optional writable paths
            args = append(args, "--bind-try", p.Resolved, p.Resolved)
        case "exclude":
            // Exclude mounts are implemented in d5g3tgg (directories vs files differ).
            // This ticket can either ignore excludes for now or call a helper stub that d5g3tgg fills in.
        }
    }
    
    // Working directory
    args = append(args, "--chdir", cfg.EffectiveCwd)
    
    return args, nil
}
```

**Key bwrap options used:**
- `--die-with-parent`: Kills sandbox when agent-sandbox dies (clean cleanup)
- `--bind-try` / `--ro-bind-try`: Gracefully ignores non-existent paths
- `--tmpfs` / `--ro-bind`: Exclude handling is implemented in d5g3tgg

**Important:** bwrap argument order matters! Earlier mounts are overlaid by
later mounts. We mount / first, then overlay more specific paths.

## Files to Create
- cmd/agent-sandbox/bwrap.go - bwrap argument generation
- cmd/agent-sandbox/bwrap_test.go - tests

## Key Invariants
- `--die-with-parent` always included (cleanup)
- / is always mounted first (ro)
- More specific paths mounted after less specific
- /dev and /proc are always mounted (virtual)
- /run is always mounted as tmpfs (isolated runtime)
- Argument order is deterministic
- Working directory is set via --chdir

## Acceptance Criteria

**Unit tests (argument generation in bwrap_test.go):**
1. `--die-with-parent` included in args
2. ro paths generate `--ro-bind-try` arguments
3. rw paths generate `--bind-try` arguments
4. exclude paths deferred to d5g3tgg (no incorrect tmpfs-only behavior baked in here)
5. / mounted first as `--ro-bind` (base)
6. /dev and /proc mounted as virtual
7. /run mounted as tmpfs
8. Working directory set via `--chdir`
9. Network sharing controlled by config (`--share-net`)
10. More specific paths appear after less specific (correct mount order)
11. Tests verify generated arguments for various config combinations

**E2E tests (deferred to d5g38e8):**
- Actual read-only enforcement
- Actual write access where permitted
- Actual exclusion behavior
