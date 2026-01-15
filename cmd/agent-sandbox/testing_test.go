package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const gitCommand = "git"

// ============================================================================
// Test binary helpers
//
// These helpers spawn the compiled agent-sandbox binary as a subprocess.
// Use when you need to test the binary from the OUTSIDE, such as:
//   - Spawning a sandbox and checking behavior INSIDE it
//   - Testing nested sandbox scenarios (sandbox within sandbox)
//   - Testing the "check" command which detects if we're in a sandbox
//
// For most tests, prefer CLI.Run() which calls the Run() function directly
// in-process - it's faster and easier to debug.
// ============================================================================

// testBinary holds the path to the compiled agent-sandbox binary.
var testBinary string

// TestMain builds the agent-sandbox binary once for all tests.
func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "agent-sandbox-test-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir for test binary: %v\n", err)
		os.Exit(1)
	}

	testBinary = filepath.Join(tmpDir, "agent-sandbox")

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		_ = os.RemoveAll(tmpDir)

		fmt.Fprintln(os.Stderr, "failed to locate test source directory")
		os.Exit(1)
	}

	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))

	cmd := exec.Command("go", "build", "-o", testBinary, "./cmd/agent-sandbox")
	cmd.Dir = moduleRoot
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		_ = os.RemoveAll(tmpDir)

		fmt.Fprintf(os.Stderr, "failed to build test binary: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()

	_ = os.RemoveAll(tmpDir)

	os.Exit(code)
}

// GetTestBinaryPath returns the path to the compiled agent-sandbox binary.
func GetTestBinaryPath(t *testing.T) string {
	t.Helper()

	if testBinary == "" {
		t.Skip("test binary not built (run via go test, not individual test)")
	}

	return testBinary
}

// RunBinary spawns the compiled agent-sandbox binary as a subprocess.
// Returns stdout, stderr, and exit code.
func RunBinary(t *testing.T, args ...string) (string, string, int) {
	t.Helper()

	binary := GetTestBinaryPath(t)

	var outBuf, errBuf bytes.Buffer

	cmd := exec.Command(binary, args...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	code := 0

	exitErr := &exec.ExitError{}
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("failed to run binary: %v", err)
	}

	return outBuf.String(), errBuf.String(), code
}

// RunBinaryWithEnv spawns the compiled binary with custom environment.
func RunBinaryWithEnv(t *testing.T, env map[string]string, args ...string) (string, string, int) {
	t.Helper()

	binary := GetTestBinaryPath(t)

	var outBuf, errBuf bytes.Buffer

	cmd := exec.Command(binary, args...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	if _, ok := env["PATH"]; !ok {
		cmd.Env = append(cmd.Env, "PATH="+os.Getenv("PATH"))
	}

	err := cmd.Run()
	code := 0

	exitErr := &exec.ExitError{}
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("failed to run binary: %v", err)
	}

	return outBuf.String(), errBuf.String(), code
}

// ============================================================================
// Skip helpers
// ============================================================================

// RequireWrapperMounting skips the test if running inside a sandbox.
// Use for tests that verify wrapper mounting behavior, which only happens
// when spawning a new sandbox from outside.
func RequireWrapperMounting(t *testing.T) {
	t.Helper()

	_, err := os.Stat(sandboxBinaryPath)
	if err == nil {
		t.Skip("test requires wrapper mounting (only works outside sandbox)")
	}
}

// RequireDocker skips the test if docker is not installed or not running.
func RequireDocker(t *testing.T) {
	t.Helper()

	_, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("test requires docker")
	}

	err = exec.Command("docker", "info").Run()
	if err != nil {
		t.Skip("test requires docker daemon to be running")
	}
}

// ============================================================================
// CLI tester
//
// Runs the agent-sandbox CLI in-process by calling the Run() function directly.
// This is faster than spawning a subprocess and easier to debug.
//
// Use this for most tests. Only use RunBinary() when you need to test behavior
// that requires actually being inside a sandbox (e.g., nested sandboxes).
// ============================================================================

// CLI runs agent-sandbox commands in-process with a managed test environment.
type CLI struct {
	t   *testing.T
	Dir string
	Env map[string]string
}

// systemPath returns a minimal PATH with only system binary directories.
func systemPath() string {
	return "/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin"
}

// NewCLITester creates a CLI with a fresh temp directory as HOME and working dir.
func NewCLITester(t *testing.T) *CLI {
	t.Helper()

	dir := t.TempDir()
	tmpDir := t.TempDir()

	return &CLI{
		t:   t,
		Dir: dir,
		Env: map[string]string{
			"HOME":   dir,
			"PATH":   systemPath(),
			"TMPDIR": tmpDir,
		},
	}
}

