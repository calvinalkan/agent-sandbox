package main

import (
	"strings"
	"testing"
)

func Test_MulticallCmd_Not_In_Help_When_Help_Is_Shown(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	stdout, _, _ := c.Run("--help")

	if strings.Contains(stdout, "multicall") {
		t.Error("multicall should not appear in --help output")
	}
}

func Test_ParseGitArgs_Finds_Subcommand_When_At_Start(t *testing.T) {
	t.Parallel()

	subcommand, rest := parseGitArgs([]string{"status", "--short"})

	if subcommand != "status" {
		t.Errorf("subcommand = %q, want %q", subcommand, "status")
	}

	if len(rest) != 1 || rest[0] != "--short" {
		t.Errorf("rest = %v, want [--short]", rest)
	}
}

func Test_ParseGitArgs_Skips_Global_Flags_When_Present(t *testing.T) {
	t.Parallel()

	args := []string{"-C", "/repo", "--no-pager", "commit", "-m", "msg"}
	subcommand, rest := parseGitArgs(args)

	if subcommand != "commit" {
		t.Errorf("subcommand = %q, want %q", subcommand, "commit")
	}

	if len(rest) != 2 || rest[0] != "-m" || rest[1] != "msg" {
		t.Errorf("rest = %v, want [-m msg]", rest)
	}
}

func Test_HasFlag_Handles_Equals_Form_When_Value_Is_Assigned(t *testing.T) {
	t.Parallel()

	if !hasFlag([]string{"--force-with-lease=branch"}, "--force-with-lease") {
		t.Error("hasFlag should find --force-with-lease in --force-with-lease=branch")
	}
}

func Test_HasFlag_Skips_Dash_Space_Prefix_When_Arg_Looks_Like_Bullet_Point(t *testing.T) {
	t.Parallel()

	// Regression: commit message "- add option-fuzzing..." was being detected as -n
	// because hasFlag iterated through characters and found 'n' in "option"
	args := []string{"-m", "test", "-m", "- add option-fuzzing variants"}
	if hasFlag(args, "-n", "--no-verify") {
		t.Error("hasFlag should not treat '- add option-fuzzing...' as containing -n flag")
	}
}

func Test_HasFlag_Detects_Standalone_Short_Flag_When_Present(t *testing.T) {
	t.Parallel()

	args := []string{"-m", "test", "-n"}
	if !hasFlag(args, "-n", "--no-verify") {
		t.Error("hasFlag should detect standalone -n flag")
	}
}

func Test_HasFlag_Detects_Combined_Short_Flags_When_Bundled(t *testing.T) {
	t.Parallel()

	// git clean -fd should detect -f
	args := []string{"-fd"}
	if !hasFlag(args, "-f", "--force") {
		t.Error("hasFlag should detect -f in combined -fd")
	}
}

func Test_IsGitOperationBlocked_Blocks_Reset_Hard_When_Hard_Reset_Is_Requested(t *testing.T) {
	t.Parallel()

	err := isBlockedGitOperation("reset", []string{"--hard"})
	if err == nil {
		t.Error("reset --hard should be blocked")
	}
}
