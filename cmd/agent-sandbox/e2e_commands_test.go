package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runBinaryAtPathWithEnv(t *testing.T, binary string, env map[string]string, args ...string) (string, string, int) {
	t.Helper()

	var outBuf, errBuf bytes.Buffer

	cmd := exec.Command(binary, args...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if env != nil {
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}

		if _, ok := env["PATH"]; !ok {
			cmd.Env = append(cmd.Env, "PATH="+os.Getenv("PATH"))
		}
	}

	err := cmd.Run()
	code := 0

	exitErr := &exec.ExitError{}
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("failed to run binary %q: %v", binary, err)
	}

	return outBuf.String(), errBuf.String(), code
}

// ============================================================================
// E2E tests for command wrappers (--cmd flag and presets)
//
// These tests verify command wrappers work correctly inside the sandbox.
// - Block rules: command fails with "blocked" message
// - Raw rules: command runs unmodified
// - Preset rules: some subcommands blocked, others allowed
// ============================================================================

// ============================================================================
// Block and Raw rules
// ============================================================================

func Test_Command_Block_Rule_Blocks_Command(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	c := NewCLITester(t)
	// Must use RunBinary for actual wrapper execution (ELF launcher needs real binary)
	_, stderr, code := RunBinaryWithEnv(t, c.Env, "-C", c.Dir, "--cmd", "cat=false", "cat", "/etc/hostname")

	if code == 0 {
		t.Error("blocked command should fail")
	}

	if !strings.Contains(stderr, "blocked") {
		t.Errorf("should mention blocked, got: %s", stderr)
	}
}

func Test_Command_Launcher_Uses_ELF_For_Target_Paths(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	c := NewCLITester(t)

	stdout, stderr, code := RunBinaryWithEnv(t, c.Env, "-C", c.Dir, "--cmd", "cat=false", "bash", "-c", `p=$(command -v cat)
od -An -t x1 -N 4 "$p" | tr -d $' \n'`)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	if strings.TrimSpace(stdout) != "7f454c46" {
		t.Fatalf("expected ELF header hex 7f454c46, got %q", stdout)
	}
}

func Test_Command_Launcher_Uses_ELF_When_Binary_Renamed(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	c := NewCLITester(t)

	src := GetTestBinaryPath(t)
	dir := t.TempDir()
	dst := filepath.Join(dir, "agent-sandbox-renamed")

	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read test binary: %v", err)
	}

	tmp, err := os.CreateTemp(dir, "agent-sandbox-renamed-*")
	if err != nil {
		t.Fatalf("create temp binary: %v", err)
	}

	_, err = tmp.Write(data)
	if err != nil {
		_ = tmp.Close()

		t.Fatalf("write temp binary: %v", err)
	}

	err = tmp.Sync()
	if err != nil {
		_ = tmp.Close()

		t.Fatalf("sync temp binary: %v", err)
	}

	err = tmp.Close()
	if err != nil {
		t.Fatalf("close temp binary: %v", err)
	}

	err = os.Chmod(tmp.Name(), 0o755)
	if err != nil {
		t.Fatalf("chmod temp binary: %v", err)
	}

	err = os.Rename(tmp.Name(), dst)
	if err != nil {
		t.Fatalf("rename temp binary: %v", err)
	}

	stdout, stderr, code := runBinaryAtPathWithEnv(t, dst, c.Env, "-C", c.Dir, "--cmd", "cat=false", "bash", "-c", `p=$(command -v cat)
od -An -t x1 -N 4 "$p" | tr -d $' \n'`)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	if strings.TrimSpace(stdout) != "7f454c46" {
		t.Fatalf("expected ELF header hex 7f454c46, got %q", stdout)
	}
}

