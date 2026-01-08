---
schema_version: 1
id: d5g35ar
status: open
blocked-by: [d5g3538]
created: 2026-01-08T22:41:47Z
type: task
priority: 1
---
# Presets: Implement @caches Preset

## Background & Context

The @caches preset makes build tool cache directories writable. Per SPEC.md:

> @caches: Build tool caches writable (~/.cache, ~/.bun, ~/go, ~/.npm, ~/.cargo)

Without this, package managers and build tools would fail as they can't write to
their cache directories.

## Rationale

Build tools store downloaded packages, compiled artifacts, and metadata in cache
directories. These must be writable for normal development workflows:

- npm/yarn/pnpm → ~/.npm
- Bun → ~/.bun
- Go → ~/go (GOPATH, module cache)
- Cargo/Rust → ~/.cargo
- Generic XDG cache → ~/.cache

## Implementation Details

@caches should resolve to:

**Read-Write (rw):**
- ~/.cache (XDG cache, used by many tools)
- ~/.bun (Bun runtime and packages)
- ~/go (Go modules, build cache)
- ~/.npm (npm cache)
- ~/.cargo (Rust/Cargo cache)

Optional follow-ups (NOT required for SPEC compliance) can be added later if needed:
- ~/.pnpm-store / ~/.local/share/pnpm (pnpm)
- ~/.yarn (yarn classic)

Note: Some of these may not exist on all systems. Path resolution handles
non-existent paths gracefully (skip silently per SPEC).

## Files to Modify
- cmd/agent-sandbox/preset.go - implement @caches Resolve function
- cmd/agent-sandbox/preset_test.go - test @caches resolution

## Key Invariants
- All paths should expand ~ to home directory
- Non-existent paths will be filtered during path resolution (not preset's job)
- No ro or exclude paths (this is purely additive for rw)

## Acceptance Criteria

1. @caches.Resolve() returns all cache directories as rw paths
2. Includes: ~/.cache, ~/.bun, ~/go, ~/.npm, ~/.cargo, ~/.pnpm-store, ~/.yarn
3. No ro or exclude paths returned
4. All paths are absolute (~ expanded)
5. Tests verify expected paths are returned