// NewCLITesterAt creates a CLI that uses the specified directory as HOME and working dir.
func NewCLITesterAt(t *testing.T, dir string) *CLI {
	t.Helper()

	return &CLI{
		t:   t,
		Dir: dir,
		Env: map[string]string{
			"HOME":   dir,
			"PATH":   systemPath(),
			"TMPDIR": t.TempDir(),
		},
	}
}

// Run executes the CLI and returns stdout, stderr, and exit code.
func (c *CLI) Run(args ...string) (string, string, int) {
	return c.RunWithInput(nil, args...)
}

// RunInDir executes the CLI with a different working directory.
func (c *CLI) RunInDir(dir string, args ...string) (string, string, int) {
	var outBuf, errBuf bytes.Buffer

	fullArgs := append([]string{"agent-sandbox", "--cwd", dir}, args...)
	code := Run(nil, &outBuf, &errBuf, fullArgs, c.Env, nil)

	return outBuf.String(), errBuf.String(), code
}

// RunWithInput executes the CLI with stdin.
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
		panic(fmt.Sprintf("stdin must be nil, io.Reader, or []string, got %T", stdin))
	}

	var outBuf, errBuf bytes.Buffer

	fullArgs := append([]string{"agent-sandbox", "--cwd", c.Dir}, args...)
	code := Run(inReader, &outBuf, &errBuf, fullArgs, c.Env, nil)

	return outBuf.String(), errBuf.String(), code
}

// RunWithSignal executes the CLI with a signal channel for testing cancellation.
func (c *CLI) RunWithSignal(sigCh chan os.Signal, args ...string) <-chan int {
	done := make(chan int, 1)

	go func() {
		fullArgs := append([]string{"agent-sandbox", "--cwd", c.Dir}, args...)

		code := Run(nil, io.Discard, io.Discard, fullArgs, c.Env, sigCh)
		done <- code
	}()

	return done
}

// MustRun executes the CLI and fails the test if the command fails.
func (c *CLI) MustRun(args ...string) string {
	c.t.Helper()

	stdout, stderr, code := c.Run(args...)
	if code != 0 {
		c.t.Fatalf("command %v failed with exit code %d\nstderr: %s", args, code, stderr)
	}

	return strings.TrimSpace(stdout)
}

// MustFail executes the CLI and fails the test if the command succeeds.
func (c *CLI) MustFail(args ...string) string {
	c.t.Helper()

	stdout, stderr, code := c.Run(args...)
	if code == 0 {
		c.t.Fatalf("command %v should have failed\nstdout: %s", args, stdout)
	}

	return strings.TrimSpace(stderr)
}

// TempFile returns a path to a temp file that is writable inside the sandbox.
func (c *CLI) TempFile(name string) string {
	return filepath.Join(c.Env["TMPDIR"], name)
}

// WriteFile writes content to a file relative to the test directory.
func (c *CLI) WriteFile(relPath, content string) {
	c.t.Helper()

	path := filepath.Join(c.Dir, relPath)

	err := os.MkdirAll(filepath.Dir(path), 0o750)
	if err != nil {
		c.t.Fatalf("failed to create dir: %v", err)
	}

	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		c.t.Fatalf("failed to write file %s: %v", relPath, err)
	}
}

// WriteExecutable writes an executable script to a file.
func (c *CLI) WriteExecutable(relPath, content string) {
	c.t.Helper()

	path := filepath.Join(c.Dir, relPath)

	err := os.MkdirAll(filepath.Dir(path), 0o750)
	if err != nil {
		c.t.Fatalf("failed to create dir: %v", err)
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

	// Brief sleep to avoid "text file busy" errors on some systems.
	time.Sleep(10 * time.Millisecond)
}

// ReadFile reads content from a file relative to the test directory.
func (c *CLI) ReadFile(relPath string) string {
	c.t.Helper()

	content, err := os.ReadFile(filepath.Join(c.Dir, relPath))
	if err != nil {
		c.t.Fatalf("failed to read file %s: %v", relPath, err)
	}

	return string(content)
}

// FileExists returns true if the file exists relative to the test directory.
func (c *CLI) FileExists(relPath string) bool {
	_, err := os.Stat(filepath.Join(c.Dir, relPath))

	return err == nil
}

// ReadFileAt reads content from a file at an absolute path.
func (c *CLI) ReadFileAt(baseDir, relPath string) string {
	c.t.Helper()

	path := filepath.Join(baseDir, relPath)

	content, err := os.ReadFile(path)
	if err != nil {
		c.t.Fatalf("failed to read file %s: %v", path, err)
	}

	return string(content)
}

// FileExistsAt returns true if the file exists at an absolute path.
func (*CLI) FileExistsAt(baseDir, relPath string) bool {
	_, err := os.Stat(filepath.Join(baseDir, relPath))

	return err == nil
}

// ============================================================================
// Misc test helpers
// ============================================================================

// mustMkdir creates a directory, failing the test if it fails.
func mustMkdir(t *testing.T, path string) {
	t.Helper()

	err := os.MkdirAll(path, 0o750)
	if err != nil {
		t.Fatal(err)
	}
}

// mustWriteFile writes content to a file, failing the test if it fails.
func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()

	err := os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}

