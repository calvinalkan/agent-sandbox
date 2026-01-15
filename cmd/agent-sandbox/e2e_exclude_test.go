package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// E2E Tests: Exclude Directory Behavior
// ============================================================================

func Test_Exclude_Directory_Exists_But_Is_Empty(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a directory with a file inside
	c.WriteFile("secrets/key.txt", "SECRET")

	// Verify directory exists (test -d returns 0)
	_, stderr, code := c.Run("--exclude", "secrets", "test", "-d", "secrets")
	if code != 0 {
		t.Errorf("expected excluded dir to exist (test -d returns 0), got exit %d, stderr: %s", code, stderr)
	}

	// Verify directory is empty (ls returns nothing)
	stdout, stderr, code := c.Run("--exclude", "secrets", "ls", "secrets")
	if code != 0 {
		t.Errorf("expected ls on excluded dir to succeed, got exit %d, stderr: %s", code, stderr)
	}

	if strings.TrimSpace(stdout) != "" {
		t.Errorf("expected excluded dir to be empty, got: %q", stdout)
	}
}

func Test_Exclude_Directory_Contents_Return_ENOENT(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a directory with a file inside
	c.WriteFile("secrets/key.txt", "SECRET")

	// Verify accessing file inside excluded dir returns ENOENT
	_, stderr, code := c.Run("--exclude", "secrets", "cat", "secrets/key.txt")
	if code == 0 {
		t.Error("expected cat on file in excluded dir to fail")
	}

	AssertContains(t, stderr, "No such file or directory")
}

