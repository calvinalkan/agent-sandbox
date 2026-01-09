package main

import (
	"errors"
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
