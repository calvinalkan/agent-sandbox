package main

import (
	"slices"
	"testing"
)

// ============================================================================
// accessPriority tests
// ============================================================================

func Test_AccessPriority_Returns_Higher_For_Exclude(t *testing.T) {
	t.Parallel()

	if accessPriority(PathAccessExclude) <= accessPriority(PathAccessRo) {
		t.Error("exclude should have higher priority than ro")
	}

	if accessPriority(PathAccessExclude) <= accessPriority(PathAccessRw) {
		t.Error("exclude should have higher priority than rw")
	}
}

func Test_AccessPriority_Returns_Ro_Higher_Than_Rw(t *testing.T) {
	t.Parallel()

	if accessPriority(PathAccessRo) <= accessPriority(PathAccessRw) {
		t.Error("ro should have higher priority than rw")
	}
}

// ============================================================================
// sourcePriority tests
// ============================================================================

func Test_SourcePriority_Returns_CLI_Highest(t *testing.T) {
	t.Parallel()

	cli := sourcePriority(PathSourceCLI)
	project := sourcePriority(PathSourceProject)
	global := sourcePriority(PathSourceGlobal)
	preset := sourcePriority(PathSourcePreset)

	if cli <= project {
		t.Error("CLI should have higher priority than project")
	}

	if project <= global {
		t.Error("project should have higher priority than global")
	}

	if global <= preset {
		t.Error("global should have higher priority than preset")
	}
}

// ============================================================================
// isGlobPattern tests
// ============================================================================

func Test_IsGlobPattern_Returns_True_For_Star(t *testing.T) {
	t.Parallel()

	if !isGlobPattern("*.txt") {
		t.Error("*.txt should be detected as glob")
	}
}

func Test_IsGlobPattern_Returns_True_For_Question(t *testing.T) {
	t.Parallel()

	if !isGlobPattern("?.txt") {
		t.Error("?.txt should be detected as glob")
	}
}

func Test_IsGlobPattern_Returns_True_For_Bracket(t *testing.T) {
	t.Parallel()

	if !isGlobPattern("[abc].txt") {
		t.Error("[abc].txt should be detected as glob")
	}
}

func Test_IsGlobPattern_Returns_False_For_Plain_Path(t *testing.T) {
	t.Parallel()

	if isGlobPattern("/home/user/.config/foo") {
		t.Error("plain path should not be detected as glob")
	}
}

func Test_IsGlobPattern_Returns_False_For_Tilde_Path(t *testing.T) {
	t.Parallel()

	if isGlobPattern("~/.ssh/id_rsa") {
		t.Error("tilde path should not be detected as glob")
	}
}

// ============================================================================
// pathDepth tests
// ============================================================================

func Test_PathDepth_Returns_Correct_Count(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path  string
		depth int
	}{
		{"/", 0},
		{"/home", 1},
		{"/home/user", 2},
		{"/home/user/.cache", 3},
		{"/home/user/.cache/pip", 4},
	}

	for _, tt := range tests {
		got := pathDepth(tt.path)
		if got != tt.depth {
			t.Errorf("pathDepth(%q) = %d, want %d", tt.path, got, tt.depth)
		}
	}
}

// ============================================================================
// pickWinner tests - Exact path beats glob
// ============================================================================

func Test_PickWinner_Exact_Path_Beats_Glob_For_Same_Target(t *testing.T) {
	t.Parallel()

	candidates := []ResolvedPath{
		{Original: "/home/user/.config/*", Resolved: "/home/user/.config/foo", Access: PathAccessRo, Source: PathSourcePreset},
		{Original: "/home/user/.config/foo", Resolved: "/home/user/.config/foo", Access: PathAccessRw, Source: PathSourcePreset},
	}

	winner := pickWinner(candidates)

	// Exact path wins even though it has lower access priority
	if winner.Original != "/home/user/.config/foo" {
		t.Errorf("expected exact path to win, got original=%q", winner.Original)
	}

	if winner.Access != PathAccessRw {
		t.Errorf("expected rw access, got %q", winner.Access)
	}
}

