package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testOriginalContent is a constant used across tests for original file content.
const testOriginalContent = "original content"

// ============================================================================
// E2E tests for filesystem access verification
// These tests verify that bwrap mounts actually enforce access levels.
// ============================================================================

func Test_Sandbox_RoPath_Allows_Read(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a file outside the working directory that we'll mark as ro
	roDir := t.TempDir()
	content := "secret content from ro path"

	err := os.WriteFile(filepath.Join(roDir, "file.txt"), []byte(content), 0o644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Run cat on the ro path from inside the sandbox
	stdout, stderr, code := c.Run("--ro", roDir, "cat", filepath.Join(roDir, "file.txt"))

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, content) {
		t.Errorf("expected stdout to contain %q, got: %s", content, stdout)
	}
}

func Test_Sandbox_RoPath_Blocks_Write(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a read-only directory for testing
	roDir := t.TempDir()

	// Try to create a new file inside the ro path
	newFile := filepath.Join(roDir, "newfile.txt")
	_, stderr, code := c.Run("--ro", roDir, "touch", newFile)

	if code == 0 {
		t.Error("expected non-zero exit code when writing to ro path")
	}

	// The error message should indicate read-only
	if !strings.Contains(strings.ToLower(stderr), "read-only") {
		t.Errorf("expected read-only error in stderr, got: %s", stderr)
	}

	// Verify file was not created on the host
	_, statErr := os.Stat(newFile)
	if statErr == nil {
		t.Error("file should not have been created on host")
	}
}

func Test_Sandbox_RoPath_Blocks_Modify(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a file in a ro directory
	roDir := t.TempDir()
	testFile := filepath.Join(roDir, "existing.txt")

	err := os.WriteFile(testFile, []byte(testOriginalContent), 0o644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Try to modify the file via bash echo redirect
	_, stderr, code := c.Run("--ro", roDir, "bash", "-c", "echo 'modified' > "+testFile)

	if code == 0 {
		t.Error("expected non-zero exit code when modifying file in ro path")
	}

	// Verify the error is about read-only
	if !strings.Contains(strings.ToLower(stderr), "read-only") {
		t.Errorf("expected read-only error in stderr, got: %s", stderr)
	}

	// Verify file was not modified on the host
	hostContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
	}

	if string(hostContent) != testOriginalContent {
		t.Errorf("file should not have been modified, got: %s", hostContent)
	}
}

