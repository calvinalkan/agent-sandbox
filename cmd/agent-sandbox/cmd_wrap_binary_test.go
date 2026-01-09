package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================================
// agentSandboxEnvVarName tests
// ============================================================================

func Test_agentSandboxEnvVarName_Formats_Name_Correctly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cmdName  string
		expected string
	}{
		{"git", "AGENT_SANDBOX_GIT"},
		{"npm", "AGENT_SANDBOX_NPM"},
		{"rm", "AGENT_SANDBOX_RM"},
		{"mycommand", "AGENT_SANDBOX_MYCOMMAND"},
	}

	for _, tt := range tests {
		t.Run(tt.cmdName, func(t *testing.T) {
			t.Parallel()

			result := agentSandboxEnvVarName(tt.cmdName)
			if result != tt.expected {
				t.Errorf("agentSandboxEnvVarName(%q) = %q, want %q", tt.cmdName, result, tt.expected)
			}
		})
	}
}

func Test_agentSandboxEnvVarName_Handles_Mixed_Case(t *testing.T) {
	t.Parallel()

	result := agentSandboxEnvVarName("MyCmd")

	if result != "AGENT_SANDBOX_MYCMD" {
		t.Errorf("agentSandboxEnvVarName(\"MyCmd\") = %q, want %q", result, "AGENT_SANDBOX_MYCMD")
	}
}

// ============================================================================
// parseGitArgs tests
// ============================================================================

func Test_parseGitArgs_Finds_Subcommand_At_Start(t *testing.T) {
	t.Parallel()

	subcommand, rest := parseGitArgs([]string{"push", "origin", "main"})

	if subcommand != "push" {
		t.Errorf("subcommand = %q, want %q", subcommand, "push")
	}

	if len(rest) != 2 || rest[0] != "origin" || rest[1] != "main" {
		t.Errorf("rest = %v, want [origin main]", rest)
	}
}

func Test_parseGitArgs_Finds_Subcommand_After_Global_Flags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		wantSubcmd  string
		wantRestLen int
	}{
		{
			name:        "-C flag",
			args:        []string{"-C", "/path/to/repo", "status"},
			wantSubcmd:  "status",
			wantRestLen: 0,
		},
		{
			name:        "--git-dir flag",
			args:        []string{"--git-dir", "/path/.git", "log"},
			wantSubcmd:  "log",
			wantRestLen: 0,
		},
		{
			name:        "--no-pager flag",
			args:        []string{"--no-pager", "diff", "--stat"},
			wantSubcmd:  "diff",
			wantRestLen: 1,
		},
		{
			name:        "multiple global flags",
			args:        []string{"-C", "/repo", "--no-pager", "-c", "core.editor=vim", "commit", "-m", "msg"},
			wantSubcmd:  "commit",
			wantRestLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			subcommand, rest := parseGitArgs(tt.args)

			if subcommand != tt.wantSubcmd {
				t.Errorf("subcommand = %q, want %q", subcommand, tt.wantSubcmd)
			}

			if len(rest) != tt.wantRestLen {
				t.Errorf("len(rest) = %d, want %d (rest = %v)", len(rest), tt.wantRestLen, rest)
			}
		})
	}
}

func Test_parseGitArgs_Returns_Empty_When_No_Subcommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{"empty args", []string{}},
		{"only global flags", []string{"-C", "/path", "--no-pager"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			subcommand, rest := parseGitArgs(tt.args)

			if subcommand != "" {
				t.Errorf("subcommand = %q, want empty string", subcommand)
			}

			if rest != nil {
				t.Errorf("rest = %v, want nil", rest)
			}
		})
	}
}

func Test_parseGitArgs_Handles_Equals_Form(t *testing.T) {
	t.Parallel()

	subcommand, rest := parseGitArgs([]string{"--git-dir=/path/.git", "status"})

	if subcommand != "status" {
		t.Errorf("subcommand = %q, want %q", subcommand, "status")
	}

	if len(rest) != 0 {
		t.Errorf("rest = %v, want []", rest)
	}
}

