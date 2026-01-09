package main

import (
	"os"
	"path/filepath"
	"testing"
)

// ============================================================================
// BinaryLocations tests - Basic discovery
// ============================================================================

func Test_BinaryLocations_Finds_Binary_In_PATH(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	mustCreateExecutable(t, filepath.Join(binDir, "mybin"))

	env := map[string]string{"PATH": binDir}

	result := BinaryLocations("mybin", env)

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(result), result)
	}

	expected := filepath.Join(binDir, "mybin")
	if result[0].Path != expected {
		t.Errorf("Path = %q, want %q", result[0].Path, expected)
	}

	if result[0].Resolved != expected {
		t.Errorf("Resolved = %q, want %q", result[0].Resolved, expected)
	}

	if result[0].IsLink {
		t.Error("IsLink should be false for direct binary")
	}
}

func Test_BinaryLocations_Finds_Binary_In_Multiple_PATH_Dirs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bin1 := filepath.Join(dir, "bin1")
	bin2 := filepath.Join(dir, "bin2")

	mustCreateDir(t, bin1)
	mustCreateDir(t, bin2)
	mustCreateExecutable(t, filepath.Join(bin1, "mybin"))
	mustCreateExecutable(t, filepath.Join(bin2, "mybin"))

	env := map[string]string{"PATH": bin1 + ":" + bin2}

	result := BinaryLocations("mybin", env)

	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d: %v", len(result), result)
	}

	if result[0].Path != filepath.Join(bin1, "mybin") {
		t.Errorf("result[0].Path = %q, want %q", result[0].Path, filepath.Join(bin1, "mybin"))
	}

	if result[1].Path != filepath.Join(bin2, "mybin") {
		t.Errorf("result[1].Path = %q, want %q", result[1].Path, filepath.Join(bin2, "mybin"))
	}
}

func Test_BinaryLocations_Returns_Empty_When_Binary_Not_Found(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	// Don't create any binary

	env := map[string]string{"PATH": binDir}

	result := BinaryLocations("nonexistent", env)

	if len(result) != 0 {
		t.Errorf("expected empty result, got %d: %v", len(result), result)
	}
}

func Test_BinaryLocations_Returns_Empty_When_PATH_Empty(t *testing.T) {
	t.Parallel()

	env := map[string]string{"PATH": ""}

	result := BinaryLocations("mybin", env)

	if len(result) != 0 {
		t.Errorf("expected empty result for empty PATH, got %d", len(result))
	}
}

func Test_BinaryLocations_Returns_Empty_When_PATH_Missing(t *testing.T) {
	t.Parallel()

	env := map[string]string{}

	result := BinaryLocations("mybin", env)

	if len(result) != 0 {
		t.Errorf("expected empty result for missing PATH, got %d", len(result))
	}
}

// ============================================================================
// BinaryLocations tests - Symlink resolution
// ============================================================================

func Test_BinaryLocations_Resolves_Symlink_To_Real_Binary(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create real binary
	realBinDir := filepath.Join(dir, "real")
	mustCreateDir(t, realBinDir)
	realBin := filepath.Join(realBinDir, "realbin")
	mustCreateExecutable(t, realBin)

	// Create symlink in PATH
	linkDir := filepath.Join(dir, "bin")
	mustCreateDir(t, linkDir)
	linkPath := filepath.Join(linkDir, "mybin")

	err := os.Symlink(realBin, linkPath)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	env := map[string]string{"PATH": linkDir}

	result := BinaryLocations("mybin", env)

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(result), result)
	}

	if result[0].Path != linkPath {
		t.Errorf("Path = %q, want %q", result[0].Path, linkPath)
	}

	if result[0].Resolved != realBin {
		t.Errorf("Resolved = %q, want %q", result[0].Resolved, realBin)
	}

	if !result[0].IsLink {
		t.Error("IsLink should be true for symlink")
	}
}

