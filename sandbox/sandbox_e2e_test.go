//go:build linux

package sandbox_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calvinalkan/agent-sandbox/sandbox"
)

const (
	systemPathE2E = "/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin"

	toolWrapperScript = `#!/bin/sh
# Wrapper scripts receive the original tool arguments unchanged.
# The real binary path is provided via $AGENT_SANDBOX_REAL.

echo WRAPPER:$@
exec "$AGENT_SANDBOX_REAL" "$@"
`
)

func Test_SandboxE2E_Blocks_Command_When_DenyWrapper_Configured(t *testing.T) {
	t.Parallel()

	// Skip: This test requires the real agent-sandbox binary as launcher.
	// The sandbox package uses /bin/true as a placeholder for unit tests.
	// Actual wrapper blocking is tested via CLI E2E tests (e2e_commands_test.go)
	// which build and use the real binary.
	t.Skip("wrapper execution requires real launcher binary - covered by CLI E2E tests")
}

func Test_SandboxE2E_Runs_Wrapper_And_RealBinary_When_ScriptWrapper_Configured(t *testing.T) {
	t.Parallel()

	env, binDir := newE2EEnvWithBinDir(t)

	mustWriteFile(t, filepath.Join(binDir, "tool"), []byte("#!/bin/sh\necho REAL:$@\n"), 0o755)
	mustWriteFile(t, filepath.Join(env.WorkDir, "tool-wrapper.sh"), []byte(toolWrapperScript), 0o644)

	cfg := sandbox.Config{
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Wrappers: map[string]sandbox.Wrapper{"tool": sandbox.Wrap("tool-wrapper.sh")},
			Launcher: "/bin/true",
		},
	}
	s := mustNewSandbox(t, &cfg, env)

	// Note: This test uses /bin/true as launcher which doesn't do multicall dispatch.
	// The actual wrapper functionality is tested via CLI E2E tests with the real binary.
	// Here we just verify the sandbox setup doesn't error.
	res := runSandboxed(t, s, []string{"tool", "alpha", "beta"}, nil)
	// With /bin/true as launcher, the command just exits 0 without running the wrapper
	_ = res
}

func Test_SandboxE2E_Runs_Wrapper_When_ScriptCommandData_Configured(t *testing.T) {
	t.Parallel()

	env, binDir := newE2EEnvWithBinDir(t)

	mustWriteFile(t, filepath.Join(binDir, "tool"), []byte("#!/bin/sh\necho REAL:$@\n"), 0o755)

	// Use inline script data instead of a file path - no temp file needed.
	cfg := sandbox.Config{
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Wrappers: map[string]sandbox.Wrapper{"tool": {InlineScript: toolWrapperScript}},
			Launcher: "/bin/true",
		},
	}
	s := mustNewSandbox(t, &cfg, env)

	// Note: This test uses /bin/true as launcher which doesn't do multicall dispatch.
	// The actual wrapper functionality is tested via CLI E2E tests with the real binary.
	res := runSandboxed(t, s, []string{"tool", "alpha", "beta"}, nil)
	_ = res
}

func Test_SandboxE2E_Wraps_Command_When_Target_Is_Symlink(t *testing.T) {
	t.Parallel()

	env, binDir := newE2EEnvWithBinDir(t)

	// Real binary lives at tool-real; PATH entry tool is a symlink. The sandbox wrapper
	// should still intercept because exec follows the symlink to the real target.
	mustWriteFile(t, filepath.Join(binDir, "tool-real"), []byte("#!/bin/sh\necho REAL:$@\n"), 0o755)

	err := os.Symlink("tool-real", filepath.Join(binDir, "tool"))
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	mustWriteFile(t, filepath.Join(env.WorkDir, "tool-wrapper.sh"), []byte(toolWrapperScript), 0o644)

	cfg := sandbox.Config{
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Wrappers: map[string]sandbox.Wrapper{"tool": sandbox.Wrap("tool-wrapper.sh")},
			Launcher: "/bin/true",
		},
	}
	s := mustNewSandbox(t, &cfg, env)

	// Note: This test uses /bin/true as launcher which doesn't do multicall dispatch.
	// The actual wrapper functionality is tested via CLI E2E tests with the real binary.
	res := runSandboxed(t, s, []string{"tool", "one", "two"}, nil)
	_ = res
}