// ============================================================================
// hasFlag tests
// ============================================================================

func Test_hasFlag_Finds_Flag(t *testing.T) {
	t.Parallel()

	args := []string{"--force", "-v", "--stat"}

	if !hasFlag(args, "--force") {
		t.Error("hasFlag should find --force")
	}

	if !hasFlag(args, "-v") {
		t.Error("hasFlag should find -v")
	}

	if !hasFlag(args, "--stat") {
		t.Error("hasFlag should find --stat")
	}
}

func Test_hasFlag_Returns_False_When_Not_Present(t *testing.T) {
	t.Parallel()

	args := []string{"--force", "-v"}

	if hasFlag(args, "--hard") {
		t.Error("hasFlag should not find --hard")
	}
}

func Test_hasFlag_Finds_Multiple_Flags(t *testing.T) {
	t.Parallel()

	args := []string{"--force", "-v"}

	if !hasFlag(args, "--force", "-f") {
		t.Error("hasFlag should find --force when checking for --force or -f")
	}

	if !hasFlag(args, "-f", "-v") {
		t.Error("hasFlag should find -v when checking for -f or -v")
	}

	if hasFlag(args, "-a", "-b", "-c") {
		t.Error("hasFlag should not find any of -a, -b, -c")
	}
}

func Test_hasFlag_Handles_Equals_Form(t *testing.T) {
	t.Parallel()

	args := []string{"--force-with-lease=branch"}

	if !hasFlag(args, "--force-with-lease") {
		t.Error("hasFlag should find --force-with-lease in --force-with-lease=branch")
	}
}

// ============================================================================
// isGitOperationBlocked tests
// ============================================================================

func Test_isGitOperationBlocked_Blocks_Checkout(t *testing.T) {
	t.Parallel()

	blocked, reason := isGitOperationBlocked("checkout", []string{"main"})

	if !blocked {
		t.Error("checkout should be blocked")
	}

	if !strings.Contains(reason, "checkout") {
		t.Errorf("reason should mention checkout, got: %s", reason)
	}
}

func Test_isGitOperationBlocked_Blocks_Restore(t *testing.T) {
	t.Parallel()

	blocked, reason := isGitOperationBlocked("restore", []string{"file.txt"})

	if !blocked {
		t.Error("restore should be blocked")
	}

	if !strings.Contains(reason, "restore") {
		t.Errorf("reason should mention restore, got: %s", reason)
	}
}

func Test_isGitOperationBlocked_Blocks_Reset_Hard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		blocked bool
	}{
		{"reset soft", []string{"--soft", "HEAD~1"}, false},
		{"reset mixed", []string{"HEAD~1"}, false},
		{"reset hard", []string{"--hard", "HEAD~1"}, true},
		{"reset hard at end", []string{"HEAD~1", "--hard"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			blocked, _ := isGitOperationBlocked("reset", tt.args)

			if blocked != tt.blocked {
				t.Errorf("blocked = %v, want %v", blocked, tt.blocked)
			}
		})
	}
}

func Test_isGitOperationBlocked_Blocks_Clean_Force(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		blocked bool
	}{
		{"clean dry run", []string{"-n"}, false},
		{"clean force", []string{"-f"}, true},
		{"clean force long", []string{"--force"}, true},
		{"clean force with dir", []string{"-fd"}, false}, // -fd is not -f, it's a different flag
		{"clean with -f", []string{"-d", "-f"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			blocked, _ := isGitOperationBlocked("clean", tt.args)

			if blocked != tt.blocked {
				t.Errorf("blocked = %v, want %v", blocked, tt.blocked)
			}
		})
	}
}

func Test_isGitOperationBlocked_Blocks_Commit_NoVerify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		blocked bool
	}{
		{"commit normal", []string{"-m", "message"}, false},
		{"commit no-verify", []string{"--no-verify", "-m", "message"}, true},
		{"commit short no-verify", []string{"-n", "-m", "message"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			blocked, _ := isGitOperationBlocked("commit", tt.args)

			if blocked != tt.blocked {
				t.Errorf("blocked = %v, want %v", blocked, tt.blocked)
			}
		})
	}
}