func Test_PickWinner_Exact_Path_Beats_Glob_Even_With_Lower_Layer(t *testing.T) {
	t.Parallel()

	candidates := []ResolvedPath{
		{Original: "~/.config/*", Resolved: "/home/user/.config/foo", Access: PathAccessRo, Source: PathSourceCLI},
		{Original: "~/.config/foo", Resolved: "/home/user/.config/foo", Access: PathAccessRo, Source: PathSourcePreset},
	}

	winner := pickWinner(candidates)

	// Exact path (preset) wins over glob (CLI)
	if winner.Original != "~/.config/foo" {
		t.Errorf("expected exact path to win, got original=%q", winner.Original)
	}
}

// ============================================================================
// pickWinner tests - More restrictive access wins at same layer
// ============================================================================

func Test_PickWinner_Exclude_Beats_Ro_At_Same_Layer(t *testing.T) {
	t.Parallel()

	candidates := []ResolvedPath{
		{Original: "/home/user/.cache", Resolved: "/home/user/.cache", Access: PathAccessRo, Source: PathSourceProject},
		{Original: "/home/user/.cache", Resolved: "/home/user/.cache", Access: PathAccessExclude, Source: PathSourceProject},
	}

	winner := pickWinner(candidates)

	if winner.Access != PathAccessExclude {
		t.Errorf("expected exclude to win, got %q", winner.Access)
	}
}

func Test_PickWinner_Exclude_Beats_Rw_At_Same_Layer(t *testing.T) {
	t.Parallel()

	candidates := []ResolvedPath{
		{Original: "/home/user/.cache", Resolved: "/home/user/.cache", Access: PathAccessRw, Source: PathSourceProject},
		{Original: "/home/user/.cache", Resolved: "/home/user/.cache", Access: PathAccessExclude, Source: PathSourceProject},
	}

	winner := pickWinner(candidates)

	if winner.Access != PathAccessExclude {
		t.Errorf("expected exclude to win, got %q", winner.Access)
	}
}

func Test_PickWinner_Ro_Beats_Rw_At_Same_Layer(t *testing.T) {
	t.Parallel()

	candidates := []ResolvedPath{
		{Original: "/home/user/.cache", Resolved: "/home/user/.cache", Access: PathAccessRw, Source: PathSourceProject},
		{Original: "/home/user/.cache", Resolved: "/home/user/.cache", Access: PathAccessRo, Source: PathSourceProject},
	}

	winner := pickWinner(candidates)

	if winner.Access != PathAccessRo {
		t.Errorf("expected ro to win, got %q", winner.Access)
	}
}

// ============================================================================
// pickWinner tests - Later config layer wins
// ============================================================================

func Test_PickWinner_CLI_Wins_Over_Preset(t *testing.T) {
	t.Parallel()

	candidates := []ResolvedPath{
		{Original: "/home/user/.cache", Resolved: "/home/user/.cache", Access: PathAccessRw, Source: PathSourcePreset},
		{Original: "/home/user/.cache", Resolved: "/home/user/.cache", Access: PathAccessExclude, Source: PathSourceCLI},
	}

	winner := pickWinner(candidates)

	if winner.Source != PathSourceCLI {
		t.Errorf("expected CLI to win, got %q", winner.Source)
	}

	if winner.Access != PathAccessExclude {
		t.Errorf("expected exclude access, got %q", winner.Access)
	}
}

func Test_PickWinner_Project_Wins_Over_Global(t *testing.T) {
	t.Parallel()

	// Same access level - layer priority should apply
	candidates := []ResolvedPath{
		{Original: "/home/user/.cache", Resolved: "/home/user/.cache", Access: PathAccessRo, Source: PathSourceGlobal},
		{Original: "/home/user/.cache", Resolved: "/home/user/.cache", Access: PathAccessRo, Source: PathSourceProject},
	}

	winner := pickWinner(candidates)

	if winner.Source != PathSourceProject {
		t.Errorf("expected project to win, got %q", winner.Source)
	}
}