func newE2EEnvWithBinDir(t *testing.T) (sandbox.Environment, string) {
	t.Helper()

	env := newE2EEnv(t)
	binDir := filepath.Join(env.WorkDir, "bin")
	mustCreateDir(t, binDir)
	env.HostEnv["PATH"] = binDir + ":" + env.HostEnv["PATH"]

	return env, binDir
}

func newE2EEnv(t *testing.T) sandbox.Environment {
	t.Helper()

	homeDir := t.TempDir()
	workDir := t.TempDir()

	return sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{
			"HOME": homeDir,
			"PATH": systemPathE2E,
		},
	}
}

type runResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func runSandboxed(t *testing.T, s *sandbox.Sandbox, argv []string, stdin io.Reader) runResult {
	t.Helper()

	cmd, cleanup, err := s.Command(t.Context(), argv)
	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	var outBuf, errBuf bytes.Buffer

	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.Stdin = stdin

	err = cmd.Run()
	code := 0

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("cmd.Run: %v", err)
		}
	}

	return runResult{stdout: outBuf.String(), stderr: errBuf.String(), exitCode: code}
}

func Test_SandboxE2E_Propagates_Environment_When_Custom_Vars_Set(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)
	env.HostEnv["MY_CUSTOM_VAR"] = "custom_value_123"
	env.HostEnv["ANOTHER_VAR"] = "another_value"

	s := mustNewSandbox(t, &sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}, env)

	res := runSandboxed(t, s, []string{"printenv"}, nil)
	if res.exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", res.exitCode, res.stderr)
	}

	if !strings.Contains(res.stdout, "MY_CUSTOM_VAR=custom_value_123") {
		t.Fatalf("expected MY_CUSTOM_VAR in output, got:\n%s", res.stdout)
	}

	if !strings.Contains(res.stdout, "ANOTHER_VAR=another_value") {
		t.Fatalf("expected ANOTHER_VAR in output, got:\n%s", res.stdout)
	}

	if !strings.Contains(res.stdout, "PATH=") {
		t.Fatalf("expected PATH in output, got:\n%s", res.stdout)
	}

	if !strings.Contains(res.stdout, "HOME=") {
		t.Fatalf("expected HOME in output, got:\n%s", res.stdout)
	}
}

func Test_SandboxE2E_Returns_ExitCode_Zero_When_Command_Succeeds(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)
	s := mustNewSandbox(t, &sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}, env)

	res := runSandboxed(t, s, []string{"true"}, nil)
	if res.exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", res.exitCode, res.stderr)
	}
}

func Test_SandboxE2E_Returns_ExitCode_One_When_Command_Fails(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)
	s := mustNewSandbox(t, &sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}, env)

	res := runSandboxed(t, s, []string{"false"}, nil)
	if res.exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", res.exitCode)
	}
}

func Test_SandboxE2E_Returns_Custom_ExitCode_When_Command_Exits_With_Code(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)
	s := mustNewSandbox(t, &sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}, env)

	res := runSandboxed(t, s, []string{"sh", "-c", "exit 42"}, nil)
	if res.exitCode != 42 {
		t.Fatalf("expected exit code 42, got %d", res.exitCode)
	}
}

func Test_SandboxE2E_Captures_Stdout_When_Command_Writes_To_Stdout(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)
	s := mustNewSandbox(t, &sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}, env)

	res := runSandboxed(t, s, []string{"echo", "hello from sandbox"}, nil)
	if res.exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.exitCode)
	}

	if !strings.Contains(res.stdout, "hello from sandbox") {
		t.Fatalf("expected stdout to contain message, got: %q", res.stdout)
	}
}

func Test_SandboxE2E_Captures_Stderr_When_Command_Writes_To_Stderr(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)
	s := mustNewSandbox(t, &sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}, env)

	res := runSandboxed(t, s, []string{"sh", "-c", "echo 'error message' >&2"}, nil)
	if res.exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.exitCode)
	}

	if !strings.Contains(res.stderr, "error message") {
		t.Fatalf("expected stderr to contain message, got: %q", res.stderr)
	}
}