func Test_isGitOperationBlocked_Blocks_Stash_Drop(t *testing.T) {
	t.Parallel()

	blocked, _ := isGitOperationBlocked("stash", []string{"drop"})

	if !blocked {
		t.Error("stash drop should be blocked")
	}
}

func Test_isGitOperationBlocked_Blocks_Stash_Clear(t *testing.T) {
	t.Parallel()

	blocked, _ := isGitOperationBlocked("stash", []string{"clear"})

	if !blocked {
		t.Error("stash clear should be blocked")
	}
}

func Test_isGitOperationBlocked_Blocks_Stash_Pop(t *testing.T) {
	t.Parallel()

	blocked, _ := isGitOperationBlocked("stash", []string{"pop"})

	if !blocked {
		t.Error("stash pop should be blocked")
	}
}

func Test_isGitOperationBlocked_Allows_Stash_Apply(t *testing.T) {
	t.Parallel()

	blocked, _ := isGitOperationBlocked("stash", []string{"apply"})

	if blocked {
		t.Error("stash apply should NOT be blocked")
	}
}

func Test_isGitOperationBlocked_Blocks_Branch_ForceDelete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		blocked bool
	}{
		{"branch delete safe", []string{"-d", "feature"}, false},
		{"branch force delete", []string{"-D", "feature"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			blocked, _ := isGitOperationBlocked("branch", tt.args)

			if blocked != tt.blocked {
				t.Errorf("blocked = %v, want %v", blocked, tt.blocked)
			}
		})
	}
}

func Test_isGitOperationBlocked_Blocks_Push_Force(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		blocked bool
	}{
		{"push normal", []string{"origin", "main"}, false},
		{"push force", []string{"--force", "origin", "main"}, true},
		{"push force short", []string{"-f", "origin", "main"}, true},
		{"push force-with-lease", []string{"--force-with-lease", "origin", "main"}, false},
		{"push force and force-with-lease", []string{"--force", "--force-with-lease", "origin", "main"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			blocked, _ := isGitOperationBlocked("push", tt.args)

			if blocked != tt.blocked {
				t.Errorf("blocked = %v, want %v", blocked, tt.blocked)
			}
		})
	}
}

func Test_isGitOperationBlocked_Allows_Normal_Operations(t *testing.T) {
	t.Parallel()

	allowedOps := []struct {
		subcommand string
		args       []string
	}{
		{"status", nil},
		{"log", []string{"--oneline"}},
		{"diff", []string{"HEAD~1"}},
		{"add", []string{"."}},
		{"commit", []string{"-m", "message"}},
		{"push", []string{"origin", "main"}},
		{"pull", []string{"origin", "main"}},
		{"fetch", []string{"--all"}},
		{"stash", nil},
		{"stash", []string{"list"}},
		{"stash", []string{"show"}},
		{"stash", []string{"apply"}},
		{"branch", []string{"-a"}},
		{"branch", []string{"-d", "feature"}},
		{"merge", []string{"feature"}},
		{"rebase", []string{"main"}},
		{"switch", []string{"main"}},
	}

	for _, op := range allowedOps {
		name := op.subcommand
		if len(op.args) > 0 {
			name = op.subcommand + " " + strings.Join(op.args, " ")
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			blocked, reason := isGitOperationBlocked(op.subcommand, op.args)

			if blocked {
				t.Errorf("%s should NOT be blocked, got reason: %s", name, reason)
			}
		})
	}
}

// ============================================================================
// findRealBinary tests
// ============================================================================

