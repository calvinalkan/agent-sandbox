package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// CLI provides a clean interface for running CLI commands in tests.
// It manages a temp directory and environment variables.
type CLI struct {
	t   *testing.T
	Dir string
	Env map[string]string
}

// NewCLITester creates a new test CLI with a temp directory.
func NewCLITester(t *testing.T) *CLI {
	t.Helper()

	return &CLI{
		t:   t,
		Dir: t.TempDir(),
		Env: map[string]string{},
	}
}

// NewCLITesterAt creates a CLI tester that runs from a specific directory.
func NewCLITesterAt(t *testing.T, dir string) *CLI {
	t.Helper()

	return &CLI{
		t:   t,
		Dir: dir,
		Env: map[string]string{},
	}
}

// Run executes the CLI with the given args and returns stdout, stderr, and exit code.
// Args should not include "agent-sandbox" or "--cwd" - those are added automatically.
func (c *CLI) Run(args ...string) (string, string, int) {
	return c.RunWithInput(nil, args...)
}

// RunInDir executes the CLI in a specific directory.
func (c *CLI) RunInDir(dir string, args ...string) (string, string, int) {
	var outBuf, errBuf bytes.Buffer

	fullArgs := append([]string{"agent-sandbox", "--cwd", dir}, args...)
	code := Run(nil, &outBuf, &errBuf, fullArgs, c.Env, nil)

	return outBuf.String(), errBuf.String(), code
}

// RunWithInput executes the CLI with stdin and args.
// stdin can be nil, an io.Reader, or a []string (joined with newlines).
func (c *CLI) RunWithInput(stdin any, args ...string) (string, string, int) {
	var inReader io.Reader

	switch v := stdin.(type) {
	case nil:
		inReader = nil
	case io.Reader:
		inReader = v
	case []string:
		inReader = strings.NewReader(strings.Join(v, "\n"))
	default:
		panic(fmt.Sprintf("RunWithInput: stdin must be nil, io.Reader, or []string, got %T", stdin))
	}

	var outBuf, errBuf bytes.Buffer

	fullArgs := append([]string{"agent-sandbox", "--cwd", c.Dir}, args...)
	code := Run(inReader, &outBuf, &errBuf, fullArgs, c.Env, nil)

	return outBuf.String(), errBuf.String(), code
}

// RunWithSignal executes the CLI with a signal channel for cancellation testing.
// Returns a channel that receives the exit code when the command completes.
// stdout/stderr are discarded to avoid race conditions with signal handler output.
func (c *CLI) RunWithSignal(sigCh chan os.Signal, args ...string) <-chan int {
	done := make(chan int, 1)

	go func() {
		fullArgs := append([]string{"agent-sandbox", "--cwd", c.Dir}, args...)

		code := Run(nil, io.Discard, io.Discard, fullArgs, c.Env, sigCh)
		done <- code
	}()

	return done
}

// MustRun executes the CLI and fails the test if the command returns non-zero.
// Returns trimmed stdout on success.
func (c *CLI) MustRun(args ...string) string {
	c.t.Helper()

	stdout, stderr, code := c.Run(args...)
	if code != 0 {
		c.t.Fatalf("command %v failed with exit code %d\nstderr: %s", args, code, stderr)
	}

	return strings.TrimSpace(stdout)
}

// MustFail executes the CLI and fails the test if the command succeeds.
// Returns trimmed stderr.
func (c *CLI) MustFail(args ...string) string {
	c.t.Helper()

	stdout, stderr, code := c.Run(args...)
	if code == 0 {
		c.t.Fatalf("command %v should have failed but succeeded\nstdout: %s", args, stdout)
	}

	return strings.TrimSpace(stderr)
}

// WriteFile writes content to a file in the test directory.
func (c *CLI) WriteFile(relPath, content string) {
	c.t.Helper()

	path := filepath.Join(c.Dir, relPath)
	dir := filepath.Dir(path)

	err := os.MkdirAll(dir, 0o750)
	if err != nil {
		c.t.Fatalf("failed to create dir %s: %v", dir, err)
	}

	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		c.t.Fatalf("failed to write file %s: %v", relPath, err)
	}
}