func Test_PickWinner_Global_Wins_Over_Preset(t *testing.T) {
	t.Parallel()

	// Same access level - layer priority should apply
	candidates := []ResolvedPath{
		{Original: "/home/user/.cache", Resolved: "/home/user/.cache", Access: PathAccessRo, Source: PathSourcePreset},
		{Original: "/home/user/.cache", Resolved: "/home/user/.cache", Access: PathAccessRo, Source: PathSourceGlobal},
	}

	winner := pickWinner(candidates)

	if winner.Source != PathSourceGlobal {
		t.Errorf("expected global to win, got %q", winner.Source)
	}
}

// ============================================================================
// pickWinner tests - Combined priority rules
// ============================================================================

func Test_PickWinner_Access_Beats_Layer_When_Both_Non_Glob(t *testing.T) {
	t.Parallel()

	// When both are non-glob and same layer, more restrictive access wins
	candidates := []ResolvedPath{
		{Original: "/home/user/.cache", Resolved: "/home/user/.cache", Access: PathAccessRw, Source: PathSourceProject},
		{Original: "/home/user/.cache", Resolved: "/home/user/.cache", Access: PathAccessExclude, Source: PathSourceProject},
	}

	winner := pickWinner(candidates)

	if winner.Access != PathAccessExclude {
		t.Errorf("expected exclude to win, got %q", winner.Access)
	}
}

func Test_PickWinner_Layer_Matters_When_Both_Non_Glob_Same_Access(t *testing.T) {
	t.Parallel()

	// When both are non-glob and same access, later layer wins
	candidates := []ResolvedPath{
		{Original: "/home/user/.cache", Resolved: "/home/user/.cache", Access: PathAccessRo, Source: PathSourcePreset},
		{Original: "/home/user/.cache", Resolved: "/home/user/.cache", Access: PathAccessRo, Source: PathSourceProject},
	}

	winner := pickWinner(candidates)

	if winner.Source != PathSourceProject {
		t.Errorf("expected project to win, got %q", winner.Source)
	}
}

func Test_PickWinner_Single_Candidate_Returns_It(t *testing.T) {
	t.Parallel()

	candidate := ResolvedPath{
		Original: "/home/user/.cache",
		Resolved: "/home/user/.cache",
		Access:   PathAccessRo,
		Source:   PathSourcePreset,
	}

	winner := pickWinner([]ResolvedPath{candidate})

	if winner != candidate {
		t.Error("single candidate should be returned as-is")
	}
}

// ============================================================================
// deduplicatePaths tests
// ============================================================================

func Test_DeduplicatePaths_Keeps_Different_Paths_Separate(t *testing.T) {
	t.Parallel()

	entries := []ResolvedPath{
		{Resolved: "/home/user", Access: PathAccessRo, Source: PathSourcePreset},
		{Resolved: "/home/user/.cache", Access: PathAccessRw, Source: PathSourcePreset},
		{Resolved: "/home/user/.ssh", Access: PathAccessExclude, Source: PathSourcePreset},
	}

	result := deduplicatePaths(entries)

	if len(result) != 3 {
		t.Errorf("expected 3 paths (all different), got %d", len(result))
	}
}

func Test_DeduplicatePaths_Merges_Same_Paths(t *testing.T) {
	t.Parallel()

	entries := []ResolvedPath{
		{Resolved: "/home/user/.cache", Access: PathAccessRw, Source: PathSourcePreset},
		{Resolved: "/home/user/.cache", Access: PathAccessExclude, Source: PathSourceCLI},
	}

	result := deduplicatePaths(entries)

	if len(result) != 1 {
		t.Errorf("expected 1 path (merged), got %d", len(result))
	}

	// CLI should win
	if result[0].Source != PathSourceCLI {
		t.Errorf("expected CLI source, got %q", result[0].Source)
	}
}

func Test_DeduplicatePaths_Returns_Empty_For_Empty_Input(t *testing.T) {
	t.Parallel()

	result := deduplicatePaths(nil)

	if len(result) != 0 {
		t.Errorf("expected empty result, got %d entries", len(result))
	}
}

