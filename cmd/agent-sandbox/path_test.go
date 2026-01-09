package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
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
// ExpandGlob tests - Symlink resolution
// ============================================================================

func Test_ExpandGlob_Resolves_Symlinks_In_Results(t *testing.T) {
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

	// Result should be the resolved real path, not the symlink
	if len(result) != 1 {
		t.Fatalf("expected 1 match, got %d: %v", len(result), result)
	}

	if result[0] != realFile {
		t.Errorf("symlink should resolve to %q, got %q", realFile, result[0])
	}
}

func Test_ExpandGlob_Skips_Dangling_Symlinks(t *testing.T) {
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

	// Should only return the valid file, not the dangling symlink
	if len(result) != 1 {
		t.Errorf("expected 1 match (valid file only), got %d: %v", len(result), result)
	}

	if len(result) > 0 && result[0] != validFile {
		t.Errorf("expected %q, got %q", validFile, result[0])
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