func Test_Sandbox_RoPath_Blocks_Delete(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a file in a ro directory
	roDir := t.TempDir()
	testFile := filepath.Join(roDir, "todelete.txt")

	err := os.WriteFile(testFile, []byte("content"), 0o644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Try to delete the file
	_, stderr, code := c.Run("--ro", roDir, "rm", testFile)

	if code == 0 {
		t.Error("expected non-zero exit code when deleting file in ro path")
	}

	// Verify the error is about read-only
	if !strings.Contains(strings.ToLower(stderr), "read-only") {
		t.Errorf("expected read-only error in stderr, got: %s", stderr)
	}

	// Verify file still exists on the host
	_, statErr := os.Stat(testFile)
	if statErr != nil {
		t.Error("file should not have been deleted on host")
	}
}

func Test_Sandbox_ExcludePath_Blocks_Ls(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a directory with files to exclude
	excludeDir := t.TempDir()
	secretFile := "supersecret.txt"

	err := os.WriteFile(filepath.Join(excludeDir, secretFile), []byte("secret data"), 0o644)
	if err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	// Run ls on the excluded directory
	stdout, _, _ := c.Run("--exclude", excludeDir, "ls", excludeDir)

	// The directory should appear empty or the file should not be visible
	if strings.Contains(stdout, secretFile) {
		t.Errorf("excluded file %q should not be visible in ls output: %s", secretFile, stdout)
	}
}

func Test_Sandbox_ExcludePath_Blocks_Cat(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a file to exclude
	excludeDir := t.TempDir()
	secretFile := filepath.Join(excludeDir, "secret.txt")

	err := os.WriteFile(secretFile, []byte("secret data"), 0o644)
	if err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	// Try to cat the excluded file
	stdout, stderr, code := c.Run("--exclude", excludeDir, "cat", secretFile)

	// Should fail - file is excluded
	if code == 0 {
		t.Error("expected non-zero exit code when reading excluded file")
	}

	// Should not contain the secret data
	if strings.Contains(stdout, "secret data") {
		t.Error("excluded file content should not be visible")
	}

	// Error should indicate file doesn't exist or permission denied
	stderrLower := strings.ToLower(stderr)
	if !strings.Contains(stderrLower, "no such file") && !strings.Contains(stderrLower, "permission denied") {
		t.Errorf("expected 'no such file' or 'permission denied' error, got: %s", stderr)
	}
}

func Test_Sandbox_ExcludedFile_Blocks_Cat(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a specific file to exclude (not a directory)
	excludeDir := t.TempDir()
	secretFile := filepath.Join(excludeDir, "secret.txt")
	otherFile := filepath.Join(excludeDir, "other.txt")

	err := os.WriteFile(secretFile, []byte("secret data"), 0o644)
	if err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	err = os.WriteFile(otherFile, []byte("public data"), 0o644)
	if err != nil {
		t.Fatalf("failed to create other file: %v", err)
	}

	// Exclude only the secret file, not the directory
	stdout, stderr, code := c.Run("--exclude", secretFile, "cat", secretFile)

	// Should fail - file is excluded
	if code == 0 {
		t.Error("expected non-zero exit code when reading excluded file")
	}

	// Should not contain the secret data
	if strings.Contains(stdout, "secret data") {
		t.Error("excluded file content should not be visible")
	}

	// Error should indicate permission denied (file exists but is unreadable)
	if !strings.Contains(strings.ToLower(stderr), "permission denied") {
		t.Errorf("expected 'permission denied' error for excluded file, got: %s", stderr)
	}

	// But the other file should still be readable
	stdout, stderr, code = c.Run("--exclude", secretFile, "cat", otherFile)

	if code != 0 {
		t.Fatalf("expected exit code 0 for non-excluded file, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "public data") {
		t.Errorf("expected 'public data' in stdout, got: %s", stdout)
	}
}

func Test_Sandbox_RwPath_Allows_Read(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a file in an rw directory
	rwDir := t.TempDir()
	content := "readable content"

	err := os.WriteFile(filepath.Join(rwDir, "file.txt"), []byte(content), 0o644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Read from the rw path
	stdout, stderr, code := c.Run("--rw", rwDir, "cat", filepath.Join(rwDir, "file.txt"))

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, content) {
		t.Errorf("expected stdout to contain %q, got: %s", content, stdout)
	}
}

func Test_Sandbox_RwPath_Allows_Write(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create an rw directory
	rwDir := t.TempDir()
	newFile := filepath.Join(rwDir, "created.txt")

	// Create a new file in the rw path
	_, stderr, code := c.Run("--rw", rwDir, "touch", newFile)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Verify file was created on the host
	_, statErr := os.Stat(newFile)
	if statErr != nil {
		t.Errorf("file should have been created on host: %v", statErr)
	}
}

func Test_Sandbox_RwPath_Allows_Modify(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a file in an rw directory
	rwDir := t.TempDir()
	testFile := filepath.Join(rwDir, "modify.txt")

	err := os.WriteFile(testFile, []byte("original"), 0o644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Modify the file via bash echo redirect
	_, stderr, code := c.Run("--rw", rwDir, "bash", "-c", "echo 'modified' > "+testFile)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Verify file was modified on the host
	hostContent, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatalf("failed to read test file: %v", err)
	}

	if !strings.Contains(string(hostContent), "modified") {
		t.Errorf("file should have been modified, got: %s", hostContent)
	}
}

func Test_Sandbox_RwPath_Allows_Delete(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a file in an rw directory
	rwDir := t.TempDir()
	testFile := filepath.Join(rwDir, "delete.txt")

	err := os.WriteFile(testFile, []byte("to be deleted"), 0o644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Delete the file
	_, stderr, code := c.Run("--rw", rwDir, "rm", testFile)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Verify file was deleted on the host
	_, statErr := os.Stat(testFile)
	if statErr == nil {
		t.Error("file should have been deleted on host")
	}
}

// ============================================================================
// Specificity tests - verify that more specific paths override less specific
// ============================================================================

func Test_Sandbox_Specificity_RoChildOfRwParent_Is_Ro(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a directory structure: parent (rw) with child (ro)
	parentDir := t.TempDir()
	childDir := filepath.Join(parentDir, "protected")

	err := os.MkdirAll(childDir, 0o750)
	if err != nil {
		t.Fatalf("failed to create child dir: %v", err)
	}

	// Create a file in the child that should be read-only
	protectedFile := filepath.Join(childDir, "protected.txt")

	err = os.WriteFile(protectedFile, []byte("protected content"), 0o644)
	if err != nil {
		t.Fatalf("failed to create protected file: %v", err)
	}

	// Parent is rw, child is ro - child should be read-only
	_, stderr, code := c.Run("--rw", parentDir, "--ro", childDir, "touch", filepath.Join(childDir, "newfile.txt"))

	if code == 0 {
		t.Error("expected non-zero exit code when writing to ro child of rw parent")
	}

	if !strings.Contains(strings.ToLower(stderr), "read-only") {
		t.Errorf("expected read-only error in stderr, got: %s", stderr)
	}

	// But writing to parent (outside child) should work
	parentFile := filepath.Join(parentDir, "allowed.txt")
	_, stderr, code = c.Run("--rw", parentDir, "--ro", childDir, "touch", parentFile)

	if code != 0 {
		t.Fatalf("expected exit code 0 for write to parent, got %d\nstderr: %s", code, stderr)
	}

	_, statErr := os.Stat(parentFile)
	if statErr != nil {
		t.Errorf("file should have been created in parent: %v", statErr)
	}
}

func Test_Sandbox_Specificity_RwChildOfRoParent_Is_Rw(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a directory structure: parent (ro) with child (rw)
	parentDir := t.TempDir()
	childDir := filepath.Join(parentDir, "writable")

	err := os.MkdirAll(childDir, 0o750)
	if err != nil {
		t.Fatalf("failed to create child dir: %v", err)
	}

	// Parent is ro, child is rw - child should be writable
	newFile := filepath.Join(childDir, "newfile.txt")
	_, stderr, code := c.Run("--ro", parentDir, "--rw", childDir, "touch", newFile)

	if code != 0 {
		t.Fatalf("expected exit code 0 for write to rw child, got %d\nstderr: %s", code, stderr)
	}

	_, statErr := os.Stat(newFile)
	if statErr != nil {
		t.Errorf("file should have been created in rw child: %v", statErr)
	}

	// But writing to parent (outside child) should fail
	parentFile := filepath.Join(parentDir, "blocked.txt")
	_, stderr, code = c.Run("--ro", parentDir, "--rw", childDir, "touch", parentFile)

	if code == 0 {
		t.Error("expected non-zero exit code when writing to ro parent")
	}

	if !strings.Contains(strings.ToLower(stderr), "read-only") {
		t.Errorf("expected read-only error in stderr, got: %s", stderr)
	}
}

func Test_Sandbox_Specificity_ExcludeChildOfRwParent_Is_Hidden(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a directory structure: parent (rw) with child (exclude)
	parentDir := t.TempDir()
	secretDir := filepath.Join(parentDir, "secrets")

	err := os.MkdirAll(secretDir, 0o750)
	if err != nil {
		t.Fatalf("failed to create secret dir: %v", err)
	}

	// Create files in both
	err = os.WriteFile(filepath.Join(parentDir, "public.txt"), []byte("public"), 0o644)
	if err != nil {
		t.Fatalf("failed to create public file: %v", err)
	}

	err = os.WriteFile(filepath.Join(secretDir, "secret.txt"), []byte("secret"), 0o644)
	if err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	// List parent - should see public.txt but not secrets contents
	stdout, stderr, code := c.Run("--rw", parentDir, "--exclude", secretDir, "ls", parentDir)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "public.txt") {
		t.Errorf("expected to see public.txt in ls output: %s", stdout)
	}

	// List the secrets dir - should be empty (tmpfs mounted)
	stdout, _, _ = c.Run("--rw", parentDir, "--exclude", secretDir, "ls", secretDir)

	if strings.Contains(stdout, "secret.txt") {
		t.Errorf("excluded file should not be visible: %s", stdout)
	}
}

func Test_Sandbox_Specificity_ExcludeFileInRwDir_Only_Hides_File(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a directory with multiple files
	rwDir := t.TempDir()
	secretFile := filepath.Join(rwDir, "secret.txt")
	publicFile := filepath.Join(rwDir, "public.txt")

	err := os.WriteFile(secretFile, []byte("secret data"), 0o644)
	if err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	err = os.WriteFile(publicFile, []byte("public data"), 0o644)
	if err != nil {
		t.Fatalf("failed to create public file: %v", err)
	}

	// Exclude only the secret file, make dir rw
	// Try to read the secret file - should fail
	stdout, stderr, code := c.Run("--rw", rwDir, "--exclude", secretFile, "cat", secretFile)

	if code == 0 {
		t.Error("expected non-zero exit code when reading excluded file")
	}

	if strings.Contains(stdout, "secret data") {
		t.Error("excluded file content should not be visible")
	}

	if !strings.Contains(strings.ToLower(stderr), "permission denied") {
		t.Errorf("expected permission denied error, got: %s", stderr)
	}

	// Reading public file should still work
	stdout, stderr, code = c.Run("--rw", rwDir, "--exclude", secretFile, "cat", publicFile)

	if code != 0 {
		t.Fatalf("expected exit code 0 for public file, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "public data") {
		t.Errorf("expected public data in output, got: %s", stdout)
	}

	// Writing to the directory should still work
	newFile := filepath.Join(rwDir, "new.txt")
	_, stderr, code = c.Run("--rw", rwDir, "--exclude", secretFile, "touch", newFile)

	if code != 0 {
		t.Fatalf("expected exit code 0 for creating new file, got %d\nstderr: %s", code, stderr)
	}

	_, statErr := os.Stat(newFile)
	if statErr != nil {
		t.Errorf("new file should have been created: %v", statErr)
	}
}

// ============================================================================
// Additional edge cases and error handling
// ============================================================================

func Test_Sandbox_NonExistentPath_Is_Skipped_Silently(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Use a path that doesn't exist - should not cause an error
	_, stderr, code := c.Run("--ro", "/nonexistent/path/that/does/not/exist", "echo", "hello")

	if code != 0 {
		t.Fatalf("expected exit code 0 (non-existent paths skipped), got %d\nstderr: %s", code, stderr)
	}
}

func Test_Sandbox_WorkingDirectory_Is_Writable_By_Default(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	// Create a separate work directory (not the same as HOME)
	// When HOME==WorkDir, specificity rules make home read-only
	homeDir := t.TempDir()
	workDir := t.TempDir()
	tmpDir := t.TempDir()

	c := &CLI{
		t:   t,
		Dir: workDir,
		Env: map[string]string{
			"HOME":   homeDir,
			"PATH":   systemPath(),
			"TMPDIR": tmpDir,
		},
	}

	// The working directory should be writable by default (per @base preset)
	newFile := "created-in-cwd.txt"
	_, stderr, code := c.Run("touch", newFile)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Verify file was created
	if !c.FileExistsAt(workDir, newFile) {
		t.Error("file should have been created in working directory")
	}
}

func Test_Sandbox_SymlinkTarget_Resolved_For_Ro(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a directory with a file, then symlink to it
	realDir := t.TempDir()
	content := "content via symlink"

	err := os.WriteFile(filepath.Join(realDir, "file.txt"), []byte(content), 0o644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Create a symlink to the real directory
	symlinkDir := t.TempDir()
	symlinkPath := filepath.Join(symlinkDir, "link")

	err = os.Symlink(realDir, symlinkPath)
	if err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// Mark the symlink as ro - the real directory should be mounted read-only
	stdout, stderr, code := c.Run("--ro", symlinkPath, "cat", filepath.Join(symlinkPath, "file.txt"))

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, content) {
		t.Errorf("expected content via symlink, got: %s", stdout)
	}

	// Should not be able to write via the symlink
	_, stderr, code = c.Run("--ro", symlinkPath, "touch", filepath.Join(symlinkPath, "newfile.txt"))

	if code == 0 {
		t.Error("expected non-zero exit code when writing to ro symlinked path")
	}

	if !strings.Contains(strings.ToLower(stderr), "read-only") {
		t.Errorf("expected read-only error, got: %s", stderr)
	}
}

func Test_Sandbox_Multiple_Ro_Paths(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create multiple directories to mark as ro
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	err := os.WriteFile(filepath.Join(dir1, "file1.txt"), []byte("content1"), 0o644)
	if err != nil {
		t.Fatalf("failed to create file1: %v", err)
	}

	err = os.WriteFile(filepath.Join(dir2, "file2.txt"), []byte("content2"), 0o644)
	if err != nil {
		t.Fatalf("failed to create file2: %v", err)
	}

	// Both directories should be readable
	stdout, stderr, code := c.Run("--ro", dir1, "--ro", dir2, "cat", filepath.Join(dir1, "file1.txt"))

	if code != 0 {
		t.Fatalf("expected exit code 0 for reading dir1, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "content1") {
		t.Errorf("expected content1, got: %s", stdout)
	}

	stdout, stderr, code = c.Run("--ro", dir1, "--ro", dir2, "cat", filepath.Join(dir2, "file2.txt"))

	if code != 0 {
		t.Fatalf("expected exit code 0 for reading dir2, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "content2") {
		t.Errorf("expected content2, got: %s", stdout)
	}

	// Neither should be writable
	_, _, code = c.Run("--ro", dir1, "--ro", dir2, "touch", filepath.Join(dir1, "new.txt"))
	if code == 0 {
		t.Error("expected write to dir1 to fail")
	}

	_, _, code = c.Run("--ro", dir1, "--ro", dir2, "touch", filepath.Join(dir2, "new.txt"))
	if code == 0 {
		t.Error("expected write to dir2 to fail")
	}
}

func Test_Sandbox_Multiple_Exclude_Paths(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create multiple directories to exclude
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	err := os.WriteFile(filepath.Join(dir1, "secret1.txt"), []byte("secret1"), 0o644)
	if err != nil {
		t.Fatalf("failed to create secret1: %v", err)
	}

	err = os.WriteFile(filepath.Join(dir2, "secret2.txt"), []byte("secret2"), 0o644)
	if err != nil {
		t.Fatalf("failed to create secret2: %v", err)
	}

	// List both directories - should be empty
	stdout, _, _ := c.Run("--exclude", dir1, "--exclude", dir2, "ls", dir1)

	if strings.Contains(stdout, "secret1.txt") {
		t.Errorf("excluded file in dir1 should not be visible: %s", stdout)
	}

	stdout, _, _ = c.Run("--exclude", dir1, "--exclude", dir2, "ls", dir2)

	if strings.Contains(stdout, "secret2.txt") {
		t.Errorf("excluded file in dir2 should not be visible: %s", stdout)
	}
}

func Test_Sandbox_ConfigFile_RoPath(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a temp directory with a config file
	configDir := t.TempDir()
	protectedDir := t.TempDir()

	// Create a file to protect
	protectedFile := filepath.Join(protectedDir, "protected.txt")

	err := os.WriteFile(protectedFile, []byte("protected"), 0o644)
	if err != nil {
		t.Fatalf("failed to create protected file: %v", err)
	}

	// Write config with the protected path as ro
	configPath := filepath.Join(configDir, "config.json")
	configContent := `{
		"filesystem": {
			"ro": ["` + protectedDir + `"]
		}
	}`

	err = os.WriteFile(configPath, []byte(configContent), 0o644)
	if err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Use the config file (--config, not -c)
	stdout, stderr, code := c.Run("--config", configPath, "cat", protectedFile)

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "protected") {
		t.Errorf("expected 'protected' in stdout, got: %s", stdout)
	}

	// Should not be able to write
	_, stderr, code = c.Run("--config", configPath, "touch", filepath.Join(protectedDir, "new.txt"))

	if code == 0 {
		t.Error("expected write to fail with config-specified ro path")
	}

	if !strings.Contains(strings.ToLower(stderr), "read-only") {
		t.Errorf("expected read-only error, got: %s", stderr)
	}
}

func Test_Sandbox_ConfigFile_ExcludePath(t *testing.T) {
	t.Parallel()

	RequireBwrap(t)

	c := NewCLITester(t)

	// Create a temp directory with a config file
	configDir := t.TempDir()
	secretDir := t.TempDir()

	// Create a secret file
	secretFile := filepath.Join(secretDir, "secret.txt")

	err := os.WriteFile(secretFile, []byte("secret data"), 0o644)
	if err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	// Write config with the secret path as excluded
	configPath := filepath.Join(configDir, "config.json")
	configContent := `{
		"filesystem": {
			"exclude": ["` + secretDir + `"]
		}
	}`

	err = os.WriteFile(configPath, []byte(configContent), 0o644)
	if err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Listing the excluded directory should not show the file (--config, not -c)
	stdout, _, _ := c.Run("--config", configPath, "ls", secretDir)

	if strings.Contains(stdout, "secret.txt") {
		t.Errorf("excluded file should not be visible via config: %s", stdout)
	}
}