// ============================================================================
// sortByMountOrder tests
// ============================================================================

func Test_SortByMountOrder_Sorts_By_Depth_Shallowest_First(t *testing.T) {
	t.Parallel()

	entries := []ResolvedPath{
		{Resolved: "/home/user/.cache/pip"},
		{Resolved: "/home/user"},
		{Resolved: "/home/user/.cache"},
	}

	sortByMountOrder(entries)

	expected := []string{
		"/home/user",
		"/home/user/.cache",
		"/home/user/.cache/pip",
	}

	for i, exp := range expected {
		if entries[i].Resolved != exp {
			t.Errorf("position %d: expected %q, got %q", i, exp, entries[i].Resolved)
		}
	}
}

func Test_SortByMountOrder_Sorts_Alphabetically_At_Same_Depth(t *testing.T) {
	t.Parallel()

	entries := []ResolvedPath{
		{Resolved: "/home/user/.ssh"},
		{Resolved: "/home/user/.cache"},
		{Resolved: "/home/user/.config"},
	}

	sortByMountOrder(entries)

	expected := []string{
		"/home/user/.cache",
		"/home/user/.config",
		"/home/user/.ssh",
	}

	for i, exp := range expected {
		if entries[i].Resolved != exp {
			t.Errorf("position %d: expected %q, got %q", i, exp, entries[i].Resolved)
		}
	}
}

func Test_SortByMountOrder_Handles_Root_Path(t *testing.T) {
	t.Parallel()

	entries := []ResolvedPath{
		{Resolved: "/home/user"},
		{Resolved: "/"},
		{Resolved: "/home"},
	}

	sortByMountOrder(entries)

	expected := []string{
		"/",
		"/home",
		"/home/user",
	}

	for i, exp := range expected {
		if entries[i].Resolved != exp {
			t.Errorf("position %d: expected %q, got %q", i, exp, entries[i].Resolved)
		}
	}
}

func Test_SortByMountOrder_Is_Stable(t *testing.T) {
	t.Parallel()

	// Run multiple times to ensure determinism
	for run := range 10 {
		entries := []ResolvedPath{
			{Resolved: "/home/user/.ssh"},
			{Resolved: "/home/user/.cache"},
			{Resolved: "/home/user"},
			{Resolved: "/home/user/.config"},
		}

		sortByMountOrder(entries)

		expected := []string{
			"/home/user",
			"/home/user/.cache",
			"/home/user/.config",
			"/home/user/.ssh",
		}

		for i, exp := range expected {
			if entries[i].Resolved != exp {
				t.Errorf("run %d, position %d: expected %q, got %q", run, i, exp, entries[i].Resolved)
			}
		}
	}
}

// ============================================================================
// ResolveAndSort integration tests
// ============================================================================

func Test_ResolveAndSort_Full_Pipeline(t *testing.T) {
	t.Parallel()

	input := []ResolvedPath{
		{Resolved: "/home/user/.cache", Access: PathAccessRw, Source: PathSourcePreset},
		{Resolved: "/home/user/.cache", Access: PathAccessExclude, Source: PathSourceCLI},
		{Resolved: "/home/user", Access: PathAccessRo, Source: PathSourcePreset},
		{Resolved: "/home/user/.ssh", Access: PathAccessExclude, Source: PathSourcePreset},
	}

	result := ResolveAndSort(input)

	// Should have 3 unique paths
	if len(result) != 3 {
		t.Fatalf("expected 3 paths after dedup, got %d", len(result))
	}

	// Should be sorted by depth
	expected := []string{
		"/home/user",
		"/home/user/.cache",
		"/home/user/.ssh",
	}

	for i, exp := range expected {
		if result[i].Resolved != exp {
			t.Errorf("position %d: expected %q, got %q", i, exp, result[i].Resolved)
		}
	}

	// .cache should have exclude access (CLI won)
	for _, r := range result {
		if r.Resolved == "/home/user/.cache" {
			if r.Access != PathAccessExclude {
				t.Errorf(".cache should have exclude access, got %q", r.Access)
			}

			if r.Source != PathSourceCLI {
				t.Errorf(".cache should be from CLI, got %q", r.Source)
			}
		}
	}
}