// WriteExecutable writes an executable script to a file in the test directory.
// Used for creating hook scripts that need to be executable.
// Uses explicit Open/Write/Sync/Close to avoid "text file busy" errors.
func (c *CLI) WriteExecutable(relPath, content string) {
	c.t.Helper()

	path := filepath.Join(c.Dir, relPath)
	dir := filepath.Dir(path)

	err := os.MkdirAll(dir, 0o750)
	if err != nil {
		c.t.Fatalf("failed to create dir %s: %v", dir, err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		c.t.Fatalf("failed to create executable %s: %v", relPath, err)
	}

	_, err = f.WriteString(content)
	if err != nil {
		_ = f.Close()

		c.t.Fatalf("failed to write executable %s: %v", relPath, err)
	}

	err = f.Sync()
	if err != nil {
		_ = f.Close()

		c.t.Fatalf("failed to sync executable %s: %v", relPath, err)
	}

	err = f.Close()
	if err != nil {
		c.t.Fatalf("failed to close executable %s: %v", relPath, err)
	}

	// Brief sleep to ensure filesystem has fully released the file.
	// This works around "text file busy" errors on some systems.
	time.Sleep(10 * time.Millisecond)
}

// ReadFile reads content from a file in the test directory.
func (c *CLI) ReadFile(relPath string) string {
	c.t.Helper()

	path := filepath.Join(c.Dir, relPath)

	content, err := os.ReadFile(path)
	if err != nil {
		c.t.Fatalf("failed to read file %s: %v", relPath, err)
	}

	return string(content)
}

// FileExists returns true if the file exists in the test directory.
func (c *CLI) FileExists(relPath string) bool {
	path := filepath.Join(c.Dir, relPath)
	_, err := os.Stat(path)

	return err == nil
}

// ReadFileAt reads content from a file at an absolute base directory.
func (c *CLI) ReadFileAt(baseDir, relPath string) string {
	c.t.Helper()

	path := filepath.Join(baseDir, relPath)

	content, err := os.ReadFile(path)
	if err != nil {
		c.t.Fatalf("failed to read file %s: %v", path, err)
	}

	return string(content)
}

// FileExistsAt returns true if the file exists at an absolute base directory.
func (c *CLI) FileExistsAt(baseDir, relPath string) bool {
	path := filepath.Join(baseDir, relPath)
	_, err := os.Stat(path)

	return err == nil
}

// stripANSI removes ANSI escape codes from a string.
// Used to normalize output for comparison regardless of TTY state.
func stripANSI(s string) string {
	// Simple approach: remove sequences starting with ESC[ and ending with m
	result := s
	for {
		start := strings.Index(result, "\033[")
		if start == -1 {
			break
		}

		end := strings.Index(result[start:], "m")
		if end == -1 {
			break
		}

		result = result[:start] + result[start+end+1:]
	}

	return result
}

// AssertContains fails the test if content doesn't contain substr.
// Strips ANSI codes from content before comparison to handle TTY/non-TTY differences.
func AssertContains(t *testing.T, content, substr string) {
	t.Helper()

	cleaned := stripANSI(content)
	if !strings.Contains(cleaned, substr) {
		t.Errorf("content should contain %q\ncontent:\n%s", substr, content)
	}
}

// AssertNotContains fails the test if content contains substr.
// Strips ANSI codes from content before comparison to handle TTY/non-TTY differences.
func AssertNotContains(t *testing.T, content, substr string) {
	t.Helper()

	cleaned := stripANSI(content)
	if strings.Contains(cleaned, substr) {
		t.Errorf("content should NOT contain %q\ncontent:\n%s", substr, content)
	}
}

// ============================================================================
// Git test helpers
// ============================================================================

// GitRepo provides helpers for creating and managing git repositories in tests.
type GitRepo struct {
	t   *testing.T
	Dir string
}

// NewGitRepo creates a new git repository in a temp directory.
// Skips the test if git is not installed.
func NewGitRepo(t *testing.T) *GitRepo {
	t.Helper()

	dir := t.TempDir()
	repo := &GitRepo{t: t, Dir: dir}
	repo.run("init")
	repo.run("config", "user.email", "test@test.com")
	repo.run("config", "user.name", "Test User")
	repo.run("config", "commit.gpgsign", "false") // Disable GPG signing for tests

	return repo
}

// NewGitRepoAt creates a git repository at the specified directory.
// Skips the test if git is not installed.
func NewGitRepoAt(t *testing.T, dir string) *GitRepo {
	t.Helper()

	err := os.MkdirAll(dir, 0o750)
	if err != nil {
		t.Fatalf("failed to create directory %s: %v", dir, err)
	}

	repo := &GitRepo{t: t, Dir: dir}
	repo.run("init")
	repo.run("config", "user.email", "test@test.com")
	repo.run("config", "user.name", "Test User")
	repo.run("config", "commit.gpgsign", "false") // Disable GPG signing for tests

	return repo
}

// WriteFile writes a file to the repository.
func (r *GitRepo) WriteFile(relPath, content string) {
	r.t.Helper()

	path := filepath.Join(r.Dir, relPath)
	dir := filepath.Dir(path)

	err := os.MkdirAll(dir, 0o750)
	if err != nil {
		r.t.Fatalf("failed to create dir %s: %v", dir, err)
	}

	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		r.t.Fatalf("failed to write file %s: %v", relPath, err)
	}
}

// Commit stages all files and creates a commit with the given message.
func (r *GitRepo) Commit(message string) {
	r.t.Helper()

	r.run("add", "-A")
	r.run("commit", "-m", message)
}

// AddWorktree creates a new worktree at the specified path with a new branch.
// Returns the worktree directory path.
func (r *GitRepo) AddWorktree(worktreeDir, branchName string) string {
	r.t.Helper()

	r.run("worktree", "add", worktreeDir, "-b", branchName)

	return worktreeDir
}

// run executes a git command in the repository directory.
// Skips the test if git is not available, fails on other errors.
func (r *GitRepo) run(args ...string) {
	r.t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir

	// Clear GIT_* environment variables to prevent interference from parent repo
	// when running tests inside a git worktree (common in development).
	cmd.Env = filterGitEnv(os.Environ())

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if git is not installed
		var execErr *exec.Error
		if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
			r.t.Skipf("git not installed, skipping test")
		}

		r.t.Fatalf("git %v failed: %v\noutput: %s", args, err, output)
	}
}

// filterGitEnv returns env with GIT_DIR, GIT_WORK_TREE, and GIT_INDEX_FILE removed.
// This prevents test git commands from inheriting parent repo state.
func filterGitEnv(env []string) []string {
	result := make([]string, 0, len(env))

	for _, e := range env {
		if strings.HasPrefix(e, "GIT_DIR=") ||
			strings.HasPrefix(e, "GIT_WORK_TREE=") ||
			strings.HasPrefix(e, "GIT_INDEX_FILE=") {
			continue
		}

		result = append(result, e)
	}

	return result
}