func Test_Renamed_Binary_Works_As_CLI_Inside_Sandbox(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	c := NewCLITester(t)

	src := GetTestBinaryPath(t)
	binDir := t.TempDir()
	ags := filepath.Join(binDir, "ags")

	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read test binary: %v", err)
	}

	tmp, err := os.CreateTemp(binDir, "ags-*")
	if err != nil {
		t.Fatalf("create temp binary: %v", err)
	}

	_, err = tmp.Write(data)
	if err != nil {
		_ = tmp.Close()

		t.Fatalf("write temp binary: %v", err)
	}

	err = tmp.Sync()
	if err != nil {
		_ = tmp.Close()

		t.Fatalf("sync temp binary: %v", err)
	}

	err = tmp.Close()
	if err != nil {
		t.Fatalf("close temp binary: %v", err)
	}

	err = os.Chmod(tmp.Name(), 0o755)
	if err != nil {
		t.Fatalf("chmod temp binary: %v", err)
	}

	err = os.Rename(tmp.Name(), ags)
	if err != nil {
		t.Fatalf("rename temp binary: %v", err)
	}

	// Run sandbox via renamed binary, verify:
	// - wrapped commands still dispatch (cat is blocked)
	// - renamed binary behaves like the normal CLI inside the sandbox (--check succeeds)
	stdout, stderr, code := runBinaryAtPathWithEnv(t, ags, c.Env,
		"-C", c.Dir,
		"--cmd", "cat=false",
		"bash", "-c", fmt.Sprintf(`set -eu
cat /etc/hostname >/dev/null 2>&1 && exit 1 || true
%q --check >/dev/null
echo OK`, ags),
	)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "OK") {
		t.Fatalf("expected OK in stdout, got: %q", stdout)
	}

	if strings.Contains(stderr, "no policy or preset configured") {
		t.Fatalf("unexpected multicall error in stderr: %s", stderr)
	}
}

func Test_Command_Runtime_Dirs_Not_Listable(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	c := NewCLITester(t)
	c.WriteExecutable("wrapper.sh", `#!/bin/sh
exec "$AGENT_SANDBOX_REAL" "$@"
`)

	wrapperPath := filepath.Join(c.Dir, "wrapper.sh")

	script := `set -eu
lsbin=$(command -v ls)
[ -x "$lsbin" ]

dirs="/run/agent-sandbox /run/agent-sandbox/bin /run/agent-sandbox/policies /run/agent-sandbox/presets"
for d in $dirs; do
  if [ ! -d "$d" ]; then
    printf 'missing dir: %s\n' "$d" >&2
    exit 1
  fi

  if [ ! -x "$d" ]; then
    printf 'expected dir to be searchable: %s\n' "$d" >&2
    exit 1
  fi

  if [ -r "$d" ]; then
    printf 'expected dir to be non-readable: %s\n' "$d" >&2
    exit 1
  fi

  if "$lsbin" "$d" >/dev/null 2>&1; then
    printf 'expected ls to fail for: %s\n' "$d" >&2
    exit 1
  fi
done
`

	_, stderr, code := RunBinaryWithEnv(t, c.Env,
		"-C", c.Dir,
		"--cmd", "git=@git",
		"--cmd", "echo="+wrapperPath,
		"bash", "-c", script,
	)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}
}

func Test_Command_Raw_Rule_Allows_Command(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	c := NewCLITester(t)
	stdout, stderr, code := c.Run("--cmd", "echo=true", "echo", "hello world")

	if code != 0 {
		t.Errorf("should work with echo=true, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "hello world") {
		t.Errorf("expected output, got: %s", stdout)
	}
}

// ============================================================================
// @git preset - command filtering
// ============================================================================

func setupGitRepoForCommandTests(t *testing.T) (map[string]string, string) {
	t.Helper()

	homeDir := t.TempDir()
	workDir := t.TempDir()
	tmpDir := t.TempDir()

	repo := NewGitRepoAt(t, workDir)
	repo.WriteFile("README.md", "initial content")
	repo.Commit("initial commit")

	// IMPORTANT: TMPDIR must be set to a DIFFERENT temp dir than workDir!
	//
	// Background: The @git preset allows blocked operations (checkout, reset --hard, etc.)
	// when the working directory is inside the system temp dir. This is so external projects
	// using the sandbox can run their test suites - those tests often create git repos in
	// t.TempDir() and need destructive git operations.
	//
	// The check works by comparing os.Getwd() against os.TempDir(). Crucially, os.TempDir()
	// reads the TMPDIR environment variable.
	//
	// Here's the trick that makes blocker tests work:
	//   - workDir = /tmp/TestXXX/001  (where git commands run)
	//   - tmpDir  = /tmp/TestXXX/002  (what TMPDIR points to)
	//   - os.TempDir() returns /tmp/TestXXX/002
	//   - os.Getwd() returns /tmp/TestXXX/001
	//   - HasPrefix("/tmp/TestXXX/001", "/tmp/TestXXX/002") = FALSE
	//   - Therefore: isInTempDir() returns false, blocking is enforced!
	//
	// If TMPDIR was unset or set to /tmp:
	//   - os.TempDir() would return /tmp
	//   - HasPrefix("/tmp/TestXXX/001", "/tmp") = TRUE
	//   - isInTempDir() returns true, blocking disabled, tests FAIL!
	//
	// DO NOT remove TMPDIR or set it to /tmp!
	env := map[string]string{
		"HOME":   homeDir,
		"PATH":   "/usr/local/bin:/usr/bin:/bin",
		"TMPDIR": tmpDir,
	}

	return env, workDir
}

func Test_Git_Preset_Blocks_Checkout(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	env, workDir := setupGitRepoForCommandTests(t)

	_, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "git", "checkout", ".")

	if code == 0 {
		t.Error("checkout should be blocked")
	}

	if !strings.Contains(stderr, "blocked") {
		t.Errorf("should mention blocked, got: %s", stderr)
	}
}