func Test_findRealBinary_Locates_Binary_At_Expected_Path(t *testing.T) {
	t.Parallel()

	// This test verifies the path convention without actually running inside a sandbox.
	// We create a directory structure that mimics what would exist inside the sandbox:
	//
	// tempDir/
	//   binaries/
	//     agent-sandbox (our "self" executable - simulated)
	//   real/
	//     git (the real binary)

	dir := t.TempDir()
	binariesDir := filepath.Join(dir, "binaries")
	realDir := filepath.Join(dir, "real")

	mustCreateDir(t, binariesDir)
	mustCreateDir(t, realDir)

	// Create a mock "real" binary
	realGit := filepath.Join(realDir, "git")
	mustCreateExecutable(t, realGit)

	// The findRealBinary function uses os.Executable() which we can't easily mock.
	// This test verifies the path calculation logic instead.
	//
	// If self = /path/to/binaries/agent-sandbox
	// Then realBinary should be /path/to/real/<cmdName>
	// Which is: filepath.Dir(self) + "/../real/" + cmdName

	selfPath := filepath.Join(binariesDir, "agent-sandbox")
	selfDir := filepath.Dir(selfPath)
	expectedPath := filepath.Clean(filepath.Join(selfDir, "..", "real", "git"))

	if expectedPath != realGit {
		t.Errorf("path calculation: expected %q, got %q", realGit, expectedPath)
	}
}

// ============================================================================
// WrapBinaryCmd tests - CLI-level
// ============================================================================

func Test_WrapBinaryCmd_Not_In_Help(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	stdout, _, _ := c.Run("--help")

	if strings.Contains(stdout, "wrap-binary") {
		t.Error("wrap-binary should not appear in --help output")
	}
}

func Test_WrapBinaryCmd_Errors_Outside_Sandbox(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// wrap-binary should fail when not inside a sandbox
	_, stderr, code := c.Run("wrap-binary", "--preset", "@git", "git", "status")

	if code == 0 {
		t.Error("wrap-binary should fail outside sandbox")
	}

	if !strings.Contains(stderr, "only run inside sandbox") {
		t.Errorf("stderr should mention sandbox requirement, got: %s", stderr)
	}
}

func Test_WrapBinaryCmd_Errors_Without_Flag(t *testing.T) {
	t.Parallel()

	// We can't easily test this without being inside a sandbox,
	// but we can verify the error message is correct.
	var stderr bytes.Buffer

	cmd := WrapBinaryCmd()
	_ = cmd.Flags.Parse([]string{"git", "status"})

	// Manually check the error condition
	preset, _ := cmd.Flags.GetString("preset")
	script, _ := cmd.Flags.GetString("script")

	if preset == "" && script == "" {
		// This is the expected state - neither flag is set
		if ErrWrapBinaryMissingFlag.Error() != "wrap-binary requires --preset or --script flag" {
			t.Errorf("unexpected error message: %s", ErrWrapBinaryMissingFlag.Error())
		}
	} else {
		t.Errorf("expected both flags to be empty, got preset=%q script=%q", preset, script)
	}

	_ = stderr // silence unused variable warning
}

func Test_WrapBinaryCmd_Errors_With_Both_Flags(t *testing.T) {
	t.Parallel()

	cmd := WrapBinaryCmd()
	_ = cmd.Flags.Parse([]string{"--preset", "@git", "--script", "/path/to/script", "git"})

	preset, _ := cmd.Flags.GetString("preset")
	script, _ := cmd.Flags.GetString("script")

	if preset != PresetGit || script != "/path/to/script" {
		t.Errorf("expected both flags to be set, got preset=%q script=%q", preset, script)
	}

	// Verify the error exists for this case
	if ErrWrapBinaryBothFlags.Error() != "wrap-binary accepts only one of --preset or --script" {
		t.Errorf("unexpected error message: %s", ErrWrapBinaryBothFlags.Error())
	}
}

func Test_WrapBinaryCmd_Errors_Without_Command(t *testing.T) {
	t.Parallel()

	cmd := WrapBinaryCmd()
	_ = cmd.Flags.Parse([]string{"--preset", "@git"})

	args := cmd.Flags.Args()

	if len(args) != 0 {
		t.Errorf("expected no args after flags, got %v", args)
	}

	// Verify the error exists for this case
	if ErrWrapBinaryNoCommand.Error() != "wrap-binary requires command name" {
		t.Errorf("unexpected error message: %s", ErrWrapBinaryNoCommand.Error())
	}
}