func Test_BinaryLocations_Handles_Multiple_Symlinks_To_Same_Target(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create real binary
	realBinDir := filepath.Join(dir, "real")
	mustCreateDir(t, realBinDir)
	realBin := filepath.Join(realBinDir, "realbin")
	mustCreateExecutable(t, realBin)

	// Create two directories with symlinks to same target
	bin1 := filepath.Join(dir, "bin1")
	bin2 := filepath.Join(dir, "bin2")

	mustCreateDir(t, bin1)
	mustCreateDir(t, bin2)

	link1 := filepath.Join(bin1, "mybin")
	link2 := filepath.Join(bin2, "mybin")

	err := os.Symlink(realBin, link1)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	err = os.Symlink(realBin, link2)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	env := map[string]string{"PATH": bin1 + ":" + bin2}

	result := BinaryLocations("mybin", env)

	// Both symlinks should be found
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d: %v", len(result), result)
	}

	// Both should resolve to same target
	if result[0].Resolved != realBin || result[1].Resolved != realBin {
		t.Errorf("both should resolve to %q, got %q and %q", realBin, result[0].Resolved, result[1].Resolved)
	}

	// Both should be marked as symlinks
	if !result[0].IsLink || !result[1].IsLink {
		t.Error("both paths should be marked as symlinks")
	}
}

func Test_BinaryLocations_Handles_Chained_Symlinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create real binary
	realBinDir := filepath.Join(dir, "real")
	mustCreateDir(t, realBinDir)
	realBin := filepath.Join(realBinDir, "realbin")
	mustCreateExecutable(t, realBin)

	// Create chain: bin/mybin -> intermediate/link1 -> real/realbin
	intermediateDir := filepath.Join(dir, "intermediate")
	mustCreateDir(t, intermediateDir)
	intermediateLink := filepath.Join(intermediateDir, "link1")

	err := os.Symlink(realBin, intermediateLink)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	finalLink := filepath.Join(binDir, "mybin")

	err = os.Symlink(intermediateLink, finalLink)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	env := map[string]string{"PATH": binDir}

	result := BinaryLocations("mybin", env)

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(result), result)
	}

	// Should resolve all the way to real binary
	if result[0].Resolved != realBin {
		t.Errorf("Resolved = %q, want %q", result[0].Resolved, realBin)
	}

	if !result[0].IsLink {
		t.Error("IsLink should be true")
	}
}

func Test_BinaryLocations_Skips_Broken_Symlinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)

	// Create broken symlink
	brokenLink := filepath.Join(binDir, "mybin")

	err := os.Symlink("/nonexistent/target", brokenLink)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	env := map[string]string{"PATH": binDir}

	result := BinaryLocations("mybin", env)

	// Should skip broken symlink without error
	if len(result) != 0 {
		t.Errorf("expected empty result for broken symlink, got %d: %v", len(result), result)
	}
}

func Test_BinaryLocations_Relative_Symlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create real binary
	realBin := filepath.Join(dir, "realbin")
	mustCreateExecutable(t, realBin)

	// Create directory with relative symlink
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	linkPath := filepath.Join(binDir, "mybin")

	// Relative symlink: ../realbin
	err := os.Symlink("../realbin", linkPath)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	env := map[string]string{"PATH": binDir}

	result := BinaryLocations("mybin", env)

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(result), result)
	}

	// Should resolve relative symlink to absolute path
	if result[0].Resolved != realBin {
		t.Errorf("Resolved = %q, want %q", result[0].Resolved, realBin)
	}

	if !result[0].IsLink {
		t.Error("IsLink should be true")
	}
}

// ============================================================================
// BinaryLocations tests - Non-executable files
// ============================================================================

func Test_BinaryLocations_Ignores_NonExecutable_Files(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)

	// Create non-executable file
	nonExec := filepath.Join(binDir, "mybin")

	err := os.WriteFile(nonExec, []byte("#!/bin/bash"), 0o644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	env := map[string]string{"PATH": binDir}

	result := BinaryLocations("mybin", env)

	if len(result) != 0 {
		t.Errorf("expected empty result for non-executable, got %d: %v", len(result), result)
	}
}