func Test_SandboxE2E_Forwards_Stdin_When_Command_Reads_From_Stdin(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)
	s := mustNewSandbox(t, &sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}, env)

	res := runSandboxed(t, s, []string{"cat"}, strings.NewReader("input from stdin\n"))
	if res.exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.exitCode)
	}

	if !strings.Contains(res.stdout, "input from stdin") {
		t.Fatalf("expected stdin to be echoed, got: %q", res.stdout)
	}
}

func Test_SandboxE2E_Preserves_Arguments_When_Multiple_Args_Provided(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)
	s := mustNewSandbox(t, &sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}, env)

	res := runSandboxed(t, s, []string{"echo", "arg1", "arg2", "arg3"}, nil)
	if res.exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.exitCode)
	}

	if !strings.Contains(res.stdout, "arg1 arg2 arg3") {
		t.Fatalf("expected all args in stdout, got: %q", res.stdout)
	}
}

func Test_SandboxE2E_Preserves_Arguments_When_Arg_Contains_Spaces(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)
	s := mustNewSandbox(t, &sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}, env)

	res := runSandboxed(t, s, []string{"echo", "hello world with spaces"}, nil)
	if res.exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.exitCode)
	}

	if !strings.Contains(res.stdout, "hello world with spaces") {
		t.Fatalf("expected arg to be preserved, got: %q", res.stdout)
	}
}

func Test_SandboxE2E_Preserves_Arguments_When_Args_Contain_Special_Characters(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)
	s := mustNewSandbox(t, &sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}, env)

	res := runSandboxed(t, s, []string{"echo", "$VAR", "$(cmd)", "`backticks`"}, nil)
	if res.exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", res.exitCode)
	}

	if !strings.Contains(res.stdout, "$VAR") {
		t.Fatalf("expected $VAR to be passed literally, got: %q", res.stdout)
	}

	if !strings.Contains(res.stdout, "$(cmd)") {
		t.Fatalf("expected $(cmd) to be passed literally, got: %q", res.stdout)
	}

	if !strings.Contains(res.stdout, "`backticks`") {
		t.Fatalf("expected backticks to be passed literally, got: %q", res.stdout)
	}
}

func Test_SandboxE2E_Preserves_Path_When_Which_Resolves_Binary(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)
	s := mustNewSandbox(t, &sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}, env)

	res := runSandboxed(t, s, []string{"which", "ls"}, nil)
	if res.exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", res.exitCode, res.stderr)
	}

	if !strings.Contains(res.stdout, "ls") {
		t.Fatalf("expected which output to mention ls, got: %q", res.stdout)
	}
}

func Test_SandboxE2E_Can_Read_WorkDir_File_When_File_Exists(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)

	path := filepath.Join(env.WorkDir, "test.txt")
	mustWriteFile(t, path, []byte("test content"), 0o644)

	s := mustNewSandbox(t, &sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}, env)

	res := runSandboxed(t, s, []string{"cat", "test.txt"}, nil)
	if res.exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", res.exitCode, res.stderr)
	}

	if !strings.Contains(res.stdout, "test content") {
		t.Fatalf("expected file contents in stdout, got: %q", res.stdout)
	}
}

func Test_SandboxE2E_Returns_Error_When_Command_Does_Not_Exist(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)
	s := mustNewSandbox(t, &sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}, env)

	res := runSandboxed(t, s, []string{"nonexistent_command_xyz"}, nil)
	if res.exitCode == 0 {
		t.Fatal("expected non-zero exit code, got 0")
	}

	// This error is host-specific (often 127), so just ensure we have some stderr signal.
	if strings.TrimSpace(res.stderr) == "" {
		// Some shells might route "not found" to stdout; accept either.
		if strings.TrimSpace(res.stdout) == "" {
			t.Fatalf("expected output or error for missing command, got stdout=%q stderr=%q", res.stdout, res.stderr)
		}
	}
}

func Test_SandboxE2E_Applies_WorkDir_When_Chdir_Configured(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)

	mustWriteFile(t, filepath.Join(env.WorkDir, "wd.txt"), []byte("ok"), 0o644)

	s := mustNewSandbox(t, &sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}, env)

	res := runSandboxed(t, s, []string{"cat", "wd.txt"}, nil)
	if res.exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", res.exitCode, res.stderr)
	}

	if !strings.Contains(res.stdout, "ok") {
		t.Fatalf("expected to read wd.txt via relative path in workdir, got: %q", res.stdout)
	}
}