func Test_ResolveAndSort_Returns_Nil_For_Empty_Input(t *testing.T) {
	t.Parallel()

	result := ResolveAndSort(nil)

	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
}

func Test_ResolveAndSort_Returns_Nil_For_Zero_Length_Slice(t *testing.T) {
	t.Parallel()

	result := ResolveAndSort([]ResolvedPath{})

	if result != nil {
		t.Errorf("expected nil for zero-length slice, got %v", result)
	}
}

func Test_ResolveAndSort_Single_Entry(t *testing.T) {
	t.Parallel()

	input := []ResolvedPath{
		{Resolved: "/home/user/.cache", Access: PathAccessRw, Source: PathSourcePreset},
	}

	result := ResolveAndSort(input)

	if len(result) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(result))
	}

	if result[0].Resolved != "/home/user/.cache" {
		t.Errorf("expected /home/user/.cache, got %q", result[0].Resolved)
	}
}

// ============================================================================
// Comprehensive table-driven tests
// ============================================================================

func Test_PickWinner_Table_Driven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		candidates []ResolvedPath
		wantAccess PathAccess
		wantSource PathSource
		wantOrig   string
	}{
		{
			name: "exact path beats glob",
			candidates: []ResolvedPath{
				{Original: "~/.config/*", Resolved: "/home/user/.config/foo", Access: PathAccessRo, Source: PathSourcePreset},
				{Original: "~/.config/foo", Resolved: "/home/user/.config/foo", Access: PathAccessRw, Source: PathSourcePreset},
			},
			wantAccess: PathAccessRw,
			wantSource: PathSourcePreset,
			wantOrig:   "~/.config/foo",
		},
		{
			name: "CLI wins over preset for same path",
			candidates: []ResolvedPath{
				{Original: "~/.cache", Resolved: "/home/user/.cache", Access: PathAccessRw, Source: PathSourcePreset},
				{Original: "~/.cache", Resolved: "/home/user/.cache", Access: PathAccessRo, Source: PathSourceCLI},
			},
			wantAccess: PathAccessRo,
			wantSource: PathSourceCLI,
		},
		{
			name: "more restrictive wins at same layer",
			candidates: []ResolvedPath{
				{Original: "~/.cache", Resolved: "/home/user/.cache", Access: PathAccessRw, Source: PathSourceProject},
				{Original: "~/.cache", Resolved: "/home/user/.cache", Access: PathAccessExclude, Source: PathSourceProject},
			},
			wantAccess: PathAccessExclude,
			wantSource: PathSourceProject,
		},
		{
			name: "three-way conflict: access priority applies",
			candidates: []ResolvedPath{
				{Original: "~/.cache", Resolved: "/home/user/.cache", Access: PathAccessRw, Source: PathSourceProject},
				{Original: "~/.cache", Resolved: "/home/user/.cache", Access: PathAccessRo, Source: PathSourceProject},
				{Original: "~/.cache", Resolved: "/home/user/.cache", Access: PathAccessExclude, Source: PathSourceProject},
			},
			wantAccess: PathAccessExclude,
			wantSource: PathSourceProject,
		},
		{
			name: "four-way conflict: layer order matters",
			candidates: []ResolvedPath{
				{Original: "~/.cache", Resolved: "/home/user/.cache", Access: PathAccessRo, Source: PathSourcePreset},
				{Original: "~/.cache", Resolved: "/home/user/.cache", Access: PathAccessRo, Source: PathSourceGlobal},
				{Original: "~/.cache", Resolved: "/home/user/.cache", Access: PathAccessRo, Source: PathSourceProject},
				{Original: "~/.cache", Resolved: "/home/user/.cache", Access: PathAccessRo, Source: PathSourceCLI},
			},
			wantAccess: PathAccessRo,
			wantSource: PathSourceCLI,
		},
		{
			name: "glob from later layer loses to exact from earlier layer",
			candidates: []ResolvedPath{
				{Original: "~/.config/foo", Resolved: "/home/user/.config/foo", Access: PathAccessRo, Source: PathSourcePreset},
				{Original: "~/.config/*", Resolved: "/home/user/.config/foo", Access: PathAccessExclude, Source: PathSourceCLI},
			},
			wantAccess: PathAccessRo,
			wantSource: PathSourcePreset,
			wantOrig:   "~/.config/foo",
		},
		{
			name: "both globs: access priority then layer",
			candidates: []ResolvedPath{
				{Original: "~/.config/*", Resolved: "/home/user/.config/foo", Access: PathAccessRw, Source: PathSourcePreset},
				{Original: "~/.config/*", Resolved: "/home/user/.config/foo", Access: PathAccessRo, Source: PathSourcePreset},
			},
			wantAccess: PathAccessRo,
			wantSource: PathSourcePreset,
		},
		{
			name: "both globs same access: layer wins",
			candidates: []ResolvedPath{
				{Original: "~/.config/*", Resolved: "/home/user/.config/foo", Access: PathAccessRo, Source: PathSourceGlobal},
				{Original: "~/.config/*", Resolved: "/home/user/.config/foo", Access: PathAccessRo, Source: PathSourceProject},
			},
			wantAccess: PathAccessRo,
			wantSource: PathSourceProject,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			winner := pickWinner(tt.candidates)

			if winner.Access != tt.wantAccess {
				t.Errorf("access = %q, want %q", winner.Access, tt.wantAccess)
			}

			if winner.Source != tt.wantSource {
				t.Errorf("source = %q, want %q", winner.Source, tt.wantSource)
			}

			if tt.wantOrig != "" && winner.Original != tt.wantOrig {
				t.Errorf("original = %q, want %q", winner.Original, tt.wantOrig)
			}
		})
	}
}

