package main

import (
	"os"
	"testing"

	flag "github.com/spf13/pflag"
)

func Test_Exec_Accepts_Network_Flag_In_Implicit_Mode(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// This should work: implicit exec with --network flag
	_, stderr, code := c.Run("--network=false", "echo", "hello")

	// Should not fail with "unknown flag" - exec command handles it
	AssertNotContains(t, stderr, "unknown flag")

	// Exit code 0 means the command was accepted (exec prints "not yet implemented")
	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}
}

func Test_Exec_Accepts_Docker_Flag_In_Implicit_Mode(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--docker", "echo", "hello")

	AssertNotContains(t, stderr, "unknown flag")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}
}

func Test_Exec_Accepts_Ro_Flag_In_Implicit_Mode(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--ro", "/tmp", "echo", "hello")

	AssertNotContains(t, stderr, "unknown flag")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}
}

func Test_Exec_Accepts_Rw_Flag_In_Implicit_Mode(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--rw", "/tmp", "echo", "hello")

	AssertNotContains(t, stderr, "unknown flag")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}
}

func Test_Exec_Accepts_Exclude_Flag_In_Implicit_Mode(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--exclude", ".env", "echo", "hello")

	AssertNotContains(t, stderr, "unknown flag")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}
}

func Test_Exec_Accepts_Cmd_Flag_In_Implicit_Mode(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	_, stderr, code := c.Run("--cmd", "git=true", "echo", "hello")

	AssertNotContains(t, stderr, "unknown flag")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}
}

func Test_Exec_Accepts_Multiple_Flags_In_Implicit_Mode(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Multiple exec flags together
	_, stderr, code := c.Run("--network=false", "--docker", "--ro", "/tmp", "echo", "hello")

	AssertNotContains(t, stderr, "unknown flag")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}
}

func Test_Exec_Works_With_Global_And_Exec_Flags(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Mix of global flag (--cwd is added by test helper) and exec flags
	_, stderr, code := c.Run("--network=false", "echo", "hello")

	AssertNotContains(t, stderr, "unknown flag")

	if code != 0 {
		t.Errorf("expected exit code 0, got %d\nstderr: %s", code, stderr)
	}
}

func Test_ApplyExecFlags_Network_Overrides_Config(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	if cfg.Network == nil || *cfg.Network != true {
		t.Fatal("default network should be true")
	}

	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.Bool("network", true, "")
	_ = flags.Parse([]string{"--network=false"})

	err := applyExecFlags(&cfg, flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Network == nil || *cfg.Network != false {
		t.Errorf("expected network=false after override, got %v", cfg.Network)
	}
}

func Test_ApplyExecFlags_Docker_Overrides_Config(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	if cfg.Docker == nil || *cfg.Docker != false {
		t.Fatal("default docker should be false")
	}

	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.Bool("docker", false, "")
	_ = flags.Parse([]string{"--docker=true"})

	err := applyExecFlags(&cfg, flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Docker == nil || *cfg.Docker != true {
		t.Errorf("expected docker=true after override, got %v", cfg.Docker)
	}
}

func Test_ApplyExecFlags_Ro_Appends_To_Config(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Filesystem: FilesystemConfig{
			Ro: []string{"/existing"},
		},
	}

	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.StringArray("ro", nil, "")
	_ = flags.Parse([]string{"--ro", "/new1", "--ro", "/new2"})

	err := applyExecFlags(&cfg, flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"/existing", "/new1", "/new2"}
	if len(cfg.Filesystem.Ro) != len(expected) {
		t.Errorf("expected %v, got %v", expected, cfg.Filesystem.Ro)
	}

	for i, v := range expected {
		if cfg.Filesystem.Ro[i] != v {
			t.Errorf("expected ro[%d]=%q, got %q", i, v, cfg.Filesystem.Ro[i])
		}
	}
}

func Test_ApplyExecFlags_Rw_Appends_To_Config(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Filesystem: FilesystemConfig{
			Rw: []string{"/existing"},
		},
	}

	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.StringArray("rw", nil, "")
	_ = flags.Parse([]string{"--rw", "/new"})

	err := applyExecFlags(&cfg, flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Filesystem.Rw) != 2 || cfg.Filesystem.Rw[1] != "/new" {
		t.Errorf("expected rw to have /existing and /new, got %v", cfg.Filesystem.Rw)
	}
}