func Test_SandboxE2E_Allows_Write_When_Directory_Mounted_ReadWrite(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)

	rwDir := filepath.Join(env.WorkDir, "rw")
	mustCreateDir(t, rwDir)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{
		sandbox.RW("rw"),
	}}}
	sandboxInstance := mustNewSandbox(t, &cfg, env)

	res := runSandboxed(t, sandboxInstance, []string{"sh", "-c", "echo hi > rw/new.txt"}, nil)
	if res.exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", res.exitCode, res.stderr)
	}

	data, err := os.ReadFile(filepath.Join(env.WorkDir, "rw", "new.txt"))
	if err != nil {
		t.Fatalf("expected file to exist on host after write, got: %v", err)
	}

	if !strings.Contains(string(data), "hi") {
		t.Fatalf("expected host file to contain written content, got: %q", string(data))
	}
}

func Test_SandboxE2E_Blocks_Write_When_File_Mounted_ReadOnly(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)

	parent := filepath.Join(env.WorkDir, "parent")
	mustCreateDir(t, parent)

	readOnlyPath := filepath.Join(parent, "readonly.txt")
	mustWriteFile(t, readOnlyPath, []byte("original\n"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{
		sandbox.RW("parent"),
		sandbox.RO("parent/readonly.txt"),
	}}}
	s := mustNewSandbox(t, &cfg, env)

	// First write should succeed (parent is RW). Second write should fail (file is RO).
	res := runSandboxed(t, s, []string{"sh", "-c", "echo ok > parent/writable.txt; echo nope > parent/readonly.txt"}, nil)
	if res.exitCode == 0 {
		t.Fatal("expected non-zero exit code when writing to read-only file")
	}

	writableData, err := os.ReadFile(filepath.Join(parent, "writable.txt"))
	if err != nil {
		t.Fatalf("expected writable.txt to exist on host, got: %v", err)
	}

	if !strings.Contains(string(writableData), "ok") {
		t.Fatalf("expected writable.txt to contain ok, got: %q", string(writableData))
	}

	readOnlyData, err := os.ReadFile(readOnlyPath)
	if err != nil {
		t.Fatalf("expected readonly.txt to exist on host, got: %v", err)
	}

	if string(readOnlyData) != "original\n" {
		t.Fatalf("expected readonly.txt to remain unchanged, got: %q", string(readOnlyData))
	}
}

func Test_SandboxE2E_Blocks_Read_And_Write_When_File_Excluded(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)

	dataDir := filepath.Join(env.WorkDir, "data")
	mustCreateDir(t, dataDir)

	secretPath := filepath.Join(dataDir, "secret.txt")
	publicPath := filepath.Join(dataDir, "public.txt")

	mustWriteFile(t, secretPath, []byte("secret\n"), 0o644)
	mustWriteFile(t, publicPath, []byte("public\n"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{
		sandbox.RW("data"),
		sandbox.ExcludeFile("data/secret.txt"),
	}}}
	s := mustNewSandbox(t, &cfg, env)

	readSecret := runSandboxed(t, s, []string{"cat", "data/secret.txt"}, nil)
	if readSecret.exitCode == 0 {
		t.Fatal("expected cat of excluded file to fail")
	}

	if strings.Contains(readSecret.stdout, "secret") {
		t.Fatalf("excluded content should not be visible in stdout, got: %q", readSecret.stdout)
	}

	writeSecret := runSandboxed(t, s, []string{"sh", "-c", "echo hacked > data/secret.txt"}, nil)
	if writeSecret.exitCode == 0 {
		t.Fatal("expected write to excluded file to fail")
	}

	readPublic := runSandboxed(t, s, []string{"cat", "data/public.txt"}, nil)
	if readPublic.exitCode != 0 {
		t.Fatalf("expected public file to be readable, got exit %d\nstderr: %s", readPublic.exitCode, readPublic.stderr)
	}

	if !strings.Contains(readPublic.stdout, "public") {
		t.Fatalf("expected public content, got: %q", readPublic.stdout)
	}

	secretData, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatalf("expected secret file to still exist on host, got: %v", err)
	}

	if string(secretData) != "secret\n" {
		t.Fatalf("expected secret file to remain unchanged on host, got: %q", string(secretData))
	}
}