func Test_Exclude_Nested_Directory_Parent_Accessible(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create nested structure
	c.WriteFile("config/secrets/api.key", "KEY")
	c.WriteFile("config/settings.json", "{}")

	// Verify sibling file is accessible
	stdout, stderr, code := c.Run("--exclude", "config/secrets", "cat", "config/settings.json")
	if code != 0 {
		t.Errorf("expected sibling file to be readable, got exit %d, stderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "{}") {
		t.Errorf("expected settings.json content, got: %q", stdout)
	}

	// Verify excluded nested dir exists but is empty
	stdout, stderr, code = c.Run("--exclude", "config/secrets", "ls", "config/secrets")
	if code != 0 {
		t.Errorf("expected ls on nested excluded dir to succeed, got exit %d, stderr: %s", code, stderr)
	}

	if strings.TrimSpace(stdout) != "" {
		t.Errorf("expected nested excluded dir to be empty, got: %q", stdout)
	}
}

func Test_Exclude_Directory_Subdirectories_Not_Visible(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create directory with subdirectories
	c.WriteFile("secrets/aws/creds", "x")
	c.WriteFile("secrets/ssh/id_rsa", "x")

	// Verify subdirectories don't exist
	_, _, code := c.Run("--exclude", "secrets", "test", "-d", "secrets/aws")
	if code == 0 {
		t.Error("expected subdir of excluded dir to not exist")
	}

	// Verify files in subdirs return ENOENT
	_, stderr, code := c.Run("--exclude", "secrets", "cat", "secrets/ssh/id_rsa")
	if code == 0 {
		t.Error("expected file in subdir of excluded dir to fail")
	}

	AssertContains(t, stderr, "No such file or directory")
}

func Test_Exclude_Directory_Sibling_Unaffected(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create sibling directories
	c.WriteFile("secrets/password.txt", "SECRET")
	c.WriteFile("public/readme.txt", "PUBLIC")

	// Verify sibling directory is accessible
	stdout, _, code := c.Run("--exclude", "secrets", "cat", "public/readme.txt")
	if code != 0 {
		t.Error("expected sibling dir to be accessible")
	}

	if !strings.Contains(stdout, "PUBLIC") {
		t.Errorf("expected PUBLIC content, got: %q", stdout)
	}

	// Verify excluded dir exists but is empty
	stdout, _, code = c.Run("--exclude", "secrets", "ls", "secrets")
	if code != 0 {
		t.Error("expected excluded dir to exist")
	}

	if strings.TrimSpace(stdout) != "" {
		t.Errorf("expected excluded dir to be empty, got: %q", stdout)
	}
}

// ============================================================================
// E2E Tests: Exclude File Behavior
// ============================================================================

func Test_Exclude_File_Exists_But_Unreadable(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a file to exclude
	c.WriteFile(".env", "SECRET=x")

	// Verify file exists (test -e returns 0)
	_, stderr, code := c.Run("--exclude", ".env", "test", "-e", ".env")
	if code != 0 {
		t.Errorf("expected excluded file to exist (test -e returns 0), got exit %d, stderr: %s", code, stderr)
	}

	// Verify file is regular file (test -f returns 0)
	_, stderr, code = c.Run("--exclude", ".env", "test", "-f", ".env")
	if code != 0 {
		t.Errorf("expected excluded file to be regular file (test -f returns 0), got exit %d, stderr: %s", code, stderr)
	}
}

func Test_Exclude_File_Read_Returns_Permission_Denied(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a file to exclude
	c.WriteFile(".env", "SECRET=x")

	// Verify reading file returns Permission denied (EACCES)
	_, stderr, code := c.Run("--exclude", ".env", "cat", ".env")
	if code == 0 {
		t.Error("expected cat on excluded file to fail")
	}

	AssertContains(t, stderr, "Permission denied")
}

func Test_Exclude_File_Has_Mode_000(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a file to exclude
	c.WriteFile(".env", "SECRET=x")

	// Verify file has mode 000 (first 10 chars of ls -la output)
	stdout, stderr, code := c.Run("--exclude", ".env", "ls", "-la", ".env")
	if code != 0 {
		t.Errorf("expected ls -la on excluded file to succeed, got exit %d, stderr: %s", code, stderr)
	}

	// Mode 000 appears as "----------" in ls output
	if !strings.Contains(stdout, "----------") {
		t.Errorf("expected excluded file to have mode 000 (----------), got: %s", stdout)
	}
}

func Test_Exclude_File_Sibling_Accessible(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create sibling files
	c.WriteFile(".env", "SECRET=x")
	c.WriteFile(".env.example", "KEY=")

	// Verify sibling file is readable
	stdout, _, code := c.Run("--exclude", ".env", "cat", ".env.example")
	if code != 0 {
		t.Error("expected sibling file to be accessible")
	}

	if !strings.Contains(stdout, "KEY=") {
		t.Errorf("expected .env.example content, got: %q", stdout)
	}

	// Verify excluded file returns Permission denied
	_, stderr, code := c.Run("--exclude", ".env", "cat", ".env")
	if code == 0 {
		t.Error("expected excluded file to fail")
	}

	AssertContains(t, stderr, "Permission denied")
}

func Test_Exclude_File_In_Subdirectory(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create files in subdirectory
	c.WriteFile("config/secrets.json", `{"key":"x"}`)
	c.WriteFile("config/settings.json", `{}`)

	// Verify sibling file is readable
	stdout, _, code := c.Run("--exclude", "config/secrets.json", "cat", "config/settings.json")
	if code != 0 {
		t.Error("expected sibling file to be accessible")
	}

	if !strings.Contains(stdout, "{}") {
		t.Errorf("expected settings.json content, got: %q", stdout)
	}

	// Verify excluded file returns Permission denied
	_, stderr, code := c.Run("--exclude", "config/secrets.json", "cat", "config/secrets.json")
	if code == 0 {
		t.Error("expected excluded file to fail")
	}

	AssertContains(t, stderr, "Permission denied")
}

// ============================================================================
// E2E Tests: Language-Specific Exclude Behavior
// ============================================================================

// requireNode checks if node is available and returns its path.
// Returns empty string and skips test if node is not found.
func requireNode(t *testing.T) string {
	t.Helper()
	// Check if node is in system path
	nodePath, err := exec.LookPath("node")
	if err != nil {
		t.Skip("test requires node, not installed in system PATH")
	}

	return nodePath
}

func Test_Exclude_Node_Existsync_Readfilesync(t *testing.T) {
	t.Parallel()

	nodePath := requireNode(t)

	c := NewCLITester(t)
	// Add node to PATH in sandbox (node may be in non-standard location)
	c.Env["PATH"] = filepath.Dir(nodePath) + ":" + c.Env["PATH"]

	// Create excluded file
	c.WriteFile(".env", "SECRET=x")

	// Node script that checks existence and tries to read
	script := `
const fs = require('fs');
console.log('exists:', fs.existsSync('.env'));
try {
  fs.readFileSync('.env');
  console.log('read: success');
} catch (e) {
  console.log('read: error:', e.code);
}
`
	c.WriteFile("test.js", script)

	stdout, stderr, code := c.Run("--exclude", ".env", "node", "test.js")
	if code != 0 {
		t.Errorf("expected node script to succeed, got exit %d, stderr: %s", code, stderr)
	}

	// File should exist
	if !strings.Contains(stdout, "exists: true") {
		t.Errorf("expected exists: true, got: %s", stdout)
	}

	// Read should fail with EACCES
	if !strings.Contains(stdout, "read: error: EACCES") {
		t.Errorf("expected read error EACCES, got: %s", stdout)
	}
}

func Test_Exclude_Node_Readdirsync_Directory(t *testing.T) {
	t.Parallel()

	nodePath := requireNode(t)

	c := NewCLITester(t)
	// Add node to PATH in sandbox
	c.Env["PATH"] = filepath.Dir(nodePath) + ":" + c.Env["PATH"]

	// Create excluded directory with contents
	c.WriteFile("secrets/key.txt", "SECRET")
	c.WriteFile("secrets/pass.txt", "PASSWORD")

	// Node script that checks directory and lists contents
	script := `
const fs = require('fs');
console.log('is-dir:', fs.statSync('secrets').isDirectory());
console.log('contents:', fs.readdirSync('secrets').join(','));
`
	c.WriteFile("test.js", script)

	stdout, stderr, code := c.Run("--exclude", "secrets", "node", "test.js")
	if code != 0 {
		t.Errorf("expected node script to succeed, got exit %d, stderr: %s", code, stderr)
	}

	// Directory should exist
	if !strings.Contains(stdout, "is-dir: true") {
		t.Errorf("expected is-dir: true, got: %s", stdout)
	}

	// Directory should be empty
	if !strings.Contains(stdout, "contents:") || strings.Contains(stdout, "key.txt") {
		t.Errorf("expected empty directory contents, got: %s", stdout)
	}
}

func Test_Exclude_Python_Exists_Read_Listdir(t *testing.T) {
	t.Parallel()

	// Skip if python isn't available (try python3 first, then python)
	pythonCmd := "python3"

	_, err := exec.LookPath(pythonCmd)
	if err != nil {
		pythonCmd = "python"

		_, err = exec.LookPath(pythonCmd)
		if err != nil {
			t.Skip("test requires python, not installed")
		}
	}

	c := NewCLITester(t)

	// Create excluded file
	c.WriteFile(".env", "SECRET=x")

	// Python script that checks existence and tries to read
	script := `
import os
print('exists:', os.path.exists('.env'))
try:
    with open('.env') as f:
        f.read()
    print('read: success')
except PermissionError as e:
    print('read: PermissionError')
except Exception as e:
    print('read: error:', type(e).__name__)
`
	c.WriteFile("test.py", script)

	stdout, stderr, code := c.Run("--exclude", ".env", pythonCmd, "test.py")
	if code != 0 {
		t.Errorf("expected python script to succeed, got exit %d, stderr: %s", code, stderr)
	}

	// File should exist
	if !strings.Contains(stdout, "exists: True") {
		t.Errorf("expected exists: True, got: %s", stdout)
	}

	// Read should fail with PermissionError
	if !strings.Contains(stdout, "read: PermissionError") {
		t.Errorf("expected read PermissionError, got: %s", stdout)
	}
}

func Test_Exclude_Python_Listdir_Directory(t *testing.T) {
	t.Parallel()

	// Skip if python isn't available
	pythonCmd := "python3"

	_, err := exec.LookPath(pythonCmd)
	if err != nil {
		pythonCmd = "python"

		_, err = exec.LookPath(pythonCmd)
		if err != nil {
			t.Skip("test requires python, not installed")
		}
	}

	c := NewCLITester(t)

	// Create excluded directory with contents
	c.WriteFile("secrets/key.txt", "SECRET")
	c.WriteFile("secrets/pass.txt", "PASSWORD")

	// Python script that checks directory and lists contents
	script := `
import os
print('is-dir:', os.path.isdir('secrets'))
print('contents:', os.listdir('secrets'))
`
	c.WriteFile("test.py", script)

	stdout, stderr, code := c.Run("--exclude", "secrets", pythonCmd, "test.py")
	if code != 0 {
		t.Errorf("expected python script to succeed, got exit %d, stderr: %s", code, stderr)
	}

	// Directory should exist
	if !strings.Contains(stdout, "is-dir: True") {
		t.Errorf("expected is-dir: True, got: %s", stdout)
	}

	// Directory should be empty
	if !strings.Contains(stdout, "contents: []") {
		t.Errorf("expected empty directory contents [], got: %s", stdout)
	}
}

// ============================================================================
// E2E Tests: Edge Cases
// ============================================================================

func Test_Exclude_Symlink_To_Directory_Excludes_Both(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a directory with content and a symlink to it
	c.WriteFile("real_secrets/key.txt", "SECRET")

	err := os.Symlink(filepath.Join(c.Dir, "real_secrets"), filepath.Join(c.Dir, "secrets"))
	if err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// Exclude the symlink - this resolves to real_secrets, so both are excluded
	stdout, _, code := c.Run("--exclude", "secrets", "ls", "secrets")
	if code != 0 {
		t.Errorf("expected ls on excluded symlink to succeed, got exit %d", code)
	}

	if strings.TrimSpace(stdout) != "" {
		t.Errorf("expected excluded symlink dir to be empty, got: %q", stdout)
	}

	// The original directory is ALSO excluded because symlinks are resolved
	// This is the expected behavior per SPEC: symlinks are resolved before mounting
	stdout, _, code = c.Run("--exclude", "secrets", "ls", "real_secrets")
	if code != 0 {
		t.Error("expected ls on original dir to succeed")
	}

	if strings.TrimSpace(stdout) != "" {
		t.Errorf("expected original dir to be empty (excluded via resolved symlink), got: %q", stdout)
	}
}

func Test_Exclude_Symlink_To_File_Excludes_Both(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a file and a symlink to it
	c.WriteFile("real_env", "SECRET=x")

	err := os.Symlink(filepath.Join(c.Dir, "real_env"), filepath.Join(c.Dir, ".env"))
	if err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// Exclude the symlink - file should exist but be unreadable
	_, stderr, code := c.Run("--exclude", ".env", "cat", ".env")
	if code == 0 {
		t.Error("expected cat on excluded symlink to fail")
	}

	AssertContains(t, stderr, "Permission denied")

	// The original file is ALSO excluded because symlinks are resolved
	// This is the expected behavior per SPEC: symlinks are resolved before mounting
	_, stderr, code = c.Run("--exclude", ".env", "cat", "real_env")
	if code == 0 {
		t.Error("expected original file to be excluded (via resolved symlink)")
	}

	AssertContains(t, stderr, "Permission denied")
}

func Test_Exclude_Nonexistent_Path_No_Error(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Exclude a path that doesn't exist - should not cause an error
	stdout, stderr, code := c.Run("--exclude", "nonexistent", "echo", "hello")
	if code != 0 {
		t.Errorf("expected success when excluding nonexistent path, got exit %d, stderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "hello") {
		t.Errorf("expected 'hello' output, got: %q", stdout)
	}
}

func Test_Exclude_Multiple_Paths_In_Same_Directory(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create multiple files in same directory
	c.WriteFile("config/.env", "SECRET=x")
	c.WriteFile("config/.secrets.json", `{"key":"x"}`)
	c.WriteFile("config/settings.json", `{}`)

	// Exclude multiple files
	stdout, _, code := c.Run("--exclude", "config/.env", "--exclude", "config/.secrets.json", "cat", "config/settings.json")
	if code != 0 {
		t.Error("expected non-excluded file to be accessible")
	}

	if !strings.Contains(stdout, "{}") {
		t.Errorf("expected settings.json content, got: %q", stdout)
	}

	// Verify both excluded files return Permission denied
	_, stderr, code := c.Run("--exclude", "config/.env", "--exclude", "config/.secrets.json", "cat", "config/.env")
	if code == 0 {
		t.Error("expected excluded .env to fail")
	}

	AssertContains(t, stderr, "Permission denied")

	_, stderr, code = c.Run("--exclude", "config/.env", "--exclude", "config/.secrets.json", "cat", "config/.secrets.json")
	if code == 0 {
		t.Error("expected excluded .secrets.json to fail")
	}

	AssertContains(t, stderr, "Permission denied")
}

func Test_Exclude_Glob_Pattern_Matches_Multiple_Files(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create files matching a glob pattern
	c.WriteFile(".env.local", "LOCAL=x")
	c.WriteFile(".env.production", "PROD=x")
	c.WriteFile("readme.txt", "README")

	// Exclude using glob pattern - note: glob must match at startup
	stdout, _, code := c.Run("--exclude", ".env.*", "cat", "readme.txt")
	if code != 0 {
		t.Error("expected non-excluded file to be accessible")
	}

	if !strings.Contains(stdout, "README") {
		t.Errorf("expected README content, got: %q", stdout)
	}

	// Verify excluded files return Permission denied
	_, stderr, code := c.Run("--exclude", ".env.*", "cat", ".env.local")
	if code == 0 {
		t.Error("expected excluded .env.local to fail")
	}

	AssertContains(t, stderr, "Permission denied")

	_, stderr, code = c.Run("--exclude", ".env.*", "cat", ".env.production")
	if code == 0 {
		t.Error("expected excluded .env.production to fail")
	}

	AssertContains(t, stderr, "Permission denied")
}

func Test_Exclude_Glob_Pattern_Matches_Directories(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create directories matching a glob pattern
	c.WriteFile("secrets-dev/key.txt", "DEV")
	c.WriteFile("secrets-prod/key.txt", "PROD")
	c.WriteFile("config/settings.json", "{}")

	// Exclude using glob pattern
	stdout, _, code := c.Run("--exclude", "secrets-*", "cat", "config/settings.json")
	if code != 0 {
		t.Error("expected non-excluded dir to be accessible")
	}

	if !strings.Contains(stdout, "{}") {
		t.Errorf("expected settings.json content, got: %q", stdout)
	}

	// Verify excluded directories are empty
	stdout, _, code = c.Run("--exclude", "secrets-*", "ls", "secrets-dev")
	if code != 0 {
		t.Error("expected excluded dir to exist")
	}

	if strings.TrimSpace(stdout) != "" {
		t.Errorf("expected excluded dir to be empty, got: %q", stdout)
	}

	// Verify files inside excluded directories return ENOENT
	_, stderr, code := c.Run("--exclude", "secrets-*", "cat", "secrets-prod/key.txt")
	if code == 0 {
		t.Error("expected file in excluded dir to fail")
	}

	AssertContains(t, stderr, "No such file or directory")
}

func Test_Exclude_Deep_Nested_Directory(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create deeply nested structure
	c.WriteFile("a/b/c/d/secret.txt", "SECRET")
	c.WriteFile("a/b/c/public.txt", "PUBLIC")

	// Exclude deep nested directory
	stdout, _, code := c.Run("--exclude", "a/b/c/d", "cat", "a/b/c/public.txt")
	if code != 0 {
		t.Error("expected sibling file to be accessible")
	}

	if !strings.Contains(stdout, "PUBLIC") {
		t.Errorf("expected PUBLIC content, got: %q", stdout)
	}

	// Verify excluded dir is empty
	stdout, _, code = c.Run("--exclude", "a/b/c/d", "ls", "a/b/c/d")
	if code != 0 {
		t.Error("expected excluded dir to exist")
	}

	if strings.TrimSpace(stdout) != "" {
		t.Errorf("expected excluded dir to be empty, got: %q", stdout)
	}
}

func Test_Exclude_File_Write_Blocked(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a file to exclude
	c.WriteFile(".env", "SECRET=x")

	// Try to write to excluded file - should fail with Permission denied or Read-only
	_, stderr, code := c.Run("--exclude", ".env", "bash", "-c", "echo 'NEW' > .env")
	if code == 0 {
		t.Error("expected write to excluded file to fail")
	}
	// The excluded file is bind-mounted from an empty mode-000 file,
	// and the parent directory is read-only, so we get "Read-only file system"
	if !strings.Contains(stderr, "Permission denied") && !strings.Contains(stderr, "Read-only") {
		t.Errorf("expected Permission denied or Read-only error, got: %s", stderr)
	}

	// Verify original content unchanged (check outside sandbox)
	content := c.ReadFile(".env")
	if content != "SECRET=x" {
		t.Errorf("expected original content 'SECRET=x', got: %q", content)
	}
}

func Test_Exclude_Directory_Create_File_Isolated(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a directory to exclude
	c.WriteFile("secrets/existing.txt", "x")

	// Try to create a file in excluded directory
	// The directory is mounted as tmpfs, so writes succeed but are isolated
	stdout, stderr, code := c.Run("--exclude", "secrets", "bash", "-c", "touch secrets/new.txt && ls secrets")
	if code != 0 {
		t.Errorf("expected touch in excluded dir tmpfs to succeed, got exit %d, stderr: %s", code, stderr)
	}

	// The new file should exist inside the sandbox (tmpfs is writable)
	if !strings.Contains(stdout, "new.txt") {
		t.Errorf("expected new.txt to exist in sandbox tmpfs, got: %s", stdout)
	}

	// But original files should not be visible (tmpfs is empty)
	if strings.Contains(stdout, "existing.txt") {
		t.Errorf("expected original files to be hidden, got: %s", stdout)
	}

	// Verify file doesn't exist outside sandbox (changes are isolated)
	if c.FileExists("secrets/new.txt") {
		t.Error("file should not have been created in real directory")
	}
}

func Test_Exclude_File_Delete_Blocked(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a file to exclude
	c.WriteFile(".env", "SECRET=x")

	// Try to delete excluded file - should fail
	_, stderr, code := c.Run("--exclude", ".env", "rm", ".env")
	if code == 0 {
		t.Error("expected delete of excluded file to fail")
	}
	// rm will fail with Permission denied or similar
	if !strings.Contains(stderr, "Permission denied") && !strings.Contains(stderr, "Read-only") {
		t.Errorf("expected permission/read-only error, got: %s", stderr)
	}

	// Verify file still exists outside sandbox
	if !c.FileExists(".env") {
		t.Error("excluded file should not have been deleted")
	}
}

func Test_Exclude_Hidden_Files_Only(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create hidden and non-hidden files
	c.WriteFile(".secret", "HIDDEN")
	c.WriteFile("public", "VISIBLE")

	// Exclude hidden file
	stdout, _, code := c.Run("--exclude", ".secret", "cat", "public")
	if code != 0 {
		t.Error("expected public file to be accessible")
	}

	if !strings.Contains(stdout, "VISIBLE") {
		t.Errorf("expected VISIBLE content, got: %q", stdout)
	}

	// Verify hidden file is excluded
	_, stderr, code := c.Run("--exclude", ".secret", "cat", ".secret")
	if code == 0 {
		t.Error("expected hidden file to be excluded")
	}

	AssertContains(t, stderr, "Permission denied")
}

func Test_Exclude_Absolute_Path(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a file
	c.WriteFile("secret.txt", "SECRET")
	absPath := filepath.Join(c.Dir, "secret.txt")

	// Exclude using absolute path
	_, stderr, code := c.Run("--exclude", absPath, "cat", "secret.txt")
	if code == 0 {
		t.Error("expected excluded file to fail")
	}

	AssertContains(t, stderr, "Permission denied")
}

func Test_Exclude_Home_Relative_Path(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a file in home directory (which is the test dir)
	c.WriteFile(".secret", "SECRET")

	// Exclude using ~ relative path
	_, stderr, code := c.Run("--exclude", "~/.secret", "cat", ".secret")
	if code == 0 {
		t.Error("expected excluded file to fail")
	}

	AssertContains(t, stderr, "Permission denied")
}

// =============================================================================
// E2E Tests: WorkDir inside excluded path
// =============================================================================

// Note: This test does not use the typical NewCLITester pattern because we need
// workDir to be INSIDE an excluded parent directory, which requires custom setup.
func Test_Exclude_Succeeds_When_WorkDir_Is_Inside_Excluded_Parent(t *testing.T) {
	t.Parallel()

	parentDir := t.TempDir()
	workDir := filepath.Join(parentDir, "project")

	err := os.MkdirAll(workDir, 0o750)
	if err != nil {
		t.Fatal(err)
	}

	// Create a sibling host file that should be hidden by excluding the parent dir.
	secretPath := filepath.Join(parentDir, "secret.txt")
	mustWriteFile(t, secretPath, "top secret")

	c := NewCLITesterAt(t, parentDir)

	script := `set -eu
parent="$1"
work="$2"

pwd="$(pwd)"
if [ "$pwd" != "$work" ]; then
  echo "pwd mismatch: got=$pwd want=$work" >&2
  exit 1
fi

# This write should hit the host filesystem (workdir is re-exposed as a RW mount).
echo sandbox > from-sandbox.txt

# Parent dir is excluded (tmpfs), but the workdir should be re-exposed as the only entry.
entries="$(ls -A "$parent")"
if [ "$entries" != "project" ]; then
  echo "expected parent dir to contain only 'project', got: $entries" >&2
  exit 1
fi

if [ -e "$parent/secret.txt" ]; then
  echo "expected parent sibling file to be hidden" >&2
  exit 1
fi
`

	_, stderr, code := c.RunInDir(workDir, "--exclude", parentDir, "sh", "-c", script, "sh", parentDir, workDir)
	if code != 0 {
		t.Fatalf("expected success when workDir is inside excluded parent (workDir should be re-exposed), got exit %d, stderr: %s", code, stderr)
	}

	// Verify the write landed on the host filesystem.
	if got := mustReadFile(t, filepath.Join(workDir, "from-sandbox.txt")); got != "sandbox\n" {
		t.Fatalf("expected from-sandbox.txt to contain %q, got %q", "sandbox\\n", got)
	}

	// Verify the sibling secret file still exists on the host.
	if got := mustReadFile(t, secretPath); got != "top secret" {
		t.Fatalf("expected secret.txt to remain unchanged on host, got %q", got)
	}
}