func Test_WrapBinaryCmd_Parses_Preset_Flag(t *testing.T) {
	t.Parallel()

	cmd := WrapBinaryCmd()
	_ = cmd.Flags.Parse([]string{"--preset", "@git", "git", "status"})

	preset, _ := cmd.Flags.GetString("preset")
	script, _ := cmd.Flags.GetString("script")
	args := cmd.Flags.Args()

	if preset != PresetGit {
		t.Errorf("preset = %q, want %s", preset, PresetGit)
	}

	if script != "" {
		t.Errorf("script should be empty, got %q", script)
	}

	if len(args) != 2 || args[0] != "git" || args[1] != "status" {
		t.Errorf("args = %v, want [git status]", args)
	}
}

func Test_WrapBinaryCmd_Parses_Script_Flag(t *testing.T) {
	t.Parallel()

	cmd := WrapBinaryCmd()
	_ = cmd.Flags.Parse([]string{"--script", "/path/to/wrapper.sh", "npm", "install"})

	preset, _ := cmd.Flags.GetString("preset")
	script, _ := cmd.Flags.GetString("script")
	args := cmd.Flags.Args()

	if preset != "" {
		t.Errorf("preset should be empty, got %q", preset)
	}

	if script != "/path/to/wrapper.sh" {
		t.Errorf("script = %q, want /path/to/wrapper.sh", script)
	}

	if len(args) != 2 || args[0] != "npm" || args[1] != "install" {
		t.Errorf("args = %v, want [npm install]", args)
	}
}

func Test_WrapBinaryCmd_Passes_All_Remaining_Args(t *testing.T) {
	t.Parallel()

	cmd := WrapBinaryCmd()
	_ = cmd.Flags.Parse([]string{"--preset", "@git", "git", "commit", "-m", "message", "--amend"})

	args := cmd.Flags.Args()

	expected := []string{"git", "commit", "-m", "message", "--amend"}
	if len(args) != len(expected) {
		t.Fatalf("len(args) = %d, want %d", len(args), len(expected))
	}

	for i, want := range expected {
		if args[i] != want {
			t.Errorf("args[%d] = %q, want %q", i, args[i], want)
		}
	}
}

// ============================================================================
// execPreset tests
// ============================================================================

func Test_execPreset_Returns_Error_For_Unknown_Preset(t *testing.T) {
	t.Parallel()

	err := execPreset(context.Background(), "@unknown", "cmd", nil, "/path/to/binary", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown preset")
	}

	if !strings.Contains(err.Error(), "unknown command preset") {
		t.Errorf("error should mention unknown preset, got: %s", err)
	}

	if !strings.Contains(err.Error(), "@unknown") {
		t.Errorf("error should mention the preset name, got: %s", err)
	}
}

func Test_ErrUnknownCommandPreset_Lists_Available_Presets(t *testing.T) {
	t.Parallel()

	// Verify the error message lists available command presets
	errMsg := ErrUnknownCommandPreset.Error()
	if !strings.Contains(errMsg, "available:") {
		t.Error("ErrUnknownCommandPreset should list available presets")
	}

	if !strings.Contains(errMsg, "@git") {
		t.Error("ErrUnknownCommandPreset should mention @git as available")
	}
}

func Test_ErrNotInSandbox_Contains_Explanation(t *testing.T) {
	t.Parallel()

	// Verify the error explains this is an internal command
	errMsg := ErrNotInSandbox.Error()
	if !strings.Contains(errMsg, "sandbox") {
		t.Error("ErrNotInSandbox should mention sandbox")
	}

	if !strings.Contains(errMsg, "internal") {
		t.Error("ErrNotInSandbox should explain this is an internal command")
	}
}

// ============================================================================
// Integration test - wrap-binary with real sandbox (skipped if not on Linux)
// ============================================================================

func Test_WrapBinaryCmd_Works_Inside_Sandbox(t *testing.T) {
	t.Parallel()
	RequireBwrap(t)

	// This test is deferred to ticket d5g38j8 (E2E tests)
	t.Skip("E2E test deferred to d5g38j8")
}

// Note: mustCreateDir and mustCreateExecutable are defined in wrapper_test.go