func Test_Git_Preset_Blocks_Reset_Hard(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	env, workDir := setupGitRepoForCommandTests(t)

	_, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "git", "reset", "--hard")

	if code == 0 {
		t.Error("reset --hard should be blocked")
	}

	if !strings.Contains(stderr, "blocked") {
		t.Errorf("should mention blocked, got: %s", stderr)
	}
}

func Test_Git_Preset_Allows_Status(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	env, workDir := setupGitRepoForCommandTests(t)

	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "git", "status")

	if code != 0 {
		t.Errorf("status should work, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "branch") && !strings.Contains(stdout, "clean") {
		t.Errorf("unexpected output: %s", stdout)
	}
}

func Test_Git_Preset_Allows_Log(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	env, workDir := setupGitRepoForCommandTests(t)

	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "git", "log", "--oneline")

	if code != 0 {
		t.Errorf("log should work, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "initial") {
		t.Errorf("should show commit, got: %s", stdout)
	}
}

func Test_Git_Preset_Allows_Commit(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	env, workDir := setupGitRepoForCommandTests(t)

	_ = os.WriteFile(filepath.Join(workDir, "new.txt"), []byte("new content"), 0o644)

	_, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "git", "add", "new.txt")
	if code != 0 {
		t.Errorf("add should work, got %d\nstderr: %s", code, stderr)
	}

	_, stderr, code = RunBinaryWithEnv(t, env, "-C", workDir, "git", "commit", "-m", "add new file")
	if code != 0 {
		t.Errorf("commit should work, got %d\nstderr: %s", code, stderr)
	}
}

// ============================================================================
// Custom script wrappers (CommandRuleScript)
//
// These tests verify custom wrapper scripts work correctly.
//
// Wrapper contract:
// - Original args are passed through unchanged as "$@"
// - AGENT_SANDBOX_REAL contains the real binary path (e.g. /run/agent-sandbox/bin/echo)
// - AGENT_SANDBOX_CMD contains the wrapped command name (e.g. echo)
// ============================================================================