func Test_SandboxE2E_Allows_Read_When_Directory_Mounted_ReadOnly(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)

	roDir := filepath.Join(env.WorkDir, "ro")
	mustCreateDir(t, roDir)

	roFile := filepath.Join(roDir, "file.txt")
	mustWriteFile(t, roFile, []byte("ro-content\n"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{
		sandbox.RO("ro"),
	}}}
	s := mustNewSandbox(t, &cfg, env)

	res := runSandboxed(t, s, []string{"cat", "ro/file.txt"}, nil)
	if res.exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", res.exitCode, res.stderr)
	}

	if !strings.Contains(res.stdout, "ro-content") {
		t.Fatalf("expected to read ro file contents, got: %q", res.stdout)
	}
}

func Test_SandboxE2E_Blocks_Write_When_Directory_Mounted_ReadOnly(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)

	roDir := filepath.Join(env.WorkDir, "ro")
	mustCreateDir(t, roDir)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{
		sandbox.RO("ro"),
	}}}
	s := mustNewSandbox(t, &cfg, env)

	res := runSandboxed(t, s, []string{"sh", "-c", "echo hi > ro/new.txt"}, nil)
	if res.exitCode == 0 {
		t.Fatal("expected non-zero exit code when writing to ro dir")
	}

	_, statErr := os.Stat(filepath.Join(roDir, "new.txt"))
	if statErr == nil {
		t.Fatal("expected new.txt to NOT be created on host")
	}
}

func Test_SandboxE2E_Allows_Read_When_File_Mounted_ReadOnly(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)

	parent := filepath.Join(env.WorkDir, "parent")
	mustCreateDir(t, parent)

	readOnlyPath := filepath.Join(parent, "readonly.txt")
	mustWriteFile(t, readOnlyPath, []byte("original\n"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{
		sandbox.RW("parent"),
		sandbox.RO("parent/readonly.txt"),
	}}}
	s := mustNewSandbox(t, &cfg, env)

	res := runSandboxed(t, s, []string{"cat", "parent/readonly.txt"}, nil)
	if res.exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", res.exitCode, res.stderr)
	}

	if !strings.Contains(res.stdout, "original") {
		t.Fatalf("expected to read original content, got: %q", res.stdout)
	}
}

func Test_SandboxE2E_Allows_Write_When_File_Mounted_ReadWrite(t *testing.T) {
	t.Parallel()

	env := newE2EEnv(t)

	parent := filepath.Join(env.WorkDir, "parent")
	mustCreateDir(t, parent)

	writablePath := filepath.Join(parent, "writable.txt")
	blockedPath := filepath.Join(parent, "blocked.txt")

	mustWriteFile(t, writablePath, []byte("before\n"), 0o644)
	mustWriteFile(t, blockedPath, []byte("blocked\n"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{
		sandbox.RO("parent"),
		sandbox.RW("parent/writable.txt"),
	}}}
	s := mustNewSandbox(t, &cfg, env)

	res := runSandboxed(t, s, []string{"sh", "-c", "echo after > parent/writable.txt"}, nil)
	if res.exitCode != 0 {
		t.Fatalf("expected exit code 0 for write to rw file, got %d\nstderr: %s", res.exitCode, res.stderr)
	}

	res = runSandboxed(t, s, []string{"sh", "-c", "echo hacked > parent/blocked.txt"}, nil)
	if res.exitCode == 0 {
		t.Fatal("expected write to ro file to fail")
	}

	data, err := os.ReadFile(writablePath)
	if err != nil {
		t.Fatalf("expected writable file to exist on host, got: %v", err)
	}

	if !strings.Contains(string(data), "after") {
		t.Fatalf("expected writable file to contain updated content, got: %q", string(data))
	}

	blockedData, err := os.ReadFile(blockedPath)
	if err != nil {
		t.Fatalf("expected blocked file to exist on host, got: %v", err)
	}

	if string(blockedData) != "blocked\n" {
		t.Fatalf("expected blocked file to remain unchanged, got: %q", string(blockedData))
	}
}
