package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func Test_Nested_Sandbox_Inherits_Git_Preset(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	env, workDir := setupGitRepoForCommandTests(t)

	_, stderr, code := RunBinaryWithEnv(t, env,
		"-C", workDir,
		"agent-sandbox", "git", "checkout", ".",
	)

	skipIfNestedUnsupported(t, stderr, code)

	if code == 0 {
		t.Fatal("expected nested git checkout to be blocked, got exit code 0")
	}

	if !strings.Contains(stderr, "blocked") {
		t.Fatalf("expected nested git checkout to be blocked (stderr contains 'blocked'), got:\n%s", stderr)
	}
}

func Test_Nested_Sandbox_Cannot_Relax_Command_Wrappers_With_CmdFlag(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	c := NewCLITester(t)
	c.Env["HOME"] = t.TempDir()

	nestedDir := filepath.Join(c.Dir, "nested")
	mustMkdir(t, nestedDir)

	victim := filepath.Join(nestedDir, "victim.txt")
	mustWriteFile(t, victim, "do not delete")

	// Verify outer sandbox blocks rm.
	_, stderr, code := RunBinaryWithEnv(t, c.Env,
		"-C", c.Dir,
		"--cmd", "rm=false",
		"rm", victim,
	)
	if code == 0 {
		t.Fatal("expected rm to be blocked in outer sandbox, got exit code 0")
	}

	if !strings.Contains(strings.ToLower(stderr), "blocked") {
		t.Fatalf("expected outer rm to be blocked (stderr contains 'blocked'), got:\n%s", stderr)
	}

	_, statErr := os.Stat(victim)
	if statErr != nil {
		t.Fatalf("expected victim file to still exist after outer rm attempt, stat: %v", statErr)
	}

	// Inner sandbox attempts to relax the outer sandbox by allowing rm.
	_, stderr, code = RunBinaryWithEnv(t, c.Env,
		"-C", c.Dir,
		"--cmd", "rm=false",
		"agent-sandbox", "-C", nestedDir, "--cmd", "rm=true", "rm", "victim.txt",
	)

	skipIfNestedUnsupported(t, stderr, code)

	if code == 0 {
		t.Fatal("expected nested rm to remain blocked by outer sandbox wrappers, got exit code 0")
	}

	if !strings.Contains(strings.ToLower(stderr), "blocked") {
		t.Fatalf("expected nested rm to be blocked (stderr contains 'blocked'), got:\n%s", stderr)
	}

	_, statErr = os.Stat(victim)
	if statErr != nil {
		t.Fatalf("expected victim file to still exist on host, stat: %v", statErr)
	}
}

func Test_Nested_Sandbox_Inner_Can_Block_Rm_With_CmdFlag_When_Outer_Allows(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	c := NewCLITester(t)
	c.Env["HOME"] = t.TempDir()

	outerVictim := filepath.Join(c.Dir, "outer-victim.txt")
	mustWriteFile(t, outerVictim, "delete me")

	_, stderr, code := RunBinaryWithEnv(t, c.Env,
		"-C", c.Dir,
		"rm", outerVictim,
	)
	if code != 0 {
		t.Fatalf("expected rm to work in outer sandbox, got exit code %d\nstderr: %s", code, stderr)
	}

	_, statErr := os.Stat(outerVictim)
	if !os.IsNotExist(statErr) {
		t.Fatalf("expected outer victim file to be deleted, stat: %v", statErr)
	}

	nestedDir := filepath.Join(c.Dir, "nested")
	mustMkdir(t, nestedDir)

	innerVictim := filepath.Join(nestedDir, "inner-victim.txt")
	mustWriteFile(t, innerVictim, "do not delete")

	_, stderr, code = RunBinaryWithEnv(t, c.Env,
		"-C", c.Dir,
		"agent-sandbox", "-C", nestedDir, "--cmd", "rm=false", "rm", "inner-victim.txt",
	)

	skipIfNestedUnsupported(t, stderr, code)

	if code == 0 {
		t.Fatal("expected rm to be blocked in inner sandbox, got exit code 0")
	}

	if !strings.Contains(strings.ToLower(stderr), "blocked") {
		t.Fatalf("expected rm to be blocked in inner sandbox (stderr contains 'blocked'), got:\n%s", stderr)
	}

	_, statErr = os.Stat(innerVictim)
	if statErr != nil {
		t.Fatalf("expected inner victim file to still exist on host, stat: %v", statErr)
	}
}

