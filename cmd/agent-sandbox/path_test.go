package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// ============================================================================
// ResolvePath tests - Home directory expansion
// ============================================================================

func Test_ResolvePath_Expands_Tilde_Slash_To_Home_Dir(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("~/foo", "/home/user", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/home/user/foo"
	if result != expected {
		t.Errorf("ResolvePath(~/foo) = %q, want %q", result, expected)
	}
}

func Test_ResolvePath_Expands_Lone_Tilde_To_Home_Dir(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("~", "/home/user", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/home/user"
	if result != expected {
		t.Errorf("ResolvePath(~) = %q, want %q", result, expected)
	}
}

func Test_ResolvePath_Expands_Tilde_Nested_Path(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("~/code/project/src", "/home/alice", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/home/alice/code/project/src"
	if result != expected {
		t.Errorf("ResolvePath(~/code/project/src) = %q, want %q", result, expected)
	}
}

func Test_ResolvePath_Handles_Tilde_With_Different_Home_Dir(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("~/.config", "/users/bob", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/users/bob/.config"
	if result != expected {
		t.Errorf("ResolvePath(~/.config) = %q, want %q", result, expected)
	}
}

// ============================================================================
// ResolvePath tests - Absolute paths
// ============================================================================

func Test_ResolvePath_Keeps_Absolute_Path_Unchanged(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("/absolute/path", "/home/user", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/absolute/path"
	if result != expected {
		t.Errorf("ResolvePath(/absolute/path) = %q, want %q", result, expected)
	}
}

func Test_ResolvePath_Keeps_Root_Path_Unchanged(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("/", "/home/user", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/"
	if result != expected {
		t.Errorf("ResolvePath(/) = %q, want %q", result, expected)
	}
}

func Test_ResolvePath_Cleans_Absolute_Path(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("/foo/../bar", "/home/user", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/bar"
	if result != expected {
		t.Errorf("ResolvePath(/foo/../bar) = %q, want %q", result, expected)
	}
}

// ============================================================================
// ResolvePath tests - Relative paths
// ============================================================================

func Test_ResolvePath_Resolves_Relative_Path_Against_WorkDir(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("foo", "/home/user", "/home/user/project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/home/user/project/foo"
	if result != expected {
		t.Errorf("ResolvePath(foo) = %q, want %q", result, expected)
	}
}

func Test_ResolvePath_Resolves_DotSlash_Relative_Path(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("./foo", "/home/user", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/work/foo"
	if result != expected {
		t.Errorf("ResolvePath(./foo) = %q, want %q", result, expected)
	}
}

func Test_ResolvePath_Resolves_Nested_Relative_Path(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("src/auth/login", "/home/user", "/home/user/project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/home/user/project/src/auth/login"
	if result != expected {
		t.Errorf("ResolvePath(src/auth/login) = %q, want %q", result, expected)
	}
}

func Test_ResolvePath_Cleans_Relative_Path_With_DotDot(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("foo/../bar", "/home/user", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/work/bar"
	if result != expected {
		t.Errorf("ResolvePath(foo/../bar) = %q, want %q", result, expected)
	}
}

func Test_ResolvePath_Cleans_Dot_In_Path(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("./foo/./bar", "/home/user", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/work/foo/bar"
	if result != expected {
		t.Errorf("ResolvePath(./foo/./bar) = %q, want %q", result, expected)
	}
}

// ============================================================================
// ResolvePath tests - Environment variables NOT expanded
// ============================================================================

func Test_ResolvePath_Treats_Dollar_HOME_As_Literal(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("$HOME/foo", "/home/user", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// $HOME is treated as a literal string, so it's a relative path
	expected := "/work/$HOME/foo"
	if result != expected {
		t.Errorf("ResolvePath($HOME/foo) = %q, want %q", result, expected)
	}
}

func Test_ResolvePath_Treats_Dollar_PWD_As_Literal(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("$PWD/foo", "/home/user", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// $PWD is treated as a literal string, so it's a relative path
	expected := "/work/$PWD/foo"
	if result != expected {
		t.Errorf("ResolvePath($PWD/foo) = %q, want %q", result, expected)
	}
}

func Test_ResolvePath_Treats_Dollar_USER_As_Literal(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("$USER/foo", "/home/user", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// $USER is treated as a literal string, so it's a relative path
	expected := "/work/$USER/foo"
	if result != expected {
		t.Errorf("ResolvePath($USER/foo) = %q, want %q", result, expected)
	}
}

// ============================================================================
// ResolvePath tests - Error handling
// ============================================================================

func Test_ResolvePath_Returns_Error_For_Empty_Pattern(t *testing.T) {
	t.Parallel()

	_, err := ResolvePath("", "/home/user", "/work")
	if err == nil {
		t.Fatal("expected error for empty pattern, got nil")
	}

	if !errors.Is(err, ErrEmptyPathPattern) {
		t.Errorf("expected ErrEmptyPathPattern, got: %v", err)
	}
}

// ============================================================================
// ResolvePath tests - Path cleaning
// ============================================================================

func Test_ResolvePath_Cleans_Multiple_Slashes(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("foo//bar///baz", "/home/user", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/work/foo/bar/baz"
	if result != expected {
		t.Errorf("ResolvePath(foo//bar///baz) = %q, want %q", result, expected)
	}
}

func Test_ResolvePath_Cleans_Trailing_Slash(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("foo/bar/", "/home/user", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/work/foo/bar"
	if result != expected {
		t.Errorf("ResolvePath(foo/bar/) = %q, want %q", result, expected)
	}
}

func Test_ResolvePath_Cleans_Tilde_Path_With_DotDot(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("~/foo/../bar", "/home/user", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/home/user/bar"
	if result != expected {
		t.Errorf("ResolvePath(~/foo/../bar) = %q, want %q", result, expected)
	}
}

// ============================================================================
// ResolvePath tests - Edge cases
// ============================================================================

func Test_ResolvePath_Handles_Dot_Pattern(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath(".", "/home/user", "/work/project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/work/project"
	if result != expected {
		t.Errorf("ResolvePath(.) = %q, want %q", result, expected)
	}
}

func Test_ResolvePath_Handles_DotDot_Pattern(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("..", "/home/user", "/work/project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "/work"
	if result != expected {
		t.Errorf("ResolvePath(..) = %q, want %q", result, expected)
	}
}

func Test_ResolvePath_Does_Not_Expand_Tilde_In_Middle(t *testing.T) {
	t.Parallel()

	result, err := ResolvePath("foo~bar", "/home/user", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Tilde in middle is not expanded
	expected := "/work/foo~bar"
	if result != expected {
		t.Errorf("ResolvePath(foo~bar) = %q, want %q", result, expected)
	}
}

func Test_ResolvePath_Does_Not_Expand_TildeUser(t *testing.T) {
	t.Parallel()

	// ~user syntax is NOT supported - only ~ and ~/
	result, err := ResolvePath("~user/foo", "/home/user", "/work")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ~user is treated as a relative path
	expected := "/work/~user/foo"
	if result != expected {
		t.Errorf("ResolvePath(~user/foo) = %q, want %q", result, expected)
	}
}

// ============================================================================
// ResolvePath tests - Table-driven comprehensive tests
// ============================================================================

func Test_ResolvePath_Table_Driven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pattern  string
		homeDir  string
		workDir  string
		expected string
	}{
		{
			name:     "tilde expands to home",
			pattern:  "~",
			homeDir:  "/home/alice",
			workDir:  "/work",
			expected: "/home/alice",
		},
		{
			name:     "tilde slash expands to home",
			pattern:  "~/foo",
			homeDir:  "/home/alice",
			workDir:  "/work",
			expected: "/home/alice/foo",
		},
		{
			name:     "absolute path unchanged",
			pattern:  "/etc/config",
			homeDir:  "/home/alice",
			workDir:  "/work",
			expected: "/etc/config",
		},
		{
			name:     "relative path resolves to workdir",
			pattern:  "src/main.go",
			homeDir:  "/home/alice",
			workDir:  "/home/alice/project",
			expected: "/home/alice/project/src/main.go",
		},
		{
			name:     "dot path resolves to workdir",
			pattern:  ".",
			homeDir:  "/home/alice",
			workDir:  "/home/alice/project",
			expected: "/home/alice/project",
		},
		{
			name:     "dotdot escapes workdir",
			pattern:  "..",
			homeDir:  "/home/alice",
			workDir:  "/home/alice/project",
			expected: "/home/alice",
		},
		{
			name:     "env vars not expanded",
			pattern:  "$HOME/file",
			homeDir:  "/home/alice",
			workDir:  "/work",
			expected: "/work/$HOME/file",
		},
		{
			name:     "path cleaned of double slashes",
			pattern:  "foo//bar",
			homeDir:  "/home/alice",
			workDir:  "/work",
			expected: "/work/foo/bar",
		},
		{
			name:     "path cleaned of dots",
			pattern:  "./foo/./bar",
			homeDir:  "/home/alice",
			workDir:  "/work",
			expected: "/work/foo/bar",
		},
		{
			name:     "path cleaned of dotdots",
			pattern:  "foo/bar/../baz",
			homeDir:  "/home/alice",
			workDir:  "/work",
			expected: "/work/foo/baz",
		},
		{
			name:     "tilde user not expanded",
			pattern:  "~bob/file",
			homeDir:  "/home/alice",
			workDir:  "/work",
			expected: "/work/~bob/file",
		},
		{
			name:     "hidden file in home",
			pattern:  "~/.ssh/id_rsa",
			homeDir:  "/home/alice",
			workDir:  "/work",
			expected: "/home/alice/.ssh/id_rsa",
		},
		{
			name:     "config pattern",
			pattern:  "~/.config/agent-sandbox/config.json",
			homeDir:  "/home/alice",
			workDir:  "/work",
			expected: "/home/alice/.config/agent-sandbox/config.json",
		},
		{
			name:     "git hooks pattern",
			pattern:  ".git/hooks",
			homeDir:  "/home/alice",
			workDir:  "/home/alice/project",
			expected: "/home/alice/project/.git/hooks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ResolvePath(tt.pattern, tt.homeDir, tt.workDir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("ResolvePath(%q) = %q, want %q", tt.pattern, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// ResolvePath tests - Result is always absolute
// ============================================================================

func Test_ResolvePath_Result_Is_Always_Absolute(t *testing.T) {
	t.Parallel()

	patterns := []string{
		"~",
		"~/foo",
		"/absolute",
		"relative",
		"./relative",
		"../relative",
		".",
		"..",
	}

	for _, pattern := range patterns {
		result, err := ResolvePath(pattern, "/home/user", "/work/project")
		if err != nil {
			t.Errorf("ResolvePath(%q) unexpected error: %v", pattern, err)

			continue
		}

		if result == "" || result[0] != '/' {
			t.Errorf("ResolvePath(%q) = %q, want absolute path (starting with /)", pattern, result)
		}
	}
}

// ============================================================================
// ExpandGlob tests - Non-glob patterns
// ============================================================================

func Test_ExpandGlob_Returns_NonGlob_Pattern_AsIs(t *testing.T) {
	t.Parallel()

	result, err := ExpandGlob("/some/path/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"/some/path/file.txt"}
	if !slices.Equal(result, expected) {
		t.Errorf("ExpandGlob(/some/path/file.txt) = %v, want %v", result, expected)
	}
}

func Test_ExpandGlob_Returns_Path_Without_Metacharacters_AsIs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
	}{
		{"simple path", "/home/user/file"},
		{"path with dots", "/home/user/.config"},
		{"path with dashes", "/home/user/my-project"},
		{"path with underscores", "/home/user/my_file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ExpandGlob(tt.pattern)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			expected := []string{tt.pattern}
			if !slices.Equal(result, expected) {
				t.Errorf("ExpandGlob(%q) = %v, want %v", tt.pattern, result, expected)
			}
		})
	}
}

// ============================================================================
// ExpandGlob tests - Invalid patterns
// ============================================================================

func Test_ExpandGlob_Returns_Error_For_Malformed_Bracket(t *testing.T) {
	t.Parallel()

	_, err := ExpandGlob("/path/[")
	if err == nil {
		t.Fatal("expected error for malformed bracket, got nil")
	}

	// Error should contain the pattern and indicate it's invalid
	errStr := err.Error()
	if !contains(errStr, "invalid glob pattern") || !contains(errStr, "/path/[") {
		t.Errorf("error should mention invalid glob pattern and the pattern, got: %v", err)
	}
}

func Test_ExpandGlob_Returns_Error_For_Unclosed_Bracket(t *testing.T) {
	t.Parallel()

	_, err := ExpandGlob("/path/[abc")
	if err == nil {
		t.Fatal("expected error for unclosed bracket, got nil")
	}
}

// ============================================================================
// ExpandGlob tests - Glob patterns with actual filesystem
// ============================================================================

func Test_ExpandGlob_Expands_Star_Pattern(t *testing.T) {
	t.Parallel()

	// Create temp directory with files
	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, "file1.txt"), "")
	mustCreateFile(t, filepath.Join(dir, "file2.txt"), "")
	mustCreateFile(t, filepath.Join(dir, "other.json"), "")

	// Expand *.txt pattern
	result, err := ExpandGlob(filepath.Join(dir, "*.txt"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should match file1.txt and file2.txt
	if len(result) != 2 {
		t.Errorf("expected 2 matches, got %d: %v", len(result), result)
	}

	// Results should be sorted (filepath.Glob behavior)
	expected := []string{
		filepath.Join(dir, "file1.txt"),
		filepath.Join(dir, "file2.txt"),
	}
	if !slices.Equal(result, expected) {
		t.Errorf("ExpandGlob(*.txt) = %v, want %v", result, expected)
	}
}

func Test_ExpandGlob_Expands_Question_Mark_Pattern(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, "a.txt"), "")
	mustCreateFile(t, filepath.Join(dir, "b.txt"), "")
	mustCreateFile(t, filepath.Join(dir, "ab.txt"), "") // Should not match

	result, err := ExpandGlob(filepath.Join(dir, "?.txt"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should match a.txt and b.txt but not ab.txt
	if len(result) != 2 {
		t.Errorf("expected 2 matches, got %d: %v", len(result), result)
	}

	expected := []string{
		filepath.Join(dir, "a.txt"),
		filepath.Join(dir, "b.txt"),
	}
	if !slices.Equal(result, expected) {
		t.Errorf("ExpandGlob(?.txt) = %v, want %v", result, expected)
	}
}

func Test_ExpandGlob_Expands_Character_Class_Pattern(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, "file1.txt"), "")
	mustCreateFile(t, filepath.Join(dir, "file2.txt"), "")
	mustCreateFile(t, filepath.Join(dir, "file3.txt"), "") // Should not match

	result, err := ExpandGlob(filepath.Join(dir, "file[12].txt"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should match file1.txt and file2.txt but not file3.txt
	if len(result) != 2 {
		t.Errorf("expected 2 matches, got %d: %v", len(result), result)
	}

	expected := []string{
		filepath.Join(dir, "file1.txt"),
		filepath.Join(dir, "file2.txt"),
	}
	if !slices.Equal(result, expected) {
		t.Errorf("ExpandGlob(file[12].txt) = %v, want %v", result, expected)
	}
}

func Test_ExpandGlob_Expands_Multiple_Wildcards(t *testing.T) {
	t.Parallel()

	// Create nested structure: dir/pkg1/sub/config.json, dir/pkg2/sub/config.json
	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, "pkg1", "sub", "config.json"), "")
	mustCreateFile(t, filepath.Join(dir, "pkg2", "sub", "config.json"), "")
	mustCreateFile(t, filepath.Join(dir, "pkg3", "other", "config.json"), "") // Different middle dir

	// Pattern: dir/*/sub/config.json
	result, err := ExpandGlob(filepath.Join(dir, "*", "sub", "config.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should match pkg1 and pkg2 but not pkg3 (different subdir name)
	if len(result) != 2 {
		t.Errorf("expected 2 matches, got %d: %v", len(result), result)
	}
}

func Test_ExpandGlob_Expands_Star_For_Directories(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustCreateDir(t, filepath.Join(dir, "packages", "pkg-a"))
	mustCreateDir(t, filepath.Join(dir, "packages", "pkg-b"))
	mustCreateFile(t, filepath.Join(dir, "packages", "pkg-a", "biome.json"), "")
	mustCreateFile(t, filepath.Join(dir, "packages", "pkg-b", "biome.json"), "")

	// Pattern: packages/*/biome.json
	result, err := ExpandGlob(filepath.Join(dir, "packages", "*", "biome.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 matches, got %d: %v", len(result), result)
	}
}

// ============================================================================
// ExpandGlob tests - Empty results
// ============================================================================

func Test_ExpandGlob_Returns_Empty_When_No_Matches(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, "file.json"), "")

	// Pattern that matches nothing
	result, err := ExpandGlob(filepath.Join(dir, "*.nonexistent"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Per SPEC: "Glob matches nothing → Skip silently"
	if result != nil {
		t.Errorf("expected nil for no matches, got %v", result)
	}
}

func Test_ExpandGlob_Returns_Empty_When_Directory_Empty(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Empty directory

	result, err := ExpandGlob(filepath.Join(dir, "*"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != nil {
		t.Errorf("expected nil for no matches, got %v", result)
	}
}

// ============================================================================
// ExpandGlob tests - Symlinks NOT resolved (done by full pipeline)
// ============================================================================

func Test_ExpandGlob_Returns_Symlinks_Unresolved(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create a real file
	realFile := filepath.Join(dir, "real", "file.txt")
	mustCreateFile(t, realFile, "content")

	// Create a symlink to the real file
	symlinkDir := filepath.Join(dir, "links")
	mustCreateDir(t, symlinkDir)
	symlink := filepath.Join(symlinkDir, "link.txt")

	err := os.Symlink(realFile, symlink)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	// Expand glob that matches the symlink
	result, err := ExpandGlob(filepath.Join(symlinkDir, "*.txt"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ExpandGlob now returns symlinks as-is (resolution done by resolveOnePath)
	if len(result) != 1 {
		t.Fatalf("expected 1 match, got %d: %v", len(result), result)
	}

	if result[0] != symlink {
		t.Errorf("expected symlink path %q, got %q", symlink, result[0])
	}
}

func Test_ExpandGlob_Includes_Dangling_Symlinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create a symlink to a non-existent file
	symlink := filepath.Join(dir, "dangling.txt")

	err := os.Symlink("/nonexistent/target", symlink)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	// Also create a valid file
	validFile := filepath.Join(dir, "valid.txt")
	mustCreateFile(t, validFile, "content")

	// Expand glob
	result, err := ExpandGlob(filepath.Join(dir, "*.txt"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ExpandGlob returns ALL matches including dangling symlinks
	// (filtering happens in resolveOnePath)
	if len(result) != 2 {
		t.Errorf("expected 2 matches (including dangling), got %d: %v", len(result), result)
	}
}

// ============================================================================
// ExpandGlob tests - Sorting
// ============================================================================

func Test_ExpandGlob_Results_Are_Sorted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create files in non-alphabetical order
	mustCreateFile(t, filepath.Join(dir, "z.txt"), "")
	mustCreateFile(t, filepath.Join(dir, "a.txt"), "")
	mustCreateFile(t, filepath.Join(dir, "m.txt"), "")

	result, err := ExpandGlob(filepath.Join(dir, "*.txt"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// filepath.Glob returns sorted results
	expected := []string{
		filepath.Join(dir, "a.txt"),
		filepath.Join(dir, "m.txt"),
		filepath.Join(dir, "z.txt"),
	}
	if !slices.Equal(result, expected) {
		t.Errorf("results should be sorted, got %v, want %v", result, expected)
	}
}

// ============================================================================
// ExpandGlob tests - Real-world patterns
// ============================================================================

func Test_ExpandGlob_Handles_DotEnv_Pattern(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, ".env"), "")
	mustCreateFile(t, filepath.Join(dir, ".env.local"), "")
	mustCreateFile(t, filepath.Join(dir, ".env.production"), "")
	mustCreateFile(t, filepath.Join(dir, "other.txt"), "")

	// Pattern: .env*
	result, err := ExpandGlob(filepath.Join(dir, ".env*"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Errorf("expected 3 matches for .env*, got %d: %v", len(result), result)
	}
}

func Test_ExpandGlob_Handles_Packages_Biome_Pattern(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, "packages", "client", "biome.json"), "")
	mustCreateFile(t, filepath.Join(dir, "packages", "server", "biome.json"), "")
	mustCreateFile(t, filepath.Join(dir, "packages", "shared", "biome.json"), "")

	// Pattern: packages/*/biome.json (common in monorepos)
	result, err := ExpandGlob(filepath.Join(dir, "packages", "*", "biome.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Errorf("expected 3 matches, got %d: %v", len(result), result)
	}
}

func Test_ExpandGlob_Handles_Config_Secrets_Pattern(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustCreateFile(t, filepath.Join(dir, "config", "dev", "secrets.json"), "")
	mustCreateFile(t, filepath.Join(dir, "config", "prod", "secrets.json"), "")
	mustCreateFile(t, filepath.Join(dir, "config", "prod", "settings.json"), "") // Different name

	// Pattern: config/*/secrets.json
	result, err := ExpandGlob(filepath.Join(dir, "config", "*", "secrets.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 matches, got %d: %v", len(result), result)
	}
}

// ============================================================================
// Helper functions for tests
// ============================================================================

func mustCreateFile(t *testing.T, path, content string) {
	t.Helper()

	dir := filepath.Dir(path)

	err := os.MkdirAll(dir, 0o750)
	if err != nil {
		t.Fatalf("failed to create directory %s: %v", dir, err)
	}

	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("failed to create file %s: %v", path, err)
	}
}

func mustCreateDir(t *testing.T, path string) {
	t.Helper()

	err := os.MkdirAll(path, 0o750)
	if err != nil {
		t.Fatalf("failed to create directory %s: %v", path, err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || substr == "" ||
		(s != "" && substr != "" && containsHelper(s, substr)))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}

// ============================================================================
// ResolvePaths tests - Full pipeline
// ============================================================================

func Test_ResolvePaths_Resolves_Existing_Paths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	homeDir := filepath.Join(dir, "home")
	workDir := filepath.Join(dir, "work")

	mustCreateDir(t, homeDir)
	mustCreateDir(t, workDir)

	// Create some test files
	testFile := filepath.Join(workDir, "test.txt")
	mustCreateFile(t, testFile, "content")

	input := ResolvePathsInput{
		CLI: PathLayerInput{
			Ro: []string{"test.txt"},
		},
		HomeDir: homeDir,
		WorkDir: workDir,
	}

	result, err := ResolvePaths(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 resolved path, got %d: %v", len(result), result)
	}

	if result[0].Resolved != testFile {
		t.Errorf("expected resolved path %q, got %q", testFile, result[0].Resolved)
	}

	if result[0].Access != PathAccessRo {
		t.Errorf("expected access 'ro', got %q", result[0].Access)
	}

	if result[0].Source != PathSourceCLI {
		t.Errorf("expected source 'cli', got %q", result[0].Source)
	}
}

func Test_ResolvePaths_Skips_NonExistent_Paths_Silently(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	homeDir := filepath.Join(dir, "home")
	workDir := filepath.Join(dir, "work")

	mustCreateDir(t, homeDir)
	mustCreateDir(t, workDir)

	// Create one existing file
	existingFile := filepath.Join(workDir, "exists.txt")
	mustCreateFile(t, existingFile, "content")

	input := ResolvePathsInput{
		CLI: PathLayerInput{
			Ro: []string{
				"exists.txt",
				"nonexistent.txt", // This file doesn't exist
			},
		},
		HomeDir: homeDir,
		WorkDir: workDir,
	}

	result, err := ResolvePaths(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should only include existing file, silently skip non-existent
	if len(result) != 1 {
		t.Fatalf("expected 1 resolved path (non-existent skipped), got %d: %v", len(result), result)
	}

	if result[0].Resolved != existingFile {
		t.Errorf("expected %q, got %q", existingFile, result[0].Resolved)
	}
}

func Test_ResolvePaths_Skips_Glob_With_No_Matches(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	homeDir := filepath.Join(dir, "home")
	workDir := filepath.Join(dir, "work")

	mustCreateDir(t, homeDir)
	mustCreateDir(t, workDir)

	// Create a file that won't match the pattern
	mustCreateFile(t, filepath.Join(workDir, "file.json"), "content")

	input := ResolvePathsInput{
		CLI: PathLayerInput{
			Ro: []string{"*.nonexistent"}, // Pattern matches nothing
		},
		HomeDir: homeDir,
		WorkDir: workDir,
	}

	result, err := ResolvePaths(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No paths should be returned, but no error either
	if len(result) != 0 {
		t.Errorf("expected 0 paths for non-matching glob, got %d: %v", len(result), result)
	}
}

func Test_ResolvePaths_Returns_Error_For_Invalid_Glob(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	homeDir := filepath.Join(dir, "home")
	workDir := filepath.Join(dir, "work")

	mustCreateDir(t, homeDir)
	mustCreateDir(t, workDir)

	input := ResolvePathsInput{
		CLI: PathLayerInput{
			Ro: []string{"[invalid"}, // Malformed bracket expression
		},
		HomeDir: homeDir,
		WorkDir: workDir,
	}

	_, err := ResolvePaths(&input)
	if err == nil {
		t.Fatal("expected error for invalid glob pattern, got nil")
	}

	if !strings.Contains(err.Error(), "invalid glob pattern") {
		t.Errorf("expected error to mention 'invalid glob pattern', got: %v", err)
	}
}

func Test_ResolvePaths_Resolves_Symlinks_To_Real_Paths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	homeDir := filepath.Join(dir, "home")
	workDir := filepath.Join(dir, "work")

	mustCreateDir(t, homeDir)
	mustCreateDir(t, workDir)

	// Create real file and symlink
	realFile := filepath.Join(workDir, "real.txt")
	mustCreateFile(t, realFile, "content")

	symlink := filepath.Join(workDir, "link.txt")

	err := os.Symlink(realFile, symlink)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	input := ResolvePathsInput{
		CLI: PathLayerInput{
			Ro: []string{"link.txt"}, // Reference the symlink
		},
		HomeDir: homeDir,
		WorkDir: workDir,
	}

	result, err := ResolvePaths(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 resolved path, got %d: %v", len(result), result)
	}

	// Resolved path should be the real file, not the symlink
	if result[0].Resolved != realFile {
		t.Errorf("symlink should resolve to %q, got %q", realFile, result[0].Resolved)
	}

	// Original should still be the pattern
	if result[0].Original != "link.txt" {
		t.Errorf("original should be %q, got %q", "link.txt", result[0].Original)
	}
}

func Test_ResolvePaths_Skips_Dangling_Symlinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	homeDir := filepath.Join(dir, "home")
	workDir := filepath.Join(dir, "work")

	mustCreateDir(t, homeDir)
	mustCreateDir(t, workDir)

	// Create dangling symlink
	symlink := filepath.Join(workDir, "dangling.txt")

	err := os.Symlink("/nonexistent/target", symlink)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	input := ResolvePathsInput{
		CLI: PathLayerInput{
			Ro: []string{"dangling.txt"},
		},
		HomeDir: homeDir,
		WorkDir: workDir,
	}

	result, err := ResolvePaths(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Dangling symlinks should be skipped silently
	if len(result) != 0 {
		t.Errorf("expected 0 paths (dangling symlink skipped), got %d: %v", len(result), result)
	}
}

func Test_ResolvePaths_Preserves_Source_Tracking(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	homeDir := filepath.Join(dir, "home")
	workDir := filepath.Join(dir, "work")

	mustCreateDir(t, homeDir)
	mustCreateDir(t, workDir)

	// Create files for each layer
	presetFile := filepath.Join(workDir, "preset.txt")
	globalFile := filepath.Join(workDir, "global.txt")
	projectFile := filepath.Join(workDir, "project.txt")
	cliFile := filepath.Join(workDir, "cli.txt")

	mustCreateFile(t, presetFile, "")
	mustCreateFile(t, globalFile, "")
	mustCreateFile(t, projectFile, "")
	mustCreateFile(t, cliFile, "")

	input := ResolvePathsInput{
		Preset:  PathLayerInput{Ro: []string{"preset.txt"}},
		Global:  PathLayerInput{Ro: []string{"global.txt"}},
		Project: PathLayerInput{Ro: []string{"project.txt"}},
		CLI:     PathLayerInput{Ro: []string{"cli.txt"}},
		HomeDir: homeDir,
		WorkDir: workDir,
	}

	result, err := ResolvePaths(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 4 {
		t.Fatalf("expected 4 resolved paths, got %d", len(result))
	}

	// Check sources are preserved (results should be in layer order)
	expected := []PathSource{PathSourcePreset, PathSourceGlobal, PathSourceProject, PathSourceCLI}
	for i, exp := range expected {
		if result[i].Source != exp {
			t.Errorf("result[%d].Source = %q, want %q", i, result[i].Source, exp)
		}
	}
}

func Test_ResolvePaths_Preserves_Access_Levels(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	homeDir := filepath.Join(dir, "home")
	workDir := filepath.Join(dir, "work")

	mustCreateDir(t, homeDir)
	mustCreateDir(t, workDir)

	// Create files for each access level
	roFile := filepath.Join(workDir, "readonly.txt")
	rwFile := filepath.Join(workDir, "readwrite.txt")
	excludeFile := filepath.Join(workDir, "excluded.txt")

	mustCreateFile(t, roFile, "")
	mustCreateFile(t, rwFile, "")
	mustCreateFile(t, excludeFile, "")

	input := ResolvePathsInput{
		CLI: PathLayerInput{
			Ro:      []string{"readonly.txt"},
			Rw:      []string{"readwrite.txt"},
			Exclude: []string{"excluded.txt"},
		},
		HomeDir: homeDir,
		WorkDir: workDir,
	}

	result, err := ResolvePaths(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 resolved paths, got %d", len(result))
	}

	// Check access levels (results should be in ro, rw, exclude order)
	expectedAccess := []PathAccess{PathAccessRo, PathAccessRw, PathAccessExclude}
	for i, exp := range expectedAccess {
		if result[i].Access != exp {
			t.Errorf("result[%d].Access = %q, want %q", i, result[i].Access, exp)
		}
	}
}

func Test_ResolvePaths_Expands_Tilde_Paths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	homeDir := filepath.Join(dir, "home")
	workDir := filepath.Join(dir, "work")

	mustCreateDir(t, homeDir)
	mustCreateDir(t, workDir)

	// Create file in home directory
	homeFile := filepath.Join(homeDir, ".config", "test.json")
	mustCreateFile(t, homeFile, "")

	input := ResolvePathsInput{
		CLI: PathLayerInput{
			Ro: []string{"~/.config/test.json"},
		},
		HomeDir: homeDir,
		WorkDir: workDir,
	}

	result, err := ResolvePaths(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 resolved path, got %d", len(result))
	}

	if result[0].Resolved != homeFile {
		t.Errorf("expected %q, got %q", homeFile, result[0].Resolved)
	}

	if result[0].Original != "~/.config/test.json" {
		t.Errorf("original should be preserved, got %q", result[0].Original)
	}
}

func Test_ResolvePaths_Expands_Glob_Patterns(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	homeDir := filepath.Join(dir, "home")
	workDir := filepath.Join(dir, "work")

	mustCreateDir(t, homeDir)
	mustCreateDir(t, workDir)

	// Create multiple matching files
	mustCreateFile(t, filepath.Join(workDir, "pkg1", "config.json"), "")
	mustCreateFile(t, filepath.Join(workDir, "pkg2", "config.json"), "")
	mustCreateFile(t, filepath.Join(workDir, "pkg3", "other.json"), "") // Won't match

	input := ResolvePathsInput{
		CLI: PathLayerInput{
			Ro: []string{"*/config.json"},
		},
		HomeDir: homeDir,
		WorkDir: workDir,
	}

	result, err := ResolvePaths(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 resolved paths from glob, got %d: %v", len(result), result)
	}

	// Both should have same original pattern
	for _, r := range result {
		if r.Original != "*/config.json" {
			t.Errorf("original should be preserved as pattern, got %q", r.Original)
		}
	}
}

func Test_ResolvePaths_Integration_Real_Filesystem(t *testing.T) {
	t.Parallel()

	// Create a realistic project structure
	dir := t.TempDir()
	homeDir := filepath.Join(dir, "home")
	workDir := filepath.Join(dir, "project")

	// Home directory structure
	mustCreateFile(t, filepath.Join(homeDir, ".ssh", "id_rsa"), "secret")
	mustCreateFile(t, filepath.Join(homeDir, ".config", "agent-sandbox", "config.json"), "{}")

	// Project directory structure
	mustCreateFile(t, filepath.Join(workDir, "src", "main.go"), "package main")
	mustCreateFile(t, filepath.Join(workDir, "packages", "client", "biome.json"), "{}")
	mustCreateFile(t, filepath.Join(workDir, "packages", "server", "biome.json"), "{}")
	mustCreateFile(t, filepath.Join(workDir, ".git", "config"), "[core]")
	mustCreateFile(t, filepath.Join(workDir, ".env"), "SECRET=value")

	// Create symlink
	realConfig := filepath.Join(workDir, "config", "real.json")
	mustCreateFile(t, realConfig, "{}")

	symConfig := filepath.Join(workDir, "config.json")

	err := os.Symlink(realConfig, symConfig)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	input := ResolvePathsInput{
		Preset: PathLayerInput{
			Exclude: []string{"~/.ssh"}, // Exclude secrets
		},
		Global: PathLayerInput{
			Ro: []string{"~/.config/agent-sandbox/config.json"},
		},
		Project: PathLayerInput{
			Ro: []string{
				"packages/*/biome.json", // Glob pattern
				".git/config",
			},
			Rw: []string{"src"},
		},
		CLI: PathLayerInput{
			Exclude: []string{".env"},
			Ro:      []string{"config.json"}, // Symlink
		},
		HomeDir: homeDir,
		WorkDir: workDir,
	}

	result, err := ResolvePaths(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected: 1 preset exclude, 1 global ro, 2 project ro (glob), 1 project ro, 1 project rw, 1 cli exclude, 1 cli ro
	if len(result) != 8 {
		t.Logf("results: %+v", result)
		t.Fatalf("expected 8 resolved paths, got %d", len(result))
	}

	// Verify symlink was resolved
	var foundSymlink bool

	for _, r := range result {
		if r.Original == "config.json" {
			foundSymlink = true

			if r.Resolved != realConfig {
				t.Errorf("symlink should resolve to %q, got %q", realConfig, r.Resolved)
			}
		}
	}

	if !foundSymlink {
		t.Error("expected to find resolved symlink in results")
	}

	// Verify source tracking
	sources := make(map[PathSource]int)
	for _, r := range result {
		sources[r.Source]++
	}

	if sources[PathSourcePreset] != 1 {
		t.Errorf("expected 1 preset path, got %d", sources[PathSourcePreset])
	}

	if sources[PathSourceGlobal] != 1 {
		t.Errorf("expected 1 global path, got %d", sources[PathSourceGlobal])
	}

	if sources[PathSourceProject] != 4 { // 2 from glob + 1 git config + 1 src
		t.Errorf("expected 4 project paths, got %d", sources[PathSourceProject])
	}

	if sources[PathSourceCLI] != 2 {
		t.Errorf("expected 2 CLI paths, got %d", sources[PathSourceCLI])
	}
}

func Test_ResolvePaths_Returns_Empty_For_Empty_Input(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	input := ResolvePathsInput{
		HomeDir: dir,
		WorkDir: dir,
	}

	result, err := ResolvePaths(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 paths for empty input, got %d", len(result))
	}
}

func Test_ResolvePaths_Returns_Error_For_Empty_Pattern(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	input := ResolvePathsInput{
		CLI: PathLayerInput{
			Ro: []string{""}, // Empty pattern
		},
		HomeDir: dir,
		WorkDir: dir,
	}

	_, err := ResolvePaths(&input)
	if err == nil {
		t.Fatal("expected error for empty pattern, got nil")
	}

	if !errors.Is(err, ErrEmptyPathPattern) {
		t.Errorf("expected ErrEmptyPathPattern, got: %v", err)
	}
}

func Test_ResolvePaths_Multiple_Globs_Same_Pattern(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	homeDir := filepath.Join(dir, "home")
	workDir := filepath.Join(dir, "work")

	mustCreateDir(t, homeDir)
	mustCreateDir(t, workDir)

	// Create files that match
	mustCreateFile(t, filepath.Join(workDir, "a.txt"), "")
	mustCreateFile(t, filepath.Join(workDir, "b.txt"), "")
	mustCreateFile(t, filepath.Join(workDir, "c.txt"), "")

	input := ResolvePathsInput{
		CLI: PathLayerInput{
			Ro: []string{"*.txt"},
		},
		HomeDir: homeDir,
		WorkDir: workDir,
	}

	result, err := ResolvePaths(&input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Errorf("expected 3 paths from glob, got %d: %v", len(result), result)
	}

	// All should have same original pattern
	for _, r := range result {
		if r.Original != "*.txt" {
			t.Errorf("original should be pattern '*.txt', got %q", r.Original)
		}
	}
}

// ============================================================================
// ValidateWorkDirNotExcluded tests
// ============================================================================

func Test_ValidateWorkDirNotExcluded_Returns_Error_When_WorkDir_Equals_Excluded_Path(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	workDir := filepath.Join(dir, "excluded")
	mustCreateDir(t, workDir)

	paths := []ResolvedPath{
		{Resolved: workDir, Access: PathAccessExclude, Source: PathSourceCLI},
	}

	err := ValidateWorkDirNotExcluded(paths, workDir)
	if err == nil {
		t.Fatal("expected error when workDir equals excluded path")
	}

	if !errors.Is(err, ErrWorkDirExcluded) {
		t.Errorf("expected ErrWorkDirExcluded, got: %v", err)
	}

	// Error should include both paths
	AssertContains(t, err.Error(), workDir)
	// Error should suggest fix
	AssertContains(t, err.Error(), "change directory or remove exclusion")
}

func Test_ValidateWorkDirNotExcluded_Returns_Error_When_WorkDir_Is_Subdirectory_Of_Excluded(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	excludedPath := filepath.Join(dir, "excluded")
	workDir := filepath.Join(excludedPath, "subdir", "deep")
	mustCreateDir(t, workDir)

	paths := []ResolvedPath{
		{Resolved: excludedPath, Access: PathAccessExclude, Source: PathSourceCLI},
	}

	err := ValidateWorkDirNotExcluded(paths, workDir)
	if err == nil {
		t.Fatal("expected error when workDir is subdirectory of excluded path")
	}

	if !errors.Is(err, ErrWorkDirExcluded) {
		t.Errorf("expected ErrWorkDirExcluded, got: %v", err)
	}

	// Error should include both paths
	AssertContains(t, err.Error(), workDir)
	AssertContains(t, err.Error(), excludedPath)
}

func Test_ValidateWorkDirNotExcluded_No_Error_When_WorkDir_Is_Under_ReadOnly_Path(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	roPath := filepath.Join(dir, "readonly")
	workDir := filepath.Join(roPath, "subdir")
	mustCreateDir(t, workDir)

	paths := []ResolvedPath{
		{Resolved: roPath, Access: PathAccessRo, Source: PathSourceCLI},
	}

	err := ValidateWorkDirNotExcluded(paths, workDir)
	if err != nil {
		t.Errorf("unexpected error for workDir under ro path: %v", err)
	}
}

func Test_ValidateWorkDirNotExcluded_No_Error_When_WorkDir_Is_Under_ReadWrite_Path(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rwPath := filepath.Join(dir, "readwrite")
	workDir := filepath.Join(rwPath, "subdir")
	mustCreateDir(t, workDir)

	paths := []ResolvedPath{
		{Resolved: rwPath, Access: PathAccessRw, Source: PathSourceCLI},
	}

	err := ValidateWorkDirNotExcluded(paths, workDir)
	if err != nil {
		t.Errorf("unexpected error for workDir under rw path: %v", err)
	}
}

func Test_ValidateWorkDirNotExcluded_Resolves_Symlinks_In_WorkDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create the real directory structure
	realDir := filepath.Join(dir, "real", "project")
	mustCreateDir(t, realDir)

	// Create a symlink to the real directory
	linkDir := filepath.Join(dir, "link")

	err := os.Symlink(filepath.Join(dir, "real"), linkDir)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	// Exclude the real path
	paths := []ResolvedPath{
		{Resolved: filepath.Join(dir, "real"), Access: PathAccessExclude, Source: PathSourceCLI},
	}

	// workDir via symlink should still be detected as excluded
	workDirViaSymlink := filepath.Join(linkDir, "project")

	err = ValidateWorkDirNotExcluded(paths, workDirViaSymlink)
	if err == nil {
		t.Fatal("expected error when workDir (via symlink) resolves to excluded path")
	}

	if !errors.Is(err, ErrWorkDirExcluded) {
		t.Errorf("expected ErrWorkDirExcluded, got: %v", err)
	}
}

func Test_ValidateWorkDirNotExcluded_No_Error_When_No_Excluded_Paths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	workDir := filepath.Join(dir, "work")
	mustCreateDir(t, workDir)

	paths := []ResolvedPath{
		{Resolved: filepath.Join(dir, "other"), Access: PathAccessRo, Source: PathSourceCLI},
		{Resolved: filepath.Join(dir, "another"), Access: PathAccessRw, Source: PathSourceProject},
	}

	err := ValidateWorkDirNotExcluded(paths, workDir)
	if err != nil {
		t.Errorf("unexpected error when no excluded paths: %v", err)
	}
}

func Test_ValidateWorkDirNotExcluded_No_Error_When_Excluded_Path_Is_Sibling(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	workDir := filepath.Join(dir, "work")
	excludedDir := filepath.Join(dir, "excluded")

	mustCreateDir(t, workDir)
	mustCreateDir(t, excludedDir)

	paths := []ResolvedPath{
		{Resolved: excludedDir, Access: PathAccessExclude, Source: PathSourceCLI},
	}

	// workDir is a sibling of excluded path, should be OK
	err := ValidateWorkDirNotExcluded(paths, workDir)
	if err != nil {
		t.Errorf("unexpected error when excluded path is sibling: %v", err)
	}
}

func Test_ValidateWorkDirNotExcluded_No_Error_When_Excluded_Path_Is_Child_Of_WorkDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	workDir := filepath.Join(dir, "work")
	excludedDir := filepath.Join(workDir, "excluded")
	mustCreateDir(t, excludedDir)

	paths := []ResolvedPath{
		{Resolved: excludedDir, Access: PathAccessExclude, Source: PathSourceCLI},
	}

	// workDir is parent of excluded path, should be OK
	err := ValidateWorkDirNotExcluded(paths, workDir)
	if err != nil {
		t.Errorf("unexpected error when excluded path is child of workDir: %v", err)
	}
}

func Test_ValidateWorkDirNotExcluded_No_Error_When_Empty_Paths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	workDir := filepath.Join(dir, "work")
	mustCreateDir(t, workDir)

	err := ValidateWorkDirNotExcluded(nil, workDir)
	if err != nil {
		t.Errorf("unexpected error for empty paths: %v", err)
	}
}

func Test_ValidateWorkDirNotExcluded_Handles_Similar_Path_Names(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	excludedDir := filepath.Join(dir, "project")
	workDir := filepath.Join(dir, "project-new") // Similar but different

	mustCreateDir(t, excludedDir)
	mustCreateDir(t, workDir)

	paths := []ResolvedPath{
		{Resolved: excludedDir, Access: PathAccessExclude, Source: PathSourceCLI},
	}

	// workDir is "project-new", not under "project" - should be OK
	err := ValidateWorkDirNotExcluded(paths, workDir)
	if err != nil {
		t.Errorf("unexpected error for similar but different path name: %v", err)
	}
}

// ============================================================================
// isPathUnder tests
// ============================================================================

func Test_isPathUnder_Returns_True_When_Equal(t *testing.T) {
	t.Parallel()

	if !isPathUnder("/home/user", "/home/user") {
		t.Error("isPathUnder should return true when paths are equal")
	}
}

func Test_isPathUnder_Returns_True_When_Child(t *testing.T) {
	t.Parallel()

	if !isPathUnder("/home/user/project", "/home/user") {
		t.Error("isPathUnder should return true when child is under parent")
	}
}

func Test_isPathUnder_Returns_True_When_Deep_Child(t *testing.T) {
	t.Parallel()

	if !isPathUnder("/home/user/project/src/main.go", "/home/user") {
		t.Error("isPathUnder should return true for deep nested child")
	}
}

func Test_isPathUnder_Returns_False_When_Sibling(t *testing.T) {
	t.Parallel()

	if isPathUnder("/home/alice", "/home/bob") {
		t.Error("isPathUnder should return false for siblings")
	}
}

func Test_isPathUnder_Returns_False_When_Similar_Prefix(t *testing.T) {
	t.Parallel()

	// "/home/user-new" is NOT under "/home/user"
	if isPathUnder("/home/user-new", "/home/user") {
		t.Error("isPathUnder should return false for similar prefix without separator")
	}
}

func Test_isPathUnder_Returns_False_When_Parent(t *testing.T) {
	t.Parallel()

	if isPathUnder("/home", "/home/user") {
		t.Error("isPathUnder should return false when 'child' is actually parent")
	}
}

func Test_ErrWorkDirExcluded_Contains_Hint(t *testing.T) {
	t.Parallel()

	// Create a temporary directory and exclude it
	tmpDir := t.TempDir()
	excludedDir := tmpDir + "/excluded"

	err := os.MkdirAll(excludedDir, 0o750)
	if err != nil {
		t.Fatal(err)
	}

	paths := []ResolvedPath{
		{
			Original: excludedDir,
			Resolved: excludedDir,
			Access:   PathAccessExclude,
			Source:   PathSourceCLI,
		},
	}

	// Working directory is inside excluded path
	workDir := excludedDir + "/project"

	err = os.MkdirAll(workDir, 0o750)
	if err != nil {
		t.Fatal(err)
	}

	validationErr := ValidateWorkDirNotExcluded(paths, workDir)
	if validationErr == nil {
		t.Fatal("expected error for workdir inside excluded path")
	}

	// Error should contain actionable hint
	errMsg := validationErr.Error()
	if !strings.Contains(errMsg, "change directory") || !strings.Contains(errMsg, "remove exclusion") {
		t.Errorf("error should suggest changing directory or removing exclusion, got: %s", errMsg)
	}
}
