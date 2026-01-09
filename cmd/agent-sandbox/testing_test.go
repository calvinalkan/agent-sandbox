package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Test binary management - build once, use in all tests
// ============================================================================

// osLinux is the GOOS value for Linux, defined as a constant to satisfy goconst.
const osLinux = "linux"

// testBinary holds the path to the compiled agent-sandbox binary.
// Set by TestMain for tests that need the real binary.
var testBinary string

// TestMain builds the agent-sandbox binary once for all tests.
func TestMain(m *testing.M) {
	// Build the binary once for all tests
	tmpDir, err := os.MkdirTemp("", "agent-sandbox-test-")
	if err != nil {
		log.Fatalf("failed to create temp dir for test binary: %v", err)
	}

	testBinary = filepath.Join(tmpDir, "agent-sandbox")

	cmd := exec.Command("go", "build", "-o", testBinary, ".")
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		// Clean up temp dir on build failure
		_ = os.RemoveAll(tmpDir)

		log.Fatalf("failed to build test binary: %v", err)
	}

	// Run tests
	code := m.Run()

	// Clean up
	_ = os.RemoveAll(tmpDir)

	os.Exit(code)
}

// GetBinaryPath returns the path to the compiled agent-sandbox binary.
// Skips the test if the binary is not available.
func GetBinaryPath(t *testing.T) string {
	t.Helper()

	if testBinary == "" {
		t.Skip("test binary not built (run via go test, not individual test)")
	}

	return testBinary
}