func Test_ApplyExecFlags_Exclude_Appends_To_Config(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Filesystem: FilesystemConfig{
			Exclude: []string{".env"},
		},
	}

	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.StringArray("exclude", nil, "")
	_ = flags.Parse([]string{"--exclude", ".secrets"})

	err := applyExecFlags(&cfg, flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Filesystem.Exclude) != 2 || cfg.Filesystem.Exclude[1] != ".secrets" {
		t.Errorf("expected exclude to have .env and .secrets, got %v", cfg.Filesystem.Exclude)
	}
}

func Test_ApplyExecFlags_Cmd_Merges_Into_Commands(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig() // Has git=@git by default

	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.StringArray("cmd", nil, "")
	_ = flags.Parse([]string{"--cmd", "rm=false", "--cmd", "git=true"})

	err := applyExecFlags(&cfg, flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// rm should be blocked
	if rule, ok := cfg.Commands["rm"]; !ok || rule.Kind != CommandRuleBlock {
		t.Errorf("expected rm=block, got %v", cfg.Commands["rm"])
	}

	// git should be overridden to raw (true)
	if rule, ok := cfg.Commands["git"]; !ok || rule.Kind != CommandRuleRaw {
		t.Errorf("expected git=raw, got %v", cfg.Commands["git"])
	}
}

func Test_ApplyExecFlags_Cmd_Comma_Separated(t *testing.T) {
	t.Parallel()

	cfg := Config{}

	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.StringArray("cmd", nil, "")
	_ = flags.Parse([]string{"--cmd", "git=true,rm=false"})

	err := applyExecFlags(&cfg, flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rule, ok := cfg.Commands["git"]; !ok || rule.Kind != CommandRuleRaw {
		t.Errorf("expected git=raw, got %v", cfg.Commands["git"])
	}

	if rule, ok := cfg.Commands["rm"]; !ok || rule.Kind != CommandRuleBlock {
		t.Errorf("expected rm=block, got %v", cfg.Commands["rm"])
	}
}

func Test_ApplyExecFlags_Cmd_Preset(t *testing.T) {
	t.Parallel()

	cfg := Config{}

	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.StringArray("cmd", nil, "")
	_ = flags.Parse([]string{"--cmd", "git=@git"})

	err := applyExecFlags(&cfg, flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rule, ok := cfg.Commands["git"]; !ok || rule.Kind != CommandRulePreset || rule.Value != "@git" {
		t.Errorf("expected git=@git preset, got %v", cfg.Commands["git"])
	}
}

func Test_ApplyExecFlags_Cmd_Script(t *testing.T) {
	t.Parallel()

	cfg := Config{}

	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.StringArray("cmd", nil, "")
	_ = flags.Parse([]string{"--cmd", "npm=/path/to/wrapper.sh"})

	err := applyExecFlags(&cfg, flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rule, ok := cfg.Commands["npm"]; !ok || rule.Kind != CommandRuleScript || rule.Value != "/path/to/wrapper.sh" {
		t.Errorf("expected npm=script, got %v", cfg.Commands["npm"])
	}
}

func Test_ApplyExecFlags_Unset_Flags_Dont_Override(t *testing.T) {
	t.Parallel()

	networkVal := false
	cfg := Config{
		Network: &networkVal,
	}

	// Parse flags but don't set any
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.Bool("network", true, "")
	flags.Bool("docker", false, "")
	_ = flags.Parse([]string{}) // No flags set

	err := applyExecFlags(&cfg, flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Config should be unchanged
	if cfg.Network == nil || *cfg.Network != false {
		t.Errorf("expected network to remain false, got %v", cfg.Network)
	}
}

func Test_ApplyExecFlags_Invalid_Cmd_Format(t *testing.T) {
	t.Parallel()

	cfg := Config{}

	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.StringArray("cmd", nil, "")
	_ = flags.Parse([]string{"--cmd", "invalid-no-equals"})

	err := applyExecFlags(&cfg, flags)
	if err == nil {
		t.Fatal("expected error for invalid --cmd format")
	}

	AssertContains(t, err.Error(), "invalid --cmd format")
}

func Test_ApplyExecFlags_Empty_Key_In_Cmd(t *testing.T) {
	t.Parallel()

	cfg := Config{}

	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.StringArray("cmd", nil, "")
	_ = flags.Parse([]string{"--cmd", "=true"})

	err := applyExecFlags(&cfg, flags)
	if err == nil {
		t.Fatal("expected error for empty key in --cmd")
	}

	AssertContains(t, err.Error(), "empty key")
}

func Test_Help_Works_With_Invalid_Config(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	c.WriteFile(".agent-sandbox.jsonc", `{invalid json}`)

	// Help should work even with invalid config
	stdout, _, code := c.Run("--help")

	if code != 0 {
		t.Errorf("expected exit code 0 for --help, got %d", code)
	}

	AssertContains(t, stdout, "agent-sandbox")
	AssertContains(t, stdout, "Commands:")
}

func Test_Help_Works_With_Missing_Explicit_Config(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Help should work even with missing explicit config
	stdout, _, code := c.Run("--config", "nonexistent.json", "--help")

	if code != 0 {
		t.Errorf("expected exit code 0 for --help, got %d", code)
	}

	AssertContains(t, stdout, "agent-sandbox")
}

// ============================================================================
// GetHomeDir tests
// ============================================================================

func Test_GetHomeDir_Returns_Home_When_Valid_Env_Set(t *testing.T) {
	t.Parallel()

	// Use temp dir as a valid home directory
	tmpDir := t.TempDir()
	env := map[string]string{"HOME": tmpDir}

	home, err := GetHomeDir(env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if home != tmpDir {
		t.Errorf("expected home %q, got %q", tmpDir, home)
	}
}

func Test_GetHomeDir_Returns_Error_When_Home_Does_Not_Exist(t *testing.T) {
	t.Parallel()

	env := map[string]string{"HOME": "/nonexistent/path/that/does/not/exist"}

	_, err := GetHomeDir(env)
	if err == nil {
		t.Fatal("expected error for nonexistent home directory")
	}

	AssertContains(t, err.Error(), "cannot determine home directory")
	AssertContains(t, err.Error(), "/nonexistent/path/that/does/not/exist")
	AssertContains(t, err.Error(), "does not exist")
}

func Test_GetHomeDir_Returns_Error_When_Home_Is_File(t *testing.T) {
	t.Parallel()

	// Create a file instead of a directory
	tmpDir := t.TempDir()
	filePath := tmpDir + "/not-a-dir"

	err := os.WriteFile(filePath, []byte("test"), 0o644)
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	env := map[string]string{"HOME": filePath}

	_, err = GetHomeDir(env)
	if err == nil {
		t.Fatal("expected error when HOME points to a file")
	}

	AssertContains(t, err.Error(), "is not a directory")
	AssertContains(t, err.Error(), filePath)
}

func Test_GetHomeDir_Falls_Back_To_UserHomeDir_When_Env_Empty(t *testing.T) {
	t.Parallel()

	// Empty env map - should fall back to os.UserHomeDir()
	env := map[string]string{}

	home, err := GetHomeDir(env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Result should be a valid directory
	info, err := os.Stat(home)
	if err != nil {
		t.Fatalf("returned home %q does not exist: %v", home, err)
	}

	if !info.IsDir() {
		t.Errorf("returned home %q is not a directory", home)
	}
}

func Test_GetHomeDir_Error_Suggests_Setting_HOME(t *testing.T) {
	t.Parallel()

	env := map[string]string{"HOME": "/nonexistent/path"}

	_, err := GetHomeDir(env)
	if err == nil {
		t.Fatal("expected error for nonexistent home directory")
	}

	// Error message should be actionable
	AssertContains(t, err.Error(), "HOME")
}

// ============================================================================
// Exec command home directory validation tests
// ============================================================================

func Test_Exec_Returns_Error_When_Home_Directory_Does_Not_Exist(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	c.Env["HOME"] = "/nonexistent/path/that/does/not/exist"

	_, stderr, code := c.Run("exec", "echo", "hello")

	if code == 0 {
		t.Errorf("expected non-zero exit code for nonexistent home")
	}

	AssertContains(t, stderr, "cannot determine home directory")
}

func Test_Exec_Returns_Error_When_Home_Is_File(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)

	// Create a file where HOME points to
	filePath := c.Dir + "/not-a-dir"
	c.WriteFile("not-a-dir", "test content")
	c.Env["HOME"] = filePath

	_, stderr, code := c.Run("exec", "echo", "hello")

	if code == 0 {
		t.Errorf("expected non-zero exit code when HOME is a file")
	}

	AssertContains(t, stderr, "is not a directory")
}

func Test_Exec_Succeeds_When_Home_Is_Valid(t *testing.T) {
	t.Parallel()

	c := NewCLITester(t)
	// Use the temp dir as HOME (it's valid)
	c.Env["HOME"] = c.Dir

	_, stderr, code := c.Run("exec", "echo", "hello")

	// Should succeed (exec prints "not yet implemented" but exit 0)
	if code != 0 {
		t.Errorf("expected exit code 0 for valid home, got %d\nstderr: %s", code, stderr)
	}
}
