package main

import (
	"bytes"
	"strings"
	"testing"
)

// ============================================================================
// DebugLogger basic functionality tests
// ============================================================================

func Test_DebugLogger_Is_Disabled_When_Output_Is_Nil(t *testing.T) {
	t.Parallel()

	debug := NewDebugLogger(nil)

	if debug.Enabled() {
		t.Error("expected logger to be disabled when output is nil")
	}
}

func Test_DebugLogger_Is_Enabled_When_Output_Is_Not_Nil(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	if !debug.Enabled() {
		t.Error("expected logger to be enabled when output is not nil")
	}
}

func Test_DebugLogger_Section_Outputs_Header(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	debug.Section("Test Section")

	output := buf.String()
	if !strings.Contains(output, "=== Test Section ===") {
		t.Errorf("expected section header, got: %s", output)
	}
}

func Test_DebugLogger_Section_Is_Noop_When_Disabled(t *testing.T) {
	t.Parallel()

	debug := NewDebugLogger(nil)
	debug.Section("Test Section")
	// No panic, no output - test passes if no error
}

func Test_DebugLogger_Logf_Outputs_Formatted_Message(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	debug.Logf("value is %d", 42)

	output := buf.String()
	if !strings.Contains(output, "value is 42") {
		t.Errorf("expected formatted message, got: %s", output)
	}
}

func Test_DebugLogger_Path_Outputs_Full_Entry_When_Different(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	debug.Path("~/.ssh", "/home/user/.ssh", PathAccessExclude, PathSourcePreset)

	output := buf.String()
	if !strings.Contains(output, "~/.ssh -> /home/user/.ssh [exclude] (from preset)") {
		t.Errorf("expected path entry with arrow, got: %s", output)
	}
}

func Test_DebugLogger_Path_Outputs_Simple_Entry_When_Same(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	debug.Path("/tmp", "/tmp", PathAccessRw, PathSourceCLI)

	output := buf.String()
	// When original == resolved, no arrow should be shown
	if strings.Contains(output, "->") {
		t.Errorf("expected path entry without arrow when original equals resolved, got: %s", output)
	}

	if !strings.Contains(output, "/tmp [rw] (from cli)") {
		t.Errorf("expected path entry, got: %s", output)
	}
}

func Test_DebugLogger_ConfigFile_Outputs_Loaded_Path(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	debug.ConfigFile("Global config", "/home/user/.config/agent-sandbox/config.json", true)

	output := buf.String()
	if !strings.Contains(output, "Global config: /home/user/.config/agent-sandbox/config.json") {
		t.Errorf("expected config file entry, got: %s", output)
	}
}

func Test_DebugLogger_ConfigFile_Outputs_Not_Found(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	debug.ConfigFile("Project config", "", false)

	output := buf.String()
	if !strings.Contains(output, "Project config: (not found)") {
		t.Errorf("expected not found message, got: %s", output)
	}
}

func Test_DebugLogger_BoolSetting_Outputs_Value_And_Source(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	debug.BoolSetting("network", true, "cli")

	output := buf.String()
	if !strings.Contains(output, "network: true (cli)") {
		t.Errorf("expected bool setting, got: %s", output)
	}
}

func Test_DebugLogger_PresetList_Outputs_Presets(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	debug.PresetList("Applied presets", []string{"@base", "@caches", "@git"})

	output := buf.String()
	if !strings.Contains(output, "Applied presets: @base, @caches, @git") {
		t.Errorf("expected preset list, got: %s", output)
	}
}

func Test_DebugLogger_PresetList_Outputs_None_When_Empty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	debug.PresetList("Removed presets", nil)

	output := buf.String()
	if !strings.Contains(output, "Removed presets: (none)") {
		t.Errorf("expected none message, got: %s", output)
	}
}

func Test_DebugLogger_CommandWrapper_Outputs_Block(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	debug.CommandWrapper("rm", CommandRule{Kind: CommandRuleBlock})

	output := buf.String()
	if !strings.Contains(output, "rm: blocked") {
		t.Errorf("expected blocked command, got: %s", output)
	}
}

func Test_DebugLogger_CommandWrapper_Outputs_Raw(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	debug.CommandWrapper("git", CommandRule{Kind: CommandRuleRaw})

	output := buf.String()
	if !strings.Contains(output, "git: raw (no wrapper)") {
		t.Errorf("expected raw command, got: %s", output)
	}
}

func Test_DebugLogger_CommandWrapper_Outputs_Preset(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	debug.CommandWrapper("git", CommandRule{Kind: CommandRulePreset, Value: "@git"})

	output := buf.String()
	if !strings.Contains(output, "git: @git (built-in wrapper)") {
		t.Errorf("expected preset wrapper, got: %s", output)
	}
}

func Test_DebugLogger_CommandWrapper_Outputs_Script(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	debug.CommandWrapper("npm", CommandRule{Kind: CommandRuleScript, Value: "/path/to/wrapper.sh"})

	output := buf.String()
	if !strings.Contains(output, "npm: /path/to/wrapper.sh (custom script)") {
		t.Errorf("expected script wrapper, got: %s", output)
	}
}

func Test_DebugLogger_BwrapArgs_Groups_Flag_And_Values(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	args := []string{"--ro-bind", "/src", "/dest", "--bind", "/tmp", "/tmp", "--chdir", "/work"}
	debug.BwrapArgs(args)

	output := buf.String()
	if !strings.Contains(output, "--ro-bind /src /dest") {
		t.Errorf("expected grouped bwrap args, got: %s", output)
	}

	if !strings.Contains(output, "--bind /tmp /tmp") {
		t.Errorf("expected bind args, got: %s", output)
	}

	if !strings.Contains(output, "--chdir /work") {
		t.Errorf("expected chdir arg, got: %s", output)
	}
}

