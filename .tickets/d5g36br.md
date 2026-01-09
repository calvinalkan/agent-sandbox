---
schema_version: 1
id: d5g36br
status: closed
closed: 2026-01-09T03:19:51Z
blocked-by: [d5g366g]
created: 2026-01-08T22:43:59Z
type: task
priority: 1
---
# Specificity Engine: Path Conflict Resolution and Mount Ordering

## Background & Context

This is the core logic that takes all resolved paths from config and produces
an ordered list for bwrap. It has two jobs:

1. **Deduplicate**: When the SAME resolved path appears from multiple sources, pick winner
2. **Sort for mount order**: bwrap overlays later mounts on earlier ones

**Understanding SPEC.md Specificity Rules:**

The SPEC lists 4 rules, but they apply to DIFFERENT problems:

**Rules for DEDUPLICATION (same resolved path from multiple sources):**
- Rule 1: Exact path beats glob (e.g., explicit `~/.config/foo` beats glob `~/.config/*`)
- Rule 3: More restrictive wins (exclude > ro > rw)
- Rule 4: Later config layer wins (CLI > project > global > preset)

**Rule handled by MOUNT ORDER (different paths, parent/child relationship):**
- Rule 2: Longer path beats shorter (~/.config/nvim beats ~/.config)

Rule 2 is NOT about deduplication! It's about which mount applies when paths overlap.
Example: If `~` is ro and `~/.cache` is rw, a file at `~/.cache/pip/x` should be rw.
This works automatically because we mount shallower paths first, and bwrap overlays
deeper mounts on top.

## Rationale

This is critical for correct sandbox behavior. Consider:

```
Input (from various sources):
  /home/user          → ro  (from @base preset)
  /home/user/.cache   → rw  (from @caches preset)
  /home/user/.cache   → exclude (from CLI --exclude)
  /home/user/.ssh     → exclude (from @base preset)
  /home/user/project  → rw  (from @base, it's workDir)

After deduplication:
  /home/user          → ro
  /home/user/.cache   → exclude (CLI wins over preset)
  /home/user/.ssh     → exclude
  /home/user/project  → rw

After sorting for bwrap (least specific first):
  /home/user          → ro      (depth 2)
  /home/user/.cache   → exclude (depth 3)
  /home/user/.ssh     → exclude (depth 3)
  /home/user/project  → rw      (depth 3)

bwrap args (conversion happens in bwrap.go, not here):
  --ro-bind /home/user /home/user
  --tmpfs /home/user/.cache           # exclude dir → --tmpfs
  --tmpfs /home/user/.ssh             # exclude dir → --tmpfs  
  --bind /home/user/project /home/user/project

Note: Exclude handling differs for dirs vs files (see d5g3tgg):
  - Directory exclude → --tmpfs (empty dir, contents ENOENT)
  - File exclude → --ro-bind /tmp/empty-000 (exists but EACCES)
```

The order matters! `/home/user` must come first so the more specific paths overlay it.

## Implementation Details

```go
// PathEntry is the input - a resolved path with metadata
type PathEntry struct {
    Original string      // pattern before resolution (for glob detection)
    Resolved string      // absolute path after ~, glob, symlink resolution
    Access   AccessLevel // ro, rw, exclude
    Layer    ConfigLayer // where it came from
}

type AccessLevel int
const (
    AccessRW AccessLevel = iota
    AccessRO
    AccessExclude
)

type ConfigLayer int
const (
    LayerPreset ConfigLayer = iota
    LayerGlobal
    LayerProject
    LayerCLI
)

// ResolveAndSort takes all path entries and returns deduplicated, sorted list
// ready for bwrap argument generation.
func ResolveAndSort(entries []PathEntry) []PathEntry {
    // Step 1: Deduplicate - group by resolved path, pick winner
    deduped := deduplicatePaths(entries)
    
    // Step 2: Sort by depth (shallowest first) for correct bwrap mount order
    sortByMountOrder(deduped)
    
    return deduped
}

// deduplicatePaths groups entries by resolved path and picks winner for each
func deduplicatePaths(entries []PathEntry) []PathEntry {
    byPath := make(map[string][]PathEntry)
    for _, e := range entries {
        byPath[e.Resolved] = append(byPath[e.Resolved], e)
    }
    
    result := make([]PathEntry, 0, len(byPath))
    for _, candidates := range byPath {
        winner := pickWinner(candidates)
        result = append(result, winner)
    }
    return result
}

// pickWinner applies SPEC specificity rules to pick one entry
func pickWinner(candidates []PathEntry) PathEntry {
    if len(candidates) == 1 {
        return candidates[0]
    }
    
    // Sort candidates by priority (highest priority first)
    sort.Slice(candidates, func(i, j int) bool {
        a, b := candidates[i], candidates[j]
        
        // 1. Exact path beats glob (filepath.Match metacharacters: *, ?, [])
        aGlob := strings.ContainsAny(a.Original, "*?[")
        bGlob := strings.ContainsAny(b.Original, "*?[")
        if aGlob != bGlob {
            return !aGlob // non-glob wins
        }
        
        // 2. More restrictive access wins (exclude > ro > rw)
        if a.Access != b.Access {
            return a.Access > b.Access
        }
        
        // 3. Later config layer wins (CLI > project > global > preset)
        return a.Layer > b.Layer
    })
    
    return candidates[0]
}

// sortByMountOrder sorts by path depth (shallowest first)
// This ensures bwrap mounts are in correct overlay order
func sortByMountOrder(entries []PathEntry) {
    sort.Slice(entries, func(i, j int) bool {
        depthI := strings.Count(entries[i].Resolved, string(filepath.Separator))
        depthJ := strings.Count(entries[j].Resolved, string(filepath.Separator))
        if depthI != depthJ {
            return depthI < depthJ // shallower first
        }
        // Stable sort by path for determinism
        return entries[i].Resolved < entries[j].Resolved
    })
}
```

