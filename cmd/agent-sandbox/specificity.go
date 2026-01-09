package main

import (
	"path/filepath"
	"slices"
	"strings"
)

// accessPriority returns the priority of an access level for conflict resolution.
// Higher values win: exclude > ro > rw.
func accessPriority(access PathAccess) int {
	switch access {
	case PathAccessExclude:
		return 2
	case PathAccessRo:
		return 1
	case PathAccessRw:
		return 0
	default:
		return 0
	}
}

// sourcePriority returns the priority of a source layer for conflict resolution.
// Higher values win: CLI > project > global > preset.
func sourcePriority(source PathSource) int {
	switch source {
	case PathSourceCLI:
		return 3
	case PathSourceProject:
		return 2
	case PathSourceGlobal:
		return 1
	case PathSourcePreset:
		return 0
	default:
		return 0
	}
}

// isGlobPattern returns true if the pattern contains glob metacharacters.
func isGlobPattern(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

// pathDepth returns the number of path components (depth) in a path.
// This is used to sort paths by depth (shallowest first).
// Examples: "/" = 0, "/home" = 1, "/home/user" = 2, "/home/user/.cache" = 3.
func pathDepth(path string) int {
	// Clean and count non-empty components
	cleaned := filepath.Clean(path)
	if cleaned == "/" {
		return 0
	}
	// Count separators minus leading one
	return strings.Count(cleaned, string(filepath.Separator))
}

// ResolveAndSort takes all resolved path entries and returns a deduplicated,
// sorted list ready for bwrap argument generation.
//
// The pipeline:
//  1. Deduplicate - when the same resolved path appears from multiple sources,
//     pick a winner using SPEC specificity rules
//  2. Sort - order by mount depth (shallowest first) so bwrap mounts correctly
//     overlay deeper paths on shallower ones
func ResolveAndSort(entries []ResolvedPath) []ResolvedPath {
	if len(entries) == 0 {
		return nil
	}

	// Step 1: Deduplicate - group by resolved path, pick winner for each
	deduped := deduplicatePaths(entries)

	// Step 2: Sort by depth (shallowest first) for correct bwrap mount order
	sortByMountOrder(deduped)

	return deduped
}

// deduplicatePaths groups entries by resolved path and picks a winner for each.
func deduplicatePaths(entries []ResolvedPath) []ResolvedPath {
	byPath := make(map[string][]ResolvedPath)
	for _, e := range entries {
		byPath[e.Resolved] = append(byPath[e.Resolved], e)
	}

	result := make([]ResolvedPath, 0, len(byPath))
	for _, candidates := range byPath {
		winner := pickWinner(candidates)
		result = append(result, winner)
	}

	return result
}

// pickWinner applies SPEC specificity rules to pick one entry from candidates.
//
// Rules (from SPEC):
//  1. Exact path beats glob - an explicit path wins over a glob-expanded path
//  2. More restrictive access wins - exclude > ro > rw
//  3. Later config layer wins - CLI > project > global > preset
func pickWinner(candidates []ResolvedPath) ResolvedPath {
	if len(candidates) == 1 {
		return candidates[0]
	}

	// Sort candidates by priority (highest priority first)
	slices.SortFunc(candidates, func(left, right ResolvedPath) int {
		// 1. Exact path beats glob (non-glob wins)
		leftGlob := isGlobPattern(left.Original)
		rightGlob := isGlobPattern(right.Original)

		if leftGlob != rightGlob {
			if rightGlob {
				return -1 // left is exact (non-glob), wins
			}

			return 1 // right is exact (non-glob), wins
		}

		// 2. More restrictive access wins (exclude > ro > rw)
		leftPrio := accessPriority(left.Access)
		rightPrio := accessPriority(right.Access)

		if leftPrio != rightPrio {
			return rightPrio - leftPrio // higher priority first
		}

		// 3. Later config layer wins (CLI > project > global > preset)
		leftLayer := sourcePriority(left.Source)
		rightLayer := sourcePriority(right.Source)

		return rightLayer - leftLayer // higher layer first
	})

	return candidates[0]
}

// sortByMountOrder sorts entries by path depth (shallowest first).
// This ensures bwrap mounts are in correct overlay order - shallower paths
// are mounted first, and deeper paths overlay them.
//
// Paths at the same depth are sorted alphabetically for determinism.
func sortByMountOrder(entries []ResolvedPath) {
	slices.SortFunc(entries, func(left, right ResolvedPath) int {
		leftDepth := pathDepth(left.Resolved)
		rightDepth := pathDepth(right.Resolved)

		if leftDepth != rightDepth {
			return leftDepth - rightDepth // shallower first
		}
		// Same depth: alphabetical order for determinism
		return strings.Compare(left.Resolved, right.Resolved)
	})
}