func Test_BinaryLocations_Finds_Executable_Only(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)

	// Create non-executable file
	nonExec := filepath.Join(binDir, "nonexec")

	err := os.WriteFile(nonExec, []byte("#!/bin/bash"), 0o644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Create executable file
	execFile := filepath.Join(binDir, "mybin")
	mustCreateExecutable(t, execFile)

	env := map[string]string{"PATH": binDir}

	result := BinaryLocations("mybin", env)

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(result), result)
	}

	if result[0].Path != execFile {
		t.Errorf("Path = %q, want %q", result[0].Path, execFile)
	}
}

func Test_BinaryLocations_Ignores_Directories(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)

	// Create a directory with the binary name
	subdir := filepath.Join(binDir, "mybin")
	mustCreateDir(t, subdir)

	env := map[string]string{"PATH": binDir}

	result := BinaryLocations("mybin", env)

	// Should not match directory
	if len(result) != 0 {
		t.Errorf("expected empty result for directory, got %d: %v", len(result), result)
	}
}

// ============================================================================
// BinaryLocations tests - Duplicate PATH entries
// ============================================================================

func Test_BinaryLocations_Deduplicates_PATH_Entries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	mustCreateExecutable(t, filepath.Join(binDir, "mybin"))

	// Same directory listed twice in PATH
	env := map[string]string{"PATH": binDir + ":" + binDir + ":" + binDir}

	result := BinaryLocations("mybin", env)

	// Should only return one result despite duplicate PATH entries
	if len(result) != 1 {
		t.Fatalf("expected 1 result (deduplicated), got %d: %v", len(result), result)
	}
}

func Test_BinaryLocations_Handles_Empty_PATH_Entries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	mustCreateExecutable(t, filepath.Join(binDir, "mybin"))

	// PATH with empty entries (::)
	env := map[string]string{"PATH": ":" + binDir + "::"}

	result := BinaryLocations("mybin", env)

	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(result), result)
	}

	if result[0].Path != filepath.Join(binDir, "mybin") {
		t.Errorf("Path = %q, want %q", result[0].Path, filepath.Join(binDir, "mybin"))
	}
}

// ============================================================================
// BinaryLocations tests - Mixed scenarios
// ============================================================================

func Test_BinaryLocations_Mixed_Direct_And_Symlinks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create direct binary in bin1
	bin1 := filepath.Join(dir, "bin1")
	mustCreateDir(t, bin1)
	directBin := filepath.Join(bin1, "mybin")
	mustCreateExecutable(t, directBin)

	// Create symlink in bin2 pointing to different binary
	bin2 := filepath.Join(dir, "bin2")
	mustCreateDir(t, bin2)

	otherBin := filepath.Join(dir, "other")
	mustCreateExecutable(t, otherBin)

	linkPath := filepath.Join(bin2, "mybin")

	err := os.Symlink(otherBin, linkPath)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	env := map[string]string{"PATH": bin1 + ":" + bin2}

	result := BinaryLocations("mybin", env)

	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d: %v", len(result), result)
	}

	// First should be direct (not a link)
	if result[0].IsLink {
		t.Error("result[0] should not be a link")
	}

	if result[0].Path != result[0].Resolved {
		t.Errorf("direct binary: Path and Resolved should match, got %q and %q", result[0].Path, result[0].Resolved)
	}

	// Second should be a link
	if !result[1].IsLink {
		t.Error("result[1] should be a link")
	}

	if result[1].Resolved != otherBin {
		t.Errorf("result[1].Resolved = %q, want %q", result[1].Resolved, otherBin)
	}
}