func Test_Custom_Script_Receives_Real_Binary_Path(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	c := NewCLITester(t)
	c.WriteExecutable("wrapper.sh", `#!/bin/sh
echo "CMD=$AGENT_SANDBOX_CMD"
echo "REAL_BINARY=$AGENT_SANDBOX_REAL"
`)

	stdout, stderr, code := RunBinaryWithEnv(t, c.Env, "-C", c.Dir, "--cmd", "echo="+filepath.Join(c.Dir, "wrapper.sh"), "echo", "ignored")

	if code != 0 {
		t.Errorf("expected exit 0, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "REAL_BINARY=") {
		t.Errorf("expected REAL_BINARY in output, got: %s", stdout)
	}

	if !strings.Contains(stdout, "/run/agent-sandbox/bin/echo") {
		t.Errorf("expected real binary path to contain /run/agent-sandbox/bin/echo, got: %s", stdout)
	}
}

func Test_Custom_Script_Receives_Command_Arguments(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	c := NewCLITester(t)
	c.WriteExecutable("wrapper.sh", `#!/bin/sh
echo "ARGS: $@"
`)

	stdout, stderr, code := RunBinaryWithEnv(t, c.Env, "-C", c.Dir, "--cmd", "echo="+filepath.Join(c.Dir, "wrapper.sh"), "echo", "arg1", "arg2", "arg with spaces")

	if code != 0 {
		t.Errorf("expected exit 0, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "arg1") {
		t.Errorf("expected arg1 in output, got: %s", stdout)
	}

	if !strings.Contains(stdout, "arg2") {
		t.Errorf("expected arg2 in output, got: %s", stdout)
	}

	if !strings.Contains(stdout, "arg with spaces") {
		t.Errorf("expected 'arg with spaces' in output, got: %s", stdout)
	}
}

func Test_Custom_Script_Can_Call_Real_Binary(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	c := NewCLITester(t)
	c.WriteExecutable("wrapper.sh", `#!/bin/sh
exec "$AGENT_SANDBOX_REAL" wrapped: "$@"
`)

	stdout, stderr, code := RunBinaryWithEnv(t, c.Env, "-C", c.Dir, "--cmd", "echo="+filepath.Join(c.Dir, "wrapper.sh"), "echo", "hello", "world")

	if code != 0 {
		t.Errorf("expected exit 0, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "wrapped: hello world") {
		t.Errorf("expected 'wrapped: hello world', got: %s", stdout)
	}
}

func Test_Custom_Script_Exit_Code_Is_Preserved(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	c := NewCLITester(t)
	c.WriteExecutable("wrapper.sh", `#!/bin/bash
exit 42
`)

	_, _, code := RunBinaryWithEnv(t, c.Env, "-C", c.Dir, "--cmd", "echo="+filepath.Join(c.Dir, "wrapper.sh"), "echo", "ignored")

	if code != 42 {
		t.Errorf("expected exit 42, got %d", code)
	}
}

func Test_Custom_Script_Can_Block_Command(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	c := NewCLITester(t)
	c.WriteExecutable("wrapper.sh", `#!/bin/sh
echo "custom block: $1 not allowed" >&2
exit 1
`)

	_, stderr, code := RunBinaryWithEnv(t, c.Env, "-C", c.Dir, "--cmd", "rm="+filepath.Join(c.Dir, "wrapper.sh"), "rm", "-rf", "/")

	if code == 0 {
		t.Error("expected non-zero exit code")
	}

	if !strings.Contains(stderr, "custom block") {
		t.Errorf("expected custom block message, got: %s", stderr)
	}

	if !strings.Contains(stderr, "-rf") {
		t.Errorf("expected args in message, got: %s", stderr)
	}
}

func Test_Custom_Script_Overrides_Preset(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	env, workDir := setupGitRepoForCommandTests(t)

	// Create a custom git wrapper that allows all git operations
	scriptPath := filepath.Join(workDir, "git-wrapper.sh")
	mustWriteFile(t, scriptPath, `#!/bin/sh
exec "$AGENT_SANDBOX_REAL" "$@"
`)

	err := os.Chmod(scriptPath, 0o755)
	if err != nil {
		t.Fatalf("failed to chmod: %v", err)
	}

	stdout, stderr, code := RunBinaryWithEnv(t, env, "-C", workDir, "--cmd", "git="+scriptPath, "git", "status")

	if code != 0 {
		t.Errorf("git status should work, got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "branch") && !strings.Contains(stdout, "clean") {
		t.Errorf("unexpected output: %s", stdout)
	}
}

func Test_Custom_Script_With_Filtering_Logic(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	c := NewCLITester(t)
	c.WriteExecutable("wrapper.sh", `#!/bin/sh
if [ "$1" = "safe" ]; then
    exec "$AGENT_SANDBOX_REAL" "allowed:" "$@"
else
    echo "error: only 'safe' argument allowed, got '$1'" >&2
    exit 1
fi
`)
	scriptPath := filepath.Join(c.Dir, "wrapper.sh")

	// Test allowed case
	stdout, stderr, code := RunBinaryWithEnv(t, c.Env, "-C", c.Dir, "--cmd", "echo="+scriptPath, "echo", "safe", "extra")
	if code != 0 {
		t.Errorf("expected exit 0 for 'safe', got %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "allowed: safe extra") {
		t.Errorf("expected 'allowed: safe extra', got: %s", stdout)
	}

	// Test blocked case
	_, stderr, code = RunBinaryWithEnv(t, c.Env, "-C", c.Dir, "--cmd", "echo="+scriptPath, "echo", "dangerous")
	if code == 0 {
		t.Error("expected non-zero exit for 'dangerous'")
	}

	if !strings.Contains(stderr, "only 'safe' argument allowed") {
		t.Errorf("expected custom error message, got: %s", stderr)
	}
}