func Test_SortByMountOrder_Table_Driven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []ResolvedPath
		want  []string
	}{
		{
			name: "basic depth ordering",
			input: []ResolvedPath{
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
			name: "alphabetical at same depth",
			input: []ResolvedPath{
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
		{
			name: "mixed depths",
			input: []ResolvedPath{
				{Resolved: "/home/user/.cache/pip/cache"},
				{Resolved: "/home"},
				{Resolved: "/home/user/.cache"},
				{Resolved: "/home/user"},
			},
			want: []string{
				"/home",
				"/home/user",
				"/home/user/.cache",
				"/home/user/.cache/pip/cache",
			},
		},
		{
			name: "root comes first",
			input: []ResolvedPath{
				{Resolved: "/etc"},
				{Resolved: "/"},
				{Resolved: "/home"},
			},
			want: []string{
				"/",
				"/etc",
				"/home",
			},
		},
		{
			name: "deeply nested paths",
			input: []ResolvedPath{
				{Resolved: "/a/b/c/d/e"},
				{Resolved: "/a"},
				{Resolved: "/a/b/c"},
				{Resolved: "/a/b"},
			},
			want: []string{
				"/a",
				"/a/b",
				"/a/b/c",
				"/a/b/c/d/e",
			},
		},
		{
			name:  "empty slice",
			input: []ResolvedPath{},
			want:  []string{},
		},
		{
			name: "single entry",
			input: []ResolvedPath{
				{Resolved: "/home/user"},
			},
			want: []string{"/home/user"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Make a copy to avoid modifying test data
			entries := slices.Clone(tt.input)
			sortByMountOrder(entries)

			got := make([]string, len(entries))
			for i, e := range entries {
				got[i] = e.Resolved
			}

			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_ResolveAndSort_Table_Driven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      []ResolvedPath
		wantPaths  []string
		wantAccess map[string]PathAccess
	}{
		{
			name: "dedup and sort combined",
			input: []ResolvedPath{
				{Resolved: "/home/user/.cache", Access: PathAccessRw, Source: PathSourcePreset},
				{Resolved: "/home/user/.cache", Access: PathAccessExclude, Source: PathSourceCLI},
				{Resolved: "/home/user", Access: PathAccessRo, Source: PathSourcePreset},
				{Resolved: "/home/user/.ssh", Access: PathAccessExclude, Source: PathSourcePreset},
			},
			wantPaths: []string{
				"/home/user",
				"/home/user/.cache",
				"/home/user/.ssh",
			},
			wantAccess: map[string]PathAccess{
				"/home/user":        PathAccessRo,
				"/home/user/.cache": PathAccessExclude, // CLI won
				"/home/user/.ssh":   PathAccessExclude,
			},
		},
		{
			name: "no duplicates just sort",
			input: []ResolvedPath{
				{Resolved: "/home/user/.ssh", Access: PathAccessExclude, Source: PathSourcePreset},
				{Resolved: "/home/user", Access: PathAccessRo, Source: PathSourcePreset},
				{Resolved: "/home/user/project", Access: PathAccessRw, Source: PathSourceProject},
			},
			wantPaths: []string{
				"/home/user",
				"/home/user/.ssh",
				"/home/user/project",
			},
			wantAccess: map[string]PathAccess{
				"/home/user":         PathAccessRo,
				"/home/user/.ssh":    PathAccessExclude,
				"/home/user/project": PathAccessRw,
			},
		},
		{
			name: "complex scenario with globs",
			input: []ResolvedPath{
				// Glob from preset
				{Original: "~/.config/*", Resolved: "/home/user/.config/foo", Access: PathAccessRo, Source: PathSourcePreset},
				// Exact from project overrides
				{Original: "~/.config/foo", Resolved: "/home/user/.config/foo", Access: PathAccessRw, Source: PathSourceProject},
				// Other paths
				{Resolved: "/home/user", Access: PathAccessRo, Source: PathSourcePreset},
			},
			wantPaths: []string{
				"/home/user",
				"/home/user/.config/foo",
			},
			wantAccess: map[string]PathAccess{
				"/home/user":             PathAccessRo,
				"/home/user/.config/foo": PathAccessRw, // Exact beats glob
			},
		},
		{
			name: "realistic @base preset scenario",
			input: []ResolvedPath{
				// Home dir ro from @base
				{Resolved: "/home/user", Access: PathAccessRo, Source: PathSourcePreset},
				// Workdir rw from @base
				{Resolved: "/home/user/project", Access: PathAccessRw, Source: PathSourcePreset},
				// Caches rw from @caches
				{Resolved: "/home/user/.cache", Access: PathAccessRw, Source: PathSourcePreset},
				// Secrets exclude from @base
				{Resolved: "/home/user/.ssh", Access: PathAccessExclude, Source: PathSourcePreset},
				{Resolved: "/home/user/.gnupg", Access: PathAccessExclude, Source: PathSourcePreset},
				// CLI override: make .cache readonly
				{Resolved: "/home/user/.cache", Access: PathAccessRo, Source: PathSourceCLI},
			},
			wantPaths: []string{
				"/home/user",
				"/home/user/.cache",
				"/home/user/.gnupg",
				"/home/user/.ssh",
				"/home/user/project",
			},
			wantAccess: map[string]PathAccess{
				"/home/user":         PathAccessRo,
				"/home/user/.cache":  PathAccessRo, // CLI override won
				"/home/user/.gnupg":  PathAccessExclude,
				"/home/user/.ssh":    PathAccessExclude,
				"/home/user/project": PathAccessRw,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := ResolveAndSort(tt.input)

			// Check path order
			gotPaths := make([]string, len(result))
			for i, r := range result {
				gotPaths[i] = r.Resolved
			}

			if !slices.Equal(gotPaths, tt.wantPaths) {
				t.Errorf("paths = %v, want %v", gotPaths, tt.wantPaths)
			}

			// Check access levels
			for _, r := range result {
				want, ok := tt.wantAccess[r.Resolved]
				if !ok {
					continue
				}

				if r.Access != want {
					t.Errorf("%s access = %q, want %q", r.Resolved, r.Access, want)
				}
			}
		})
	}
}