func Test_BinaryLocations_Order_Matches_PATH_Order(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	bin1 := filepath.Join(dir, "bin1")
	bin2 := filepath.Join(dir, "bin2")
	bin3 := filepath.Join(dir, "bin3")

	mustCreateDir(t, bin1)
	mustCreateDir(t, bin2)
	mustCreateDir(t, bin3)
	mustCreateExecutable(t, filepath.Join(bin1, "mybin"))
	mustCreateExecutable(t, filepath.Join(bin2, "mybin"))
	mustCreateExecutable(t, filepath.Join(bin3, "mybin"))

	env := map[string]string{"PATH": bin1 + ":" + bin2 + ":" + bin3}

	result := BinaryLocations("mybin", env)

	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}

	// Results should be in PATH order
	expected := []string{
		filepath.Join(bin1, "mybin"),
		filepath.Join(bin2, "mybin"),
		filepath.Join(bin3, "mybin"),
	}

	for i, exp := range expected {
		if result[i].Path != exp {
			t.Errorf("result[%d].Path = %q, want %q", i, result[i].Path, exp)
		}
	}
}

// ============================================================================
// BinaryLocations tests - Real system binaries (integration)
// ============================================================================

func Test_BinaryLocations_Finds_Real_System_Binary(t *testing.T) {
	t.Parallel()

	// Use the real PATH from the system
	env := map[string]string{"PATH": os.Getenv("PATH")}

	// Try to find "ls" which should exist on any Unix system
	result := BinaryLocations("ls", env)

	if len(result) == 0 {
		t.Skip("ls not found in PATH, skipping test")
	}

	// At least one result should exist
	if result[0].Path == "" {
		t.Error("Path should not be empty")
	}

	if result[0].Resolved == "" {
		t.Error("Resolved should not be empty")
	}

	// Resolved should be an absolute path
	if !filepath.IsAbs(result[0].Resolved) {
		t.Errorf("Resolved should be absolute, got %q", result[0].Resolved)
	}
}

// ============================================================================
// isExecutable tests
// ============================================================================

func Test_isExecutable_Returns_True_For_Executable_File(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "exec")
	mustCreateExecutable(t, path)

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}

	if !isExecutable(info) {
		t.Error("isExecutable should return true for executable file")
	}
}

func Test_isExecutable_Returns_False_For_NonExecutable_File(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "nonexec")

	err := os.WriteFile(path, []byte("content"), 0o644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("failed to stat file: %v", err)
	}

	if isExecutable(info) {
		t.Error("isExecutable should return false for non-executable file")
	}
}

func Test_isExecutable_Returns_True_For_Symlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "link")

	err := os.WriteFile(target, []byte("content"), 0o644)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	err = os.Symlink(target, link)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("failed to stat link: %v", err)
	}

	// Symlinks are considered potentially executable
	if !isExecutable(info) {
		t.Error("isExecutable should return true for symlink")
	}
}

func Test_isExecutable_Returns_False_For_Directory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	mustCreateDir(t, subdir)

	info, err := os.Lstat(subdir)
	if err != nil {
		t.Fatalf("failed to stat directory: %v", err)
	}

	if isExecutable(info) {
		t.Error("isExecutable should return false for directory")
	}
}

func Test_isExecutable_With_Different_Execute_Bits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mode       os.FileMode
		executable bool
	}{
		{"no execute bits", 0o644, false},
		{"owner execute", 0o744, true},
		{"group execute", 0o654, true},
		{"other execute", 0o645, true},
		{"all execute", 0o755, true},
		{"only execute", 0o111, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "file")

			err := os.WriteFile(path, []byte("content"), tt.mode)
			if err != nil {
				t.Fatalf("failed to create file: %v", err)
			}

			info, err := os.Lstat(path)
			if err != nil {
				t.Fatalf("failed to stat file: %v", err)
			}

			got := isExecutable(info)
			if got != tt.executable {
				t.Errorf("isExecutable() = %v, want %v", got, tt.executable)
			}
		})
	}
}

// ============================================================================
// Test helpers
// ============================================================================

func mustCreateExecutable(t *testing.T, path string) {
	t.Helper()

	dir := filepath.Dir(path)

	err := os.MkdirAll(dir, 0o750)
	if err != nil {
		t.Fatalf("failed to create directory %s: %v", dir, err)
	}

	err = os.WriteFile(path, []byte("#!/bin/bash\necho hello\n"), 0o755)
	if err != nil {
		t.Fatalf("failed to create executable %s: %v", path, err)
	}
}