func Test_Nested_Sandbox_Inner_Can_Run_Custom_Script_Wrapper_With_CmdFlag(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	c := NewCLITester(t)
	c.Env["HOME"] = t.TempDir()

	nestedDir := filepath.Join(c.Dir, "nested")
	mustMkdir(t, nestedDir)

	mustWriteFile(t, filepath.Join(nestedDir, "hello.txt"), "hello")
	c.WriteExecutable("nested/cat-wrapper.sh", `#!/bin/sh
	echo WRAPPER_RAN >&2
	exec "$AGENT_SANDBOX_REAL" "$@"
	`)

	stdout, stderr, code := RunBinaryWithEnv(t, c.Env,
		"-C", c.Dir,
		"cat", "nested/hello.txt",
	)
	if code != 0 {
		t.Fatalf("expected cat to work in outer sandbox, got exit code %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "hello") {
		t.Fatalf("expected cat output to contain hello, got: %q", stdout)
	}

	if strings.Contains(stderr, "WRAPPER_RAN") {
		t.Fatalf("did not expect wrapper to run in outer sandbox, stderr: %s", stderr)
	}

	stdout, stderr, code = RunBinaryWithEnv(t, c.Env,
		"-C", c.Dir,
		"agent-sandbox", "-C", nestedDir, "--cmd", "cat=cat-wrapper.sh", "cat", "hello.txt",
	)

	skipIfNestedUnsupported(t, stderr, code)

	if code != 0 {
		t.Fatalf("expected cat to succeed in inner sandbox, got exit code %d\nstderr: %s", code, stderr)
	}

	if !strings.Contains(stdout, "hello") {
		t.Fatalf("expected inner cat output to contain hello, got: %q", stdout)
	}

	if !strings.Contains(stderr, "WRAPPER_RAN") {
		t.Fatalf("expected wrapper to run in inner sandbox (stderr contains WRAPPER_RAN), got:\n%s", stderr)
	}
}

func Test_Nested_Sandbox_Inner_Can_Enable_Git_Preset_With_CmdFlag_When_Outer_Disabled(t *testing.T) {
	t.Parallel()
	RequireWrapperMounting(t)

	env, workDir := setupGitRepoForCommandTests(t)

	_, stderr, code := RunBinaryWithEnv(t, env,
		"-C", workDir,
		"--cmd", "git=true",
		"git", "checkout", "-b", "outer-branch",
	)
	if code != 0 {
		t.Fatalf("expected git checkout to work in outer sandbox (git preset disabled), got exit code %d\nstderr: %s", code, stderr)
	}

	_, stderr, code = RunBinaryWithEnv(t, env,
		"-C", workDir,
		"--cmd", "git=true",
		"agent-sandbox", "-C", workDir, "--cmd", "git=@git", "git", "checkout", "-b", "inner-branch",
	)

	skipIfNestedUnsupported(t, stderr, code)

	if code == 0 {
		t.Fatal("expected inner git checkout to be blocked by @git preset, got exit code 0")
	}

	if !strings.Contains(stderr, "blocked") {
		t.Fatalf("expected inner git checkout to be blocked (stderr contains 'blocked'), got:\n%s", stderr)
	}
}

func skipIfNestedUnsupported(t *testing.T, stderr string, code int) {
	t.Helper()

	if code == 0 {
		return
	}

	lower := strings.ToLower(stderr)
	if strings.Contains(stderr, "uid map") ||
		strings.Contains(stderr, "ns failed") ||
		strings.Contains(stderr, "user namespace") ||
		strings.Contains(lower, "operation not permitted") {
		t.Skip("nested namespaces not supported")
	}
}