// mustReadFile reads content from a file, failing the test if it fails.
func mustReadFile(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(content)
}

// ============================================================================
// Assertions
// ============================================================================

// stripANSI removes ANSI escape codes from a string.
func stripANSI(s string) string {
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
func AssertContains(t *testing.T, content, substr string) {
	t.Helper()

	if !strings.Contains(stripANSI(content), substr) {
		t.Errorf("expected %q in:\n%s", substr, content)
	}
}

// AssertNotContains fails the test if content contains substr.
func AssertNotContains(t *testing.T, content, substr string) {
	t.Helper()

	if strings.Contains(stripANSI(content), substr) {
		t.Errorf("unexpected %q in:\n%s", substr, content)
	}
}

// ============================================================================
// Git helpers
// ============================================================================

// GitRepo provides helpers for creating git repositories in tests.
type GitRepo struct {
	t   *testing.T
	Dir string
}

func testdataTempDir(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate test source directory")
	}

	moduleRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	baseRoot := filepath.Join(moduleRoot, ".testdata", "tmp")

	err := os.MkdirAll(baseRoot, 0o750)
	if err != nil {
		t.Fatalf("failed to create temp root %s: %v", baseRoot, err)
	}

	tempDir := t.TempDir()
	dir := filepath.Join(baseRoot, filepath.Base(tempDir))

	err = os.Mkdir(dir, 0o750)
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})

	return dir
}

// NewGitRepo creates a new git repository in a temp directory.
func NewGitRepo(t *testing.T) *GitRepo {
	t.Helper()

	dir := testdataTempDir(t)
	repo := &GitRepo{t: t, Dir: dir}
	repo.run("init")
	repo.run("config", "user.email", "test@test.com")
	repo.run("config", "user.name", "Test User")
	repo.run("config", "commit.gpgsign", "false")

	return repo
}

// NewGitRepoAt creates a git repository at the specified directory.
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
	repo.run("config", "commit.gpgsign", "false")

	return repo
}

// WriteFile writes a file to the repository.
func (r *GitRepo) WriteFile(relPath, content string) {
	r.t.Helper()

	path := filepath.Join(r.Dir, relPath)

	err := os.MkdirAll(filepath.Dir(path), 0o750)
	if err != nil {
		r.t.Fatalf("failed to create dir: %v", err)
	}

	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		r.t.Fatalf("failed to write file %s: %v", relPath, err)
	}
}

// Commit stages all files and creates a commit.
func (r *GitRepo) Commit(message string) {
	r.t.Helper()

	r.run("add", "-A")
	r.run("commit", "-m", message)
}

// AddWorktree creates a new worktree with a new branch.
func (r *GitRepo) AddWorktree(worktreeDir, branchName string) {
	r.t.Helper()

	r.run("worktree", "add", worktreeDir, "-b", branchName)
}

// cleanGitEnv returns os.Environ() with GIT_* variables removed.
func cleanGitEnv() []string {
	var clean []string

	for _, env := range os.Environ() {
		key, _, _ := strings.Cut(env, "=")
		if !strings.HasPrefix(key, "GIT_") {
			clean = append(clean, env)
		}
	}

	return clean
}

// findRealGitBinary returns the path to the real git binary.
// When inside a sandbox, wrappers may block certain git operations.
func findRealGitBinary() string {
	const realGit = "/run/agent-sandbox/bin/git"

	_, statErr := os.Stat(realGit)
	if statErr == nil {
		return realGit
	}

	return gitCommand
}

func (r *GitRepo) run(args ...string) {
	r.t.Helper()

	cmd := exec.Command(findRealGitBinary(), args...)
	cmd.Dir = r.Dir
	cmd.Env = cleanGitEnv()

	output, err := cmd.CombinedOutput()
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
			r.t.Skipf("git not installed")
		}

		r.t.Fatalf("git %v failed: %v\noutput: %s", args, err, output)
	}
}