// ============================================================================
// CLI integration tests for --debug flag
// ============================================================================

func Test_Debug_Flag_Outputs_Config_Loading_Section(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--debug", "echo", "hello")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	AssertContains(t, stderr, "=== Config Loading ===")
}

func Test_Debug_Flag_Outputs_Config_Merge_Section(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--debug", "echo", "hello")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	AssertContains(t, stderr, "=== Config Merge ===")
}

func Test_Debug_Flag_Shows_Network_Setting(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--debug", "echo", "hello")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	AssertContains(t, stderr, "network: true")
}

func Test_Debug_Flag_Shows_Docker_Setting(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--debug", "echo", "hello")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	AssertContains(t, stderr, "docker: false")
}

func Test_Debug_Flag_Shows_CLI_Override_For_Network(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--debug", "--network=false", "echo", "hello")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	AssertContains(t, stderr, "network: false (cli)")
}

func Test_Debug_Flag_Shows_Project_Config_When_Loaded(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	c.WriteFile(".agent-sandbox.json", `{"network": false}`)

	_, stderr, code := c.Run("--debug", "echo", "hello")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	AssertContains(t, stderr, "Project config:")
	AssertContains(t, stderr, ".agent-sandbox.json")
}

func Test_Debug_Flag_Shows_Global_Config_When_Loaded(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Global config is at $XDG_CONFIG_HOME/agent-sandbox/config.json
	// Set XDG_CONFIG_HOME to point to our test directory
	configDir := c.Dir + "/xdg-config"
	c.Env["XDG_CONFIG_HOME"] = configDir

	c.WriteFile("xdg-config/agent-sandbox/config.json", `{"docker": true}`)

	_, stderr, code := c.Run("--debug", "echo", "hello")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// The global config should be loaded and displayed
	AssertContains(t, stderr, "Global config:")
	AssertContains(t, stderr, "agent-sandbox/config.json")
}

func Test_Debug_Flag_Shows_Default_Commands(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--debug", "echo", "hello")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Default config has git=@git
	AssertContains(t, stderr, "commands:")
	AssertContains(t, stderr, "git:")
}

func Test_Debug_Is_Disabled_By_Default(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("echo", "hello")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	// Should NOT contain debug sections when --debug is not specified
	AssertNotContains(t, stderr, "=== Config Loading ===")
	AssertNotContains(t, stderr, "=== Config Merge ===")
}

func Test_Debug_Works_With_Explicit_Config(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	c.WriteFile("custom-config.json", `{"docker": true}`)

	_, stderr, code := c.Run("--config", "custom-config.json", "--debug", "echo", "hello")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}

	AssertContains(t, stderr, "Explicit config (--config):")
	AssertContains(t, stderr, "custom-config.json")
}

// ============================================================================
// Helper function tests
// ============================================================================

func Test_DebugPresetExpansion_Outputs_Applied_And_Removed(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	DebugPresetExpansion(debug, []string{"!@lint/python"}, []string{"@base", "@caches", "@git", "@lint/ts", "@lint/go"}, []string{"@lint/python"})

	output := buf.String()
	AssertContains(t, output, "=== Preset Expansion ===")
	AssertContains(t, output, "Input presets: [!@lint/python]")
	AssertContains(t, output, "Applied presets: @base, @caches, @git, @lint/ts, @lint/go")
	AssertContains(t, output, "Removed presets: @lint/python")
}

func Test_DebugPresetExpansion_Shows_Default_When_Empty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	DebugPresetExpansion(debug, nil, []string{"@all"}, nil)

	output := buf.String()
	AssertContains(t, output, "Input presets: (none, using default @all)")
}

func Test_DebugPathResolution_Outputs_Paths(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	paths := []ResolvedPath{
		{Original: "~/.ssh", Resolved: "/home/user/.ssh", Access: PathAccessExclude, Source: PathSourcePreset},
		{Original: ".", Resolved: "/work", Access: PathAccessRw, Source: PathSourcePreset},
	}
	DebugPathResolution(debug, paths)

	output := buf.String()
	AssertContains(t, output, "=== Path Resolution ===")
	AssertContains(t, output, "~/.ssh -> /home/user/.ssh [exclude] (from preset)")
	AssertContains(t, output, ". -> /work [rw] (from preset)")
}

func Test_DebugPathResolution_Shows_Empty_Message(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	DebugPathResolution(debug, nil)

	output := buf.String()
	AssertContains(t, output, "No paths resolved")
}

func Test_DebugCommandWrappers_Outputs_Commands(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	commands := map[string]CommandRule{
		"git": {Kind: CommandRulePreset, Value: "@git"},
		"rm":  {Kind: CommandRuleBlock},
	}
	DebugCommandWrappers(debug, commands)

	output := buf.String()
	AssertContains(t, output, "=== Command Wrappers ===")
	AssertContains(t, output, "git: @git (built-in wrapper)")
	AssertContains(t, output, "rm: blocked")
}

func Test_DebugBwrapArgs_Outputs_Arguments(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	debug := NewDebugLogger(&buf)

	args := []string{"--die-with-parent", "--unshare-all", "--share-net", "--ro-bind", "/", "/"}
	DebugBwrapArgs(debug, args)

	output := buf.String()
	AssertContains(t, output, "=== Generated bwrap Arguments ===")
	AssertContains(t, output, "--die-with-parent")
	AssertContains(t, output, "--ro-bind / /")
}
