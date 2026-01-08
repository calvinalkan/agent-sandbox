package main

import (
	"strings"
	"testing"
)

func Test_Run_Shows_Help_When_No_Args(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	stdout, _, code := c.Run()

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	AssertContains(t, stdout, "agent-sandbox - filesystem sandbox")
	AssertContains(t, stdout, "Commands:")
}

func Test_Run_Shows_Help_When_Help_Flag(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	stdout, _, code := c.Run("--help")

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	AssertContains(t, stdout, "agent-sandbox - filesystem sandbox")
	AssertContains(t, stdout, "Commands:")
}

func Test_Run_Shows_Help_When_H_Flag(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	stdout, _, code := c.Run("-h")

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	AssertContains(t, stdout, "agent-sandbox - filesystem sandbox")
	AssertContains(t, stdout, "Commands:")
}

func Test_Run_Global_Help_Shows_Description_And_Footer(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	stdout, _, code := c.Run("--help")

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	// Verify tagline is present
	AssertContains(t, stdout, "agent-sandbox - filesystem sandbox for agentic coding workflows")

	// Verify footer hint is present
	AssertContains(t, stdout, "Run 'agent-sandbox <command> --help' for more information on a command.")
}

func Test_Run_Shows_Version_When_Version_Flag(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	stdout, _, code := c.Run("--version")

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	AssertContains(t, stdout, "agent-sandbox")
	// Default version is "dev" when not built with ldflags
	AssertContains(t, stdout, "dev")
	// When built from source (no ldflags), show cleaner output
	AssertContains(t, stdout, "built from source")
}

func Test_Run_Shows_Version_When_V_Flag(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	stdout, _, code := c.Run("-v")

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	AssertContains(t, stdout, "agent-sandbox")
	AssertContains(t, stdout, "dev (built from source)")
}

func Test_Run_Version_Flag_In_Help_Output(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	stdout, _, code := c.Run("--help")

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	AssertContains(t, stdout, "--version")
	AssertContains(t, stdout, "Show version")
}

func Test_Run_Error_Output_Contains_Error_Prefix(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	_, stderr, code := c.Run("--unknown-flag")

	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}

	// Error output should contain "error:" (may or may not have ANSI codes depending on TTY)
	if !strings.Contains(stderr, "error:") {
		t.Errorf("stderr should contain 'error:', got: %s", stderr)
	}
}

func Test_Run_Fails_With_Error_When_Unknown_Global_Flag(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--unknown", "exec")

	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}

	AssertContains(t, stderr, "unknown flag: --unknown")
	AssertContains(t, stderr, "Usage:")
}

func Test_Run_Shows_Blank_Line_Between_Global_Flag_Error_And_Usage(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--unknown", "exec")

	if code != 1 {
		t.Errorf("expected exit code 1, got %d", code)
	}

	// Error message should be followed by blank line before usage help
	AssertContains(t, stderr, "error: unknown flag: --unknown\n\nUsage:")
}

func Test_Run_Help_Shows_All_Commands(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	stdout, _, code := c.Run("--help")

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}

	AssertContains(t, stdout, "exec")
	AssertContains(t, stdout, "check")
}

func Test_Config_Uses_Defaults_When_No_Config_File(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Should work without any config file
	stdout, stderr, code := c.Run("--help")

	if code != 0 {
		t.Errorf("exit code = %d, want 0\nstderr: %s", code, stderr)
	}

	AssertContains(t, stdout, "agent-sandbox")
}

func Test_Config_Uses_Custom_Config_When_Config_Flag(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	c.WriteFile("custom-config.jsonc", `{"network": false}`)

	// Should load custom config without error
	stdout, stderr, code := c.Run("--config", "custom-config.jsonc", "--help")

	if code != 0 {
		t.Errorf("exit code = %d, want 0\nstderr: %s", code, stderr)
	}

	AssertContains(t, stdout, "agent-sandbox")
}

func Test_Config_Invalid_JSON_Returns_Error(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	c.WriteFile(".agent-sandbox.jsonc", `{invalid json}`)

	_, stderr, code := c.Run("--help")

	if code != 1 {
		t.Errorf("expected exit code 1 for invalid config, got %d", code)
	}

	AssertContains(t, stderr, "parsing config")
}

func Test_Config_Missing_Explicit_Config_Returns_Error(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Reference a config file that doesn't exist - should error (per spec)
	_, stderr, code := c.Run("--config", "nonexistent.jsonc", "--help")

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}

	AssertContains(t, stderr, "nonexistent.jsonc")
}

func Test_Config_XDG_CONFIG_HOME_Respected(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create XDG config directory
	xdgConfig := c.Dir + "/xdg-config"
	c.WriteFile("xdg-config/agent-sandbox/config.jsonc", `{"network": false}`)

	// Set XDG_CONFIG_HOME in env
	c.Env["XDG_CONFIG_HOME"] = xdgConfig

	// Should load XDG config without error
	stdout, stderr, code := c.Run("--help")

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	AssertContains(t, stdout, "agent-sandbox")
}

func Test_Config_XDG_CONFIG_HOME_Invalid_JSON_Returns_Error(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create XDG config directory with invalid JSON
	xdgConfig := c.Dir + "/xdg-config"
	c.WriteFile("xdg-config/agent-sandbox/config.json", `{invalid}`)

	c.Env["XDG_CONFIG_HOME"] = xdgConfig

	_, stderr, code := c.Run("--help")

	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}

	AssertContains(t, stderr, "parsing config")
}