// RunBinary executes the compiled agent-sandbox binary with the given args.
// Returns stdout, stderr, and exit code.
// Use this for tests that need the real binary (e.g., check inside sandbox).
func RunBinary(t *testing.T, args ...string) (string, string, int) {
	t.Helper()

	binary := GetBinaryPath(t)

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

// RunBinaryWithEnv executes the compiled binary with custom environment.
// Env is a map of key=value pairs that will be set in addition to the
// minimal required environment (PATH).
func RunBinaryWithEnv(t *testing.T, env map[string]string, args ...string) (string, string, int) {
	t.Helper()

	binary := GetBinaryPath(t)

	var outBuf, errBuf bytes.Buffer

	cmd := exec.Command(binary, args...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	// Build environment from map
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// Ensure PATH is always set
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
// Skip helpers for platform and dependency checks
// ============================================================================

// RequireLinux skips the test if not running on Linux.
func RequireLinux(t *testing.T) {
	t.Helper()

	if runtime.GOOS != osLinux {
		t.Skipf("test requires Linux, running on %s", runtime.GOOS)
	}
}

// RequireBwrap skips the test if bwrap is not installed.
func RequireBwrap(t *testing.T) {
	t.Helper()

	RequireLinux(t)

	_, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("test requires bwrap (bubblewrap), not installed")
	}
}

// RequireGit skips the test if git is not installed.
func RequireGit(t *testing.T) {
	t.Helper()

	_, err := exec.LookPath("git")
	if err != nil {
		t.Skip("test requires git, not installed")
	}
}

// RequireDocker skips the test if docker is not installed or not running.
func RequireDocker(t *testing.T) {
	t.Helper()

	_, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("test requires docker, not installed")
	}

	// Check if docker daemon is running
	cmd := exec.Command("docker", "info")

	err = cmd.Run()
	if err != nil {
		t.Skip("test requires docker daemon to be running")
	}
}

// CLI provides a clean interface for running CLI commands in tests.
// It manages a temp directory and environment variables.
type CLI struct {
	t   *testing.T
	Dir string
	Env map[string]string
}

// NewCLITester creates a new test CLI with a temp directory.
// The environment is pre-seeded with HOME (pointing to Dir) and PATH
// so that sandboxed commands can run without manual env setup.
func NewCLITester(t *testing.T) *CLI {
	t.Helper()

	dir := t.TempDir()

	return &CLI{
		t:   t,
		Dir: dir,
		Env: map[string]string{
			"HOME": dir,
			"PATH": os.Getenv("PATH"),
		},
	}
}

// NewCLITesterAt creates a CLI tester that runs from a specific directory.
// The environment is pre-seeded with HOME (pointing to dir) and PATH.
func NewCLITesterAt(t *testing.T, dir string) *CLI {
	t.Helper()

	return &CLI{
		t:   t,
		Dir: dir,
		Env: map[string]string{
			"HOME": dir,
			"PATH": os.Getenv("PATH"),
		},
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

	RequireGit(t)

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

	RequireGit(t)

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

// gitEnvExcludes lists git environment variables that should be excluded
// when running git commands in tests. These variables are inherited from
// the parent process (e.g., pre-commit hooks) and can interfere with
// git operations in isolated test directories.
var gitEnvExcludes = map[string]bool{
	"GIT_DIR":                            true,
	"GIT_WORK_TREE":                      true,
	"GIT_INDEX_FILE":                     true,
	"GIT_OBJECT_DIRECTORY":               true,
	"GIT_ALTERNATE_OBJECT_DIRECTORIES":   true,
	"GIT_CONFIG":                         true,
	"GIT_CONFIG_GLOBAL":                  true,
	"GIT_COMMON_DIR":                     true,
	"GIT_CEILING_DIRECTORIES":            true,
	"GIT_DISCOVERY_ACROSS_FILESYSTEM":    true,
	"GIT_QUARANTINE_PATH":                true,
	"GIT_PUSH_OPTION_COUNT":              true,
	"GIT_AUTHOR_NAME":                    true,
	"GIT_AUTHOR_EMAIL":                   true,
	"GIT_AUTHOR_DATE":                    true,
	"GIT_COMMITTER_NAME":                 true,
	"GIT_COMMITTER_EMAIL":                true,
	"GIT_COMMITTER_DATE":                 true,
	"GIT_LITERAL_PATHSPECS":              true,
	"GIT_GLOB_PATHSPECS":                 true,
	"GIT_NOGLOB_PATHSPECS":               true,
	"GIT_ICASE_PATHSPECS":                true,
	"GIT_REFLOG_ACTION":                  true,
	"GIT_SEQUENCE_EDITOR":                true,
	"GIT_SSH":                            true,
	"GIT_SSH_COMMAND":                    true,
	"GIT_ASKPASS":                        true,
	"GIT_TERMINAL_PROMPT":                true,
	"GIT_FLUSH":                          true,
	"GIT_TRACE":                          true,
	"GIT_TRACE_PACK_ACCESS":              true,
	"GIT_TRACE_PACKET":                   true,
	"GIT_TRACE_PERFORMANCE":              true,
	"GIT_TRACE_SETUP":                    true,
	"GIT_TRACE_SHALLOW":                  true,
	"GIT_TRACE_CURL":                     true,
	"GIT_TRACE_CURL_NO_DATA":             true,
	"GIT_TRACE2":                         true,
	"GIT_TRACE2_EVENT":                   true,
	"GIT_TRACE2_PERF":                    true,
	"GIT_REDACT_COOKIES":                 true,
	"GIT_CURL_VERBOSE":                   true,
	"GIT_DIFF_OPTS":                      true,
	"GIT_EXTERNAL_DIFF":                  true,
	"GIT_DIFF_PATH_COUNTER":              true,
	"GIT_DIFF_PATH_TOTAL":                true,
	"GIT_MERGE_VERBOSITY":                true,
	"GIT_PAGER":                          true,
	"GIT_PROGRESS_DELAY":                 true,
	"GIT_DEFAULT_HASH":                   true,
	"GIT_ALLOW_PROTOCOL":                 true,
	"GIT_PROTOCOL_FROM_USER":             true,
	"GIT_OPTIONAL_LOCKS":                 true,
	"GIT_CLONE_PROTECTION_ACTIVE":        true,
	"GIT_EXEC_PATH":                      true,
	"GIT_TEMPLATE_DIR":                   true,
	"GIT_NO_REPLACE_OBJECTS":             true,
	"GIT_REPLACE_REF_BASE":               true,
	"GIT_PREFIX":                         true,
	"GIT_SHALLOW_FILE":                   true,
	"GIT_NAMESPACE":                      true,
	"GIT_ATTR_SOURCE":                    true,
	"GIT_INTERNAL_GETTEXT_TEST_FALLBACK": true,
	"GIT_INTERNAL_GETTEXT_SH_SCHEME":     true,
}

// cleanGitEnv returns a copy of os.Environ() with git-related variables removed.
func cleanGitEnv() []string {
	var clean []string

	for _, env := range os.Environ() {
		key := env
		if before, _, ok := strings.Cut(env, "="); ok {
			key = before
		}

		if !gitEnvExcludes[key] {
			clean = append(clean, env)
		}
	}

	return clean
}

// run executes a git command in the repository directory.
// Skips the test if git is not available, fails on other errors.
// Clears git environment variables to prevent interference from pre-commit hooks.
func (r *GitRepo) run(args ...string) {
	r.t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	cmd.Env = cleanGitEnv()

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

// ============================================================================
// Test helper tests
// ============================================================================

func Test_GetBinaryPath_Returns_Compiled_Binary_Path(t *testing.T) {
	t.Parallel()

	path := GetBinaryPath(t)

	if path == "" {
		t.Fatal("GetBinaryPath returned empty string")
	}

	// Verify the file exists
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("binary path %q does not exist: %v", path, err)
	}

	// Verify it's executable (not a directory)
	if info.IsDir() {
		t.Fatalf("binary path %q is a directory", path)
	}
}

func Test_RunBinary_Executes_Binary_And_Returns_Output(t *testing.T) {
	t.Parallel()

	stdout, _, exitCode := RunBinary(t, "--help")

	if exitCode != 0 {
		t.Errorf("expected exit code 0 for --help, got %d", exitCode)
	}

	if !strings.Contains(stdout, "agent-sandbox") {
		t.Errorf("expected stdout to contain 'agent-sandbox', got: %s", stdout)
	}
}

func Test_RunBinary_Returns_NonZero_Exit_Code_On_Error(t *testing.T) {
	t.Parallel()

	_, stderr, exitCode := RunBinary(t, "--unknown-flag")

	if exitCode == 0 {
		t.Error("expected non-zero exit code for unknown flag")
	}

	if !strings.Contains(stderr, "unknown flag") {
		t.Errorf("expected stderr to contain 'unknown flag', got: %s", stderr)
	}
}

func Test_RunBinaryWithEnv_Passes_Custom_Environment(t *testing.T) {
	t.Parallel()

	// Use a custom HOME that doesn't exist - exec should fail
	env := map[string]string{
		"HOME": "/nonexistent/path/for/testing",
	}

	_, stderr, exitCode := RunBinaryWithEnv(t, env, "exec", "echo", "hello")

	// Should fail because HOME doesn't exist
	if exitCode == 0 {
		t.Error("expected non-zero exit code when HOME doesn't exist")
	}

	if !strings.Contains(stderr, "cannot determine home directory") {
		t.Errorf("expected error about home directory, got: %s", stderr)
	}
}

func Test_NewCLITester_Seeds_Default_Env(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Verify HOME is set to the temp dir
	if c.Env["HOME"] != c.Dir {
		t.Errorf("expected HOME=%q, got %q", c.Dir, c.Env["HOME"])
	}

	// Verify PATH is set
	if c.Env["PATH"] == "" {
		t.Error("expected PATH to be set")
	}
}

func Test_NewCLITesterAt_Seeds_Default_Env(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	c := NewCLITesterAt(t, dir)

	// Verify HOME is set to the specified dir
	if c.Env["HOME"] != dir {
		t.Errorf("expected HOME=%q, got %q", dir, c.Env["HOME"])
	}

	// Verify PATH is set
	if c.Env["PATH"] == "" {
		t.Error("expected PATH to be set")
	}
}

func Test_RequireLinux_Does_Not_Skip_On_Linux(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != osLinux {
		t.Skip("test only runs on Linux")
	}

	// Should not skip on Linux
	RequireLinux(t)
	// If we get here, RequireLinux didn't skip
}

func Test_RequireGit_Does_Not_Skip_When_Git_Available(t *testing.T) {
	t.Parallel()

	// First check if git is available
	_, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}

	// Should not skip when git is available
	RequireGit(t)
	// If we get here, RequireGit didn't skip
}

func Test_RequireBwrap_Does_Not_Skip_When_Bwrap_Available(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != osLinux {
		t.Skip("bwrap only available on Linux")
	}

	// First check if bwrap is available
	_, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bwrap not installed")
	}

	// Should not skip when bwrap is available
	RequireBwrap(t)
	// If we get here, RequireBwrap didn't skip
}
