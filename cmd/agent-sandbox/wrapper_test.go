package main

import (
	"os"
	"path/filepath"
	"strings"
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

// ============================================================================
// GenerateWrappers tests
// ============================================================================

func Test_GenerateWrappers_Creates_Temp_Directory(t *testing.T) {
	t.Parallel()

	commands := map[string]CommandRule{
		"git": {Kind: CommandRuleBlock},
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	mustCreateExecutable(t, filepath.Join(binDir, "git"))

	binPaths := map[string][]BinaryPath{
		"git": {{Path: filepath.Join(binDir, "git"), Resolved: filepath.Join(binDir, "git")}},
	}

	setup, err := GenerateWrappers(commands, binPaths, "/usr/bin/agent-sandbox")
	if err != nil {
		t.Fatalf("GenerateWrappers failed: %v", err)
	}

	defer setup.Cleanup()

	if setup.TempDir == "" {
		t.Error("TempDir should not be empty")
	}

	// Verify temp directory exists
	info, err := os.Stat(setup.TempDir)
	if err != nil {
		t.Fatalf("temp directory does not exist: %v", err)
	}

	if !info.IsDir() {
		t.Error("TempDir should be a directory")
	}

	// Verify temp directory name pattern
	if !strings.Contains(setup.TempDir, "agent-sandbox-wrappers-") {
		t.Errorf("TempDir name should contain 'agent-sandbox-wrappers-', got %q", setup.TempDir)
	}
}

func Test_GenerateWrappers_Creates_Deny_Script_For_Blocked_Command(t *testing.T) {
	t.Parallel()

	commands := map[string]CommandRule{
		"rm": {Kind: CommandRuleBlock},
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	mustCreateExecutable(t, filepath.Join(binDir, "rm"))

	binPaths := map[string][]BinaryPath{
		"rm": {{Path: filepath.Join(binDir, "rm"), Resolved: filepath.Join(binDir, "rm")}},
	}

	setup, err := GenerateWrappers(commands, binPaths, "/usr/bin/agent-sandbox")
	if err != nil {
		t.Fatalf("GenerateWrappers failed: %v", err)
	}

	defer setup.Cleanup()

	// Verify deny-binary script exists
	denyScript := filepath.Join(setup.TempDir, "deny-binary")

	content, err := os.ReadFile(denyScript)
	if err != nil {
		t.Fatalf("failed to read deny-binary script: %v", err)
	}

	// Verify script content
	if !strings.Contains(string(content), "#!/bin/bash") {
		t.Error("deny-binary should have bash shebang")
	}

	if !strings.Contains(string(content), "is blocked in this sandbox") {
		t.Error("deny-binary should contain 'blocked' message")
	}

	if !strings.Contains(string(content), "exit 1") {
		t.Error("deny-binary should exit with code 1")
	}
}

func Test_GenerateWrappers_Deny_Script_Is_Executable(t *testing.T) {
	t.Parallel()

	commands := map[string]CommandRule{
		"rm": {Kind: CommandRuleBlock},
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	mustCreateExecutable(t, filepath.Join(binDir, "rm"))

	binPaths := map[string][]BinaryPath{
		"rm": {{Path: filepath.Join(binDir, "rm"), Resolved: filepath.Join(binDir, "rm")}},
	}

	setup, err := GenerateWrappers(commands, binPaths, "/usr/bin/agent-sandbox")
	if err != nil {
		t.Fatalf("GenerateWrappers failed: %v", err)
	}

	defer setup.Cleanup()

	denyScript := filepath.Join(setup.TempDir, "deny-binary")

	info, err := os.Stat(denyScript)
	if err != nil {
		t.Fatalf("failed to stat deny-binary: %v", err)
	}

	// Check executable bit
	mode := info.Mode().Perm()
	if mode&0o111 == 0 {
		t.Errorf("deny-binary should be executable, got mode %o", mode)
	}
}

func Test_GenerateWrappers_Creates_Preset_Wrapper_Script(t *testing.T) {
	t.Parallel()

	commands := map[string]CommandRule{
		"git": {Kind: CommandRulePreset, Value: "@git"},
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	mustCreateExecutable(t, filepath.Join(binDir, "git"))

	binPaths := map[string][]BinaryPath{
		"git": {{Path: filepath.Join(binDir, "git"), Resolved: filepath.Join(binDir, "git")}},
	}

	sandboxBinary := "/run/sandbox/agent-sandbox"

	setup, err := GenerateWrappers(commands, binPaths, sandboxBinary)
	if err != nil {
		t.Fatalf("GenerateWrappers failed: %v", err)
	}

	defer setup.Cleanup()

	// Verify wrapper script exists
	wrapperScript := filepath.Join(setup.TempDir, "wrap-git")

	content, err := os.ReadFile(wrapperScript)
	if err != nil {
		t.Fatalf("failed to read wrapper script: %v", err)
	}

	// Verify script content
	if !strings.Contains(string(content), "#!/bin/bash") {
		t.Error("wrapper should have bash shebang")
	}

	if !strings.Contains(string(content), sandboxBinary) {
		t.Errorf("wrapper should call sandbox binary %q", sandboxBinary)
	}

	if !strings.Contains(string(content), "wrap-binary") {
		t.Error("wrapper should call wrap-binary subcommand")
	}

	if !strings.Contains(string(content), "--preset") {
		t.Error("wrapper should use --preset flag")
	}

	if !strings.Contains(string(content), "@git") {
		t.Error("wrapper should include preset name @git")
	}

	if !strings.Contains(string(content), `"$@"`) {
		t.Error("wrapper should pass through arguments with \"$@\"")
	}
}

func Test_GenerateWrappers_Creates_Custom_Wrapper_Script(t *testing.T) {
	t.Parallel()

	userScript := "/home/user/.config/agent-sandbox/npm-wrapper.sh"
	commands := map[string]CommandRule{
		"npm": {Kind: CommandRuleScript, Value: userScript},
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	mustCreateExecutable(t, filepath.Join(binDir, "npm"))

	binPaths := map[string][]BinaryPath{
		"npm": {{Path: filepath.Join(binDir, "npm"), Resolved: filepath.Join(binDir, "npm")}},
	}

	sandboxBinary := "/run/sandbox/agent-sandbox"

	setup, err := GenerateWrappers(commands, binPaths, sandboxBinary)
	if err != nil {
		t.Fatalf("GenerateWrappers failed: %v", err)
	}

	defer setup.Cleanup()

	// Verify wrapper script exists
	wrapperScript := filepath.Join(setup.TempDir, "wrap-npm")

	content, err := os.ReadFile(wrapperScript)
	if err != nil {
		t.Fatalf("failed to read wrapper script: %v", err)
	}

	// Verify script content
	if !strings.Contains(string(content), "#!/bin/bash") {
		t.Error("wrapper should have bash shebang")
	}

	if !strings.Contains(string(content), sandboxBinary) {
		t.Errorf("wrapper should call sandbox binary %q", sandboxBinary)
	}

	if !strings.Contains(string(content), "wrap-binary") {
		t.Error("wrapper should call wrap-binary subcommand")
	}

	if !strings.Contains(string(content), "--script") {
		t.Error("wrapper should use --script flag")
	}

	if !strings.Contains(string(content), userScript) {
		t.Errorf("wrapper should include user script path %q", userScript)
	}

	if !strings.Contains(string(content), `"$@"`) {
		t.Error("wrapper should pass through arguments with \"$@\"")
	}
}

func Test_GenerateWrappers_Raw_Rule_Adds_No_Mounts(t *testing.T) {
	t.Parallel()

	commands := map[string]CommandRule{
		"git": {Kind: CommandRuleRaw},
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	mustCreateExecutable(t, filepath.Join(binDir, "git"))

	binPaths := map[string][]BinaryPath{
		"git": {{Path: filepath.Join(binDir, "git"), Resolved: filepath.Join(binDir, "git")}},
	}

	setup, err := GenerateWrappers(commands, binPaths, "/usr/bin/agent-sandbox")
	if err != nil {
		t.Fatalf("GenerateWrappers failed: %v", err)
	}

	defer setup.Cleanup()

	// Raw rule should not add any mounts
	if len(setup.Mounts) != 0 {
		t.Errorf("raw rule should add no mounts, got %d", len(setup.Mounts))
	}
}

func Test_GenerateWrappers_Block_Rule_Mounts_Deny_Script_At_All_Locations(t *testing.T) {
	t.Parallel()

	commands := map[string]CommandRule{
		"rm": {Kind: CommandRuleBlock},
	}

	dir := t.TempDir()
	bin1 := filepath.Join(dir, "bin1")
	bin2 := filepath.Join(dir, "bin2")

	mustCreateDir(t, bin1)
	mustCreateDir(t, bin2)
	mustCreateExecutable(t, filepath.Join(bin1, "rm"))
	mustCreateExecutable(t, filepath.Join(bin2, "rm"))

	binPaths := map[string][]BinaryPath{
		"rm": {
			{Path: filepath.Join(bin1, "rm"), Resolved: filepath.Join(bin1, "rm")},
			{Path: filepath.Join(bin2, "rm"), Resolved: filepath.Join(bin2, "rm")},
		},
	}

	setup, err := GenerateWrappers(commands, binPaths, "/usr/bin/agent-sandbox")
	if err != nil {
		t.Fatalf("GenerateWrappers failed: %v", err)
	}

	defer setup.Cleanup()

	// Should have two mounts (one for each binary location)
	if len(setup.Mounts) != 2 {
		t.Fatalf("expected 2 mounts, got %d", len(setup.Mounts))
	}

	// Both should use the same deny-binary source
	denyScript := filepath.Join(setup.TempDir, "deny-binary")

	for i, mount := range setup.Mounts {
		if mount.Source != denyScript {
			t.Errorf("mount[%d].Source = %q, want %q", i, mount.Source, denyScript)
		}
	}

	// Check destinations
	destinations := make(map[string]bool)
	for _, mount := range setup.Mounts {
		destinations[mount.Destination] = true
	}

	if !destinations[filepath.Join(bin1, "rm")] {
		t.Error("missing mount for bin1/rm")
	}

	if !destinations[filepath.Join(bin2, "rm")] {
		t.Error("missing mount for bin2/rm")
	}
}

func Test_GenerateWrappers_Preset_Rule_Mounts_At_All_Locations(t *testing.T) {
	t.Parallel()

	commands := map[string]CommandRule{
		"git": {Kind: CommandRulePreset, Value: "@git"},
	}

	dir := t.TempDir()
	bin1 := filepath.Join(dir, "bin1")
	bin2 := filepath.Join(dir, "bin2")

	mustCreateDir(t, bin1)
	mustCreateDir(t, bin2)
	mustCreateExecutable(t, filepath.Join(bin1, "git"))
	mustCreateExecutable(t, filepath.Join(bin2, "git"))

	binPaths := map[string][]BinaryPath{
		"git": {
			{Path: filepath.Join(bin1, "git"), Resolved: filepath.Join(bin1, "git")},
			{Path: filepath.Join(bin2, "git"), Resolved: filepath.Join(bin2, "git")},
		},
	}

	setup, err := GenerateWrappers(commands, binPaths, "/usr/bin/agent-sandbox")
	if err != nil {
		t.Fatalf("GenerateWrappers failed: %v", err)
	}

	defer setup.Cleanup()

	// Should have two mounts
	if len(setup.Mounts) != 2 {
		t.Fatalf("expected 2 mounts, got %d", len(setup.Mounts))
	}

	// Both should use the same wrapper script
	wrapperScript := filepath.Join(setup.TempDir, "wrap-git")

	for i, mount := range setup.Mounts {
		if mount.Source != wrapperScript {
			t.Errorf("mount[%d].Source = %q, want %q", i, mount.Source, wrapperScript)
		}
	}
}

func Test_GenerateWrappers_Cleanup_Removes_Temp_Directory(t *testing.T) {
	t.Parallel()

	commands := map[string]CommandRule{
		"rm": {Kind: CommandRuleBlock},
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	mustCreateExecutable(t, filepath.Join(binDir, "rm"))

	binPaths := map[string][]BinaryPath{
		"rm": {{Path: filepath.Join(binDir, "rm"), Resolved: filepath.Join(binDir, "rm")}},
	}

	setup, err := GenerateWrappers(commands, binPaths, "/usr/bin/agent-sandbox")
	if err != nil {
		t.Fatalf("GenerateWrappers failed: %v", err)
	}

	tempDir := setup.TempDir

	// Verify temp dir exists before cleanup
	_, err = os.Stat(tempDir)
	if err != nil {
		t.Fatalf("temp dir should exist before cleanup: %v", err)
	}

	// Call cleanup
	setup.Cleanup()

	// Verify temp dir is removed
	_, err = os.Stat(tempDir)
	if !os.IsNotExist(err) {
		t.Errorf("temp dir should not exist after cleanup, err = %v", err)
	}
}

func Test_GenerateWrappers_Skips_Command_When_Binary_Not_Found(t *testing.T) {
	t.Parallel()

	commands := map[string]CommandRule{
		"rm":       {Kind: CommandRuleBlock},
		"npm":      {Kind: CommandRuleScript, Value: "/path/to/script"},
		"notfound": {Kind: CommandRulePreset, Value: "@preset"},
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	mustCreateExecutable(t, filepath.Join(binDir, "rm"))
	mustCreateExecutable(t, filepath.Join(binDir, "npm"))
	// Don't create "notfound" binary

	binPaths := map[string][]BinaryPath{
		"rm":  {{Path: filepath.Join(binDir, "rm"), Resolved: filepath.Join(binDir, "rm")}},
		"npm": {{Path: filepath.Join(binDir, "npm"), Resolved: filepath.Join(binDir, "npm")}},
		// No entry for "notfound"
	}

	setup, err := GenerateWrappers(commands, binPaths, "/usr/bin/agent-sandbox")
	if err != nil {
		t.Fatalf("GenerateWrappers failed: %v", err)
	}

	defer setup.Cleanup()

	// Should have 2 mounts (rm and npm), not 3
	if len(setup.Mounts) != 2 {
		t.Errorf("expected 2 mounts (skipping notfound), got %d", len(setup.Mounts))
	}
}

func Test_GenerateWrappers_Skips_Empty_Binary_Paths(t *testing.T) {
	t.Parallel()

	commands := map[string]CommandRule{
		"rm": {Kind: CommandRuleBlock},
	}

	binPaths := map[string][]BinaryPath{
		"rm": {}, // Empty slice
	}

	setup, err := GenerateWrappers(commands, binPaths, "/usr/bin/agent-sandbox")
	if err != nil {
		t.Fatalf("GenerateWrappers failed: %v", err)
	}

	defer setup.Cleanup()

	// Should have no mounts
	if len(setup.Mounts) != 0 {
		t.Errorf("expected 0 mounts for empty binary paths, got %d", len(setup.Mounts))
	}
}

func Test_GenerateWrappers_Unset_Rule_Adds_No_Mounts(t *testing.T) {
	t.Parallel()

	commands := map[string]CommandRule{
		"git": {Kind: CommandRuleUnset},
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	mustCreateExecutable(t, filepath.Join(binDir, "git"))

	binPaths := map[string][]BinaryPath{
		"git": {{Path: filepath.Join(binDir, "git"), Resolved: filepath.Join(binDir, "git")}},
	}

	setup, err := GenerateWrappers(commands, binPaths, "/usr/bin/agent-sandbox")
	if err != nil {
		t.Fatalf("GenerateWrappers failed: %v", err)
	}

	defer setup.Cleanup()

	// Unset rule should not add any mounts
	if len(setup.Mounts) != 0 {
		t.Errorf("unset rule should add no mounts, got %d", len(setup.Mounts))
	}
}

func Test_GenerateWrappers_Multiple_Commands(t *testing.T) {
	t.Parallel()

	commands := map[string]CommandRule{
		"rm":  {Kind: CommandRuleBlock},
		"git": {Kind: CommandRulePreset, Value: "@git"},
		"npm": {Kind: CommandRuleScript, Value: "/path/to/script"},
		"go":  {Kind: CommandRuleRaw},
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	mustCreateExecutable(t, filepath.Join(binDir, "rm"))
	mustCreateExecutable(t, filepath.Join(binDir, "git"))
	mustCreateExecutable(t, filepath.Join(binDir, "npm"))
	mustCreateExecutable(t, filepath.Join(binDir, "go"))

	binPaths := map[string][]BinaryPath{
		"rm":  {{Path: filepath.Join(binDir, "rm"), Resolved: filepath.Join(binDir, "rm")}},
		"git": {{Path: filepath.Join(binDir, "git"), Resolved: filepath.Join(binDir, "git")}},
		"npm": {{Path: filepath.Join(binDir, "npm"), Resolved: filepath.Join(binDir, "npm")}},
		"go":  {{Path: filepath.Join(binDir, "go"), Resolved: filepath.Join(binDir, "go")}},
	}

	setup, err := GenerateWrappers(commands, binPaths, "/usr/bin/agent-sandbox")
	if err != nil {
		t.Fatalf("GenerateWrappers failed: %v", err)
	}

	defer setup.Cleanup()

	// rm (block) + git (preset) + npm (script) = 3 mounts
	// go (raw) = 0 mounts
	if len(setup.Mounts) != 3 {
		t.Errorf("expected 3 mounts, got %d", len(setup.Mounts))
	}

	// Verify scripts exist
	denyScript := filepath.Join(setup.TempDir, "deny-binary")
	gitWrapper := filepath.Join(setup.TempDir, "wrap-git")
	npmWrapper := filepath.Join(setup.TempDir, "wrap-npm")

	for _, script := range []string{denyScript, gitWrapper, npmWrapper} {
		_, statErr := os.Stat(script)
		if statErr != nil {
			t.Errorf("script %q should exist: %v", script, statErr)
		}
	}

	// Verify no go wrapper exists
	goWrapper := filepath.Join(setup.TempDir, "wrap-go")
	_, statErr := os.Stat(goWrapper)

	if !os.IsNotExist(statErr) {
		t.Errorf("go wrapper should not exist (raw rule)")
	}
}

func Test_GenerateWrappers_Wrapper_Scripts_Are_Executable(t *testing.T) {
	t.Parallel()

	commands := map[string]CommandRule{
		"git": {Kind: CommandRulePreset, Value: "@git"},
		"npm": {Kind: CommandRuleScript, Value: "/path/to/script"},
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	mustCreateExecutable(t, filepath.Join(binDir, "git"))
	mustCreateExecutable(t, filepath.Join(binDir, "npm"))

	binPaths := map[string][]BinaryPath{
		"git": {{Path: filepath.Join(binDir, "git"), Resolved: filepath.Join(binDir, "git")}},
		"npm": {{Path: filepath.Join(binDir, "npm"), Resolved: filepath.Join(binDir, "npm")}},
	}

	setup, err := GenerateWrappers(commands, binPaths, "/usr/bin/agent-sandbox")
	if err != nil {
		t.Fatalf("GenerateWrappers failed: %v", err)
	}

	defer setup.Cleanup()

	// Check all wrapper scripts are executable
	scripts := []string{
		filepath.Join(setup.TempDir, "deny-binary"),
		filepath.Join(setup.TempDir, "wrap-git"),
		filepath.Join(setup.TempDir, "wrap-npm"),
	}

	for _, script := range scripts {
		info, err := os.Stat(script)
		if err != nil {
			t.Fatalf("failed to stat %q: %v", script, err)
		}

		mode := info.Mode().Perm()
		if mode&0o111 == 0 {
			t.Errorf("script %q should be executable, got mode %o", script, mode)
		}
	}
}

func Test_GenerateWrappers_Preset_Wrapper_No_PATH_Lookup(t *testing.T) {
	t.Parallel()

	commands := map[string]CommandRule{
		"git": {Kind: CommandRulePreset, Value: "@git"},
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	mustCreateExecutable(t, filepath.Join(binDir, "git"))

	binPaths := map[string][]BinaryPath{
		"git": {{Path: filepath.Join(binDir, "git"), Resolved: filepath.Join(binDir, "git")}},
	}

	// Use a path with special characters to verify proper quoting
	sandboxBinary := "/run/abc123/agent-sandbox"

	setup, err := GenerateWrappers(commands, binPaths, sandboxBinary)
	if err != nil {
		t.Fatalf("GenerateWrappers failed: %v", err)
	}

	defer setup.Cleanup()

	wrapperScript := filepath.Join(setup.TempDir, "wrap-git")

	content, err := os.ReadFile(wrapperScript)
	if err != nil {
		t.Fatalf("failed to read wrapper script: %v", err)
	}

	// Wrapper should NOT use PATH lookup (no bare "agent-sandbox")
	// It should use the full path to the sandbox binary
	scriptContent := string(content)

	// Should contain the exec line with full path
	if !strings.Contains(scriptContent, "exec \""+sandboxBinary+"\"") {
		t.Errorf("wrapper should exec sandbox binary with full path, got:\n%s", scriptContent)
	}
}

func Test_GenerateWrappers_Custom_Wrapper_Quotes_Script_Path(t *testing.T) {
	t.Parallel()

	// Use a path with spaces to verify proper quoting
	userScript := "/home/user/my scripts/npm-wrapper.sh"
	commands := map[string]CommandRule{
		"npm": {Kind: CommandRuleScript, Value: userScript},
	}

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mustCreateDir(t, binDir)
	mustCreateExecutable(t, filepath.Join(binDir, "npm"))

	binPaths := map[string][]BinaryPath{
		"npm": {{Path: filepath.Join(binDir, "npm"), Resolved: filepath.Join(binDir, "npm")}},
	}

	setup, err := GenerateWrappers(commands, binPaths, "/usr/bin/agent-sandbox")
	if err != nil {
		t.Fatalf("GenerateWrappers failed: %v", err)
	}

	defer setup.Cleanup()

	wrapperScript := filepath.Join(setup.TempDir, "wrap-npm")

	content, err := os.ReadFile(wrapperScript)
	if err != nil {
		t.Fatalf("failed to read wrapper script: %v", err)
	}

	// Script path should be quoted
	if !strings.Contains(string(content), "\""+userScript+"\"") {
		t.Errorf("wrapper should quote script path, got:\n%s", string(content))
	}
}