## Table Test Cases

This MUST have comprehensive table-driven tests:

```go
func TestDeduplicatePaths(t *testing.T) {
    tests := []struct {
        name    string
        input   []PathEntry
        want    []PathEntry // expected winners (unordered)
    }{
        {
            name: "same path, CLI wins over preset",
            input: []PathEntry{
                {Resolved: "/home/user/.cache", Access: AccessRW, Layer: LayerPreset},
                {Resolved: "/home/user/.cache", Access: AccessExclude, Layer: LayerCLI},
            },
            want: []PathEntry{
                {Resolved: "/home/user/.cache", Access: AccessExclude, Layer: LayerCLI},
            },
        },
        {
            name: "same path, more restrictive wins at same layer",
            input: []PathEntry{
                {Resolved: "/home/user/.cache", Access: AccessRW, Layer: LayerProject},
                {Resolved: "/home/user/.cache", Access: AccessRO, Layer: LayerProject},
            },
            want: []PathEntry{
                {Resolved: "/home/user/.cache", Access: AccessRO, Layer: LayerProject},
            },
        },
        {
            name: "exact path beats glob for same target",
            input: []PathEntry{
                {Original: "/home/user/.config/*", Resolved: "/home/user/.config/foo", Access: AccessRO, Layer: LayerPreset},
                {Original: "/home/user/.config/foo", Resolved: "/home/user/.config/foo", Access: AccessRW, Layer: LayerPreset},
            },
            want: []PathEntry{
                {Original: "/home/user/.config/foo", Resolved: "/home/user/.config/foo", Access: AccessRW, Layer: LayerPreset},
            },
        },
        {
            name: "different paths kept separate",
            input: []PathEntry{
                {Resolved: "/home/user", Access: AccessRO, Layer: LayerPreset},
                {Resolved: "/home/user/.cache", Access: AccessRW, Layer: LayerPreset},
                {Resolved: "/home/user/.ssh", Access: AccessExclude, Layer: LayerPreset},
            },
            want: []PathEntry{
                {Resolved: "/home/user", Access: AccessRO, Layer: LayerPreset},
                {Resolved: "/home/user/.cache", Access: AccessRW, Layer: LayerPreset},
                {Resolved: "/home/user/.ssh", Access: AccessExclude, Layer: LayerPreset},
            },
        },
    }
    // ...
}

func TestSortByMountOrder(t *testing.T) {
    tests := []struct {
        name  string
        input []PathEntry
        want  []string // expected order of Resolved paths
    }{
        {
            name: "sorts by depth, shallowest first",
            input: []PathEntry{
                {Resolved: "/home/user/.cache/pip"},
                {Resolved: "/home/user"},
                {Resolved: "/home/user/.cache"},
            },
            want: []string{
                "/home/user",
                "/home/user/.cache",
                "/home/user/.cache/pip",
            },
        },
        {
            name: "same depth sorted alphabetically for determinism",
            input: []PathEntry{
                {Resolved: "/home/user/.ssh"},
                {Resolved: "/home/user/.cache"},
                {Resolved: "/home/user/.config"},
            },
            want: []string{
                "/home/user/.cache",
                "/home/user/.config",
                "/home/user/.ssh",
            },
        },
    }
    // ...
}

func TestResolveAndSort_Integration(t *testing.T) {
    tests := []struct {
        name  string
        input []PathEntry
        want  []PathEntry // expected final output in order
    }{
        {
            name: "full pipeline: dedupe then sort",
            input: []PathEntry{
                {Resolved: "/home/user/.cache", Access: AccessRW, Layer: LayerPreset},
                {Resolved: "/home/user/.cache", Access: AccessExclude, Layer: LayerCLI},
                {Resolved: "/home/user", Access: AccessRO, Layer: LayerPreset},
                {Resolved: "/home/user/.ssh", Access: AccessExclude, Layer: LayerPreset},
            },
            want: []PathEntry{
                {Resolved: "/home/user", Access: AccessRO},           // depth 2
                {Resolved: "/home/user/.cache", Access: AccessExclude}, // depth 3, CLI won
                {Resolved: "/home/user/.ssh", Access: AccessExclude},   // depth 3
            },
        },
    }
    // ...
}
```

## Files to Create
- cmd/agent-sandbox/specificity.go - specificity resolution
- cmd/agent-sandbox/specificity_test.go - comprehensive table tests

## Key Invariants
- Output has exactly one entry per unique resolved path
- Output is sorted by depth (shallowest first) for bwrap mount order
- Same depth paths sorted alphabetically for determinism
- Deduplication uses rules 1, 3, 4 from SPEC (NOT rule 2 - that's mount order)
- Rule 2 ("longer path beats shorter") is handled by sorting shallower first

## Acceptance Criteria

**Deduplication (pickWinner):**
1. Same path from glob vs exact: exact wins
2. Same path, same layer: more restrictive access wins (exclude > ro > rw)
3. Same path, same access: later layer wins (CLI > project > global > preset)
4. Different paths: all kept (no deduplication)

**Mount ordering (sortByMountOrder):**
5. Shallower paths come before deeper paths
6. Same depth: alphabetical order for determinism
7. Output order is stable (same input → same output)

**Integration:**
8. Full pipeline produces correct bwrap mount order
9. `--dry-run` output can be visually verified

**Testing:**
10. Table-driven tests for deduplication rules (all edge cases)
11. Table-driven tests for sort ordering
12. Integration tests for full pipeline
13. At least 20 test cases covering combinations
