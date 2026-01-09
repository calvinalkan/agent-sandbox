package main

import (
	"slices"
	"strings"
	"testing"
)

func Test_PresetRegistry_Contains_All_Expected_Presets(t *testing.T) {
	t.Parallel()

	expectedPresets := []string{
		"@base",
		"@caches",
		"@git",
		"@lint/ts",
		"@lint/go",
		"@lint/python",
		"@lint/all",
		"@all",
	}

	for _, name := range expectedPresets {
		if _, ok := PresetRegistry[name]; !ok {
			t.Errorf("PresetRegistry missing expected preset %q", name)
		}
	}

	// Also verify count matches
	if len(PresetRegistry) != len(expectedPresets) {
		t.Errorf("PresetRegistry has %d presets, expected %d", len(PresetRegistry), len(expectedPresets))
	}
}

func Test_PresetRegistry_Names_All_Start_With_At_Symbol(t *testing.T) {
	t.Parallel()

	for name, preset := range PresetRegistry {
		if !strings.HasPrefix(name, "@") {
			t.Errorf("preset registry key %q does not start with @", name)
		}

		if !strings.HasPrefix(preset.Name, "@") {
			t.Errorf("preset.Name %q does not start with @", preset.Name)
		}

		// Verify key matches Name field
		if name != preset.Name {
			t.Errorf("preset registry key %q does not match preset.Name %q", name, preset.Name)
		}
	}
}

func Test_PresetRegistry_All_Have_Description(t *testing.T) {
	t.Parallel()

	for name, preset := range PresetRegistry {
		if preset.Description == "" {
			t.Errorf("preset %q has empty description", name)
		}
	}
}

func Test_PresetRegistry_All_Have_Resolve_Function(t *testing.T) {
	t.Parallel()

	for name, preset := range PresetRegistry {
		if preset.Resolve == nil {
			t.Errorf("preset %q has nil Resolve function", name)
		}
	}
}

func Test_PresetRegistry_Composite_Flag_Is_Correct(t *testing.T) {
	t.Parallel()

	// Only @all and @lint/all should be composite
	compositePresets := map[string]bool{
		"@all":      true,
		"@lint/all": true,
	}

	for name, preset := range PresetRegistry {
		expectedComposite := compositePresets[name]
		if preset.Composite != expectedComposite {
			t.Errorf("preset %q has Composite=%v, expected %v", name, preset.Composite, expectedComposite)
		}
	}
}

func Test_PresetPaths_Has_All_Access_Level_Fields(t *testing.T) {
	t.Parallel()

	// Verify the struct has all required fields by creating an instance
	paths := PresetPaths{
		Ro:      []string{"/readonly/path"},
		Rw:      []string{"/readwrite/path"},
		Exclude: []string{"/excluded/path"},
	}

	if len(paths.Ro) != 1 || paths.Ro[0] != "/readonly/path" {
		t.Error("PresetPaths.Ro field not working correctly")
	}

	if len(paths.Rw) != 1 || paths.Rw[0] != "/readwrite/path" {
		t.Error("PresetPaths.Rw field not working correctly")
	}

	if len(paths.Exclude) != 1 || paths.Exclude[0] != "/excluded/path" {
		t.Error("PresetPaths.Exclude field not working correctly")
	}
}

func Test_PresetContext_Has_All_Required_Fields(t *testing.T) {
	t.Parallel()

	// Verify the struct has all required fields by creating an instance
	ctx := PresetContext{
		HomeDir:           "/home/user",
		WorkDir:           "/home/user/project",
		GitWorktree:       "/home/user/project/.git/worktrees/feature",
		LoadedConfigPaths: []string{"/home/user/.config/agent-sandbox/config.json"},
	}

	if ctx.HomeDir != "/home/user" {
		t.Error("PresetContext.HomeDir field not working correctly")
	}

	if ctx.WorkDir != "/home/user/project" {
		t.Error("PresetContext.WorkDir field not working correctly")
	}

	if ctx.GitWorktree != "/home/user/project/.git/worktrees/feature" {
		t.Error("PresetContext.GitWorktree field not working correctly")
	}

	if len(ctx.LoadedConfigPaths) != 1 || ctx.LoadedConfigPaths[0] != "/home/user/.config/agent-sandbox/config.json" {
		t.Error("PresetContext.LoadedConfigPaths field not working correctly")
	}
}

func Test_Preset_Resolve_Functions_Accept_Context_And_Disabled_Map(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir:           "/home/user",
		WorkDir:           "/home/user/project",
		GitWorktree:       "",
		LoadedConfigPaths: nil,
	}

	disabled := map[string]bool{
		"@lint/python": true,
	}

	// Call all resolve functions to ensure they accept the correct arguments
	// This is a compile-time check that the function signatures match
	for name, preset := range PresetRegistry {
		result := preset.Resolve(ctx, disabled)
		// Stub implementations return empty PresetPaths
		_ = result

		t.Logf("preset %q Resolve function called successfully", name)
	}
}

// sliceContains checks if a string slice contains a specific value.
func sliceContains(slice []string, value string) bool {
	return slices.Contains(slice, value)
}

// ============================================================================
// @base preset tests
// ============================================================================

func Test_BasePreset_Returns_WorkDir_As_Writable(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveBasePreset(ctx, nil)

	if !sliceContains(paths.Rw, "/home/user/project") {
		t.Errorf("@base should include WorkDir in rw paths, got: %v", paths.Rw)
	}
}

func Test_BasePreset_Returns_Tmp_As_Writable(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveBasePreset(ctx, nil)

	if !sliceContains(paths.Rw, "/tmp") {
		t.Errorf("@base should include /tmp in rw paths, got: %v", paths.Rw)
	}
}

func Test_BasePreset_Returns_HomeDir_As_ReadOnly(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveBasePreset(ctx, nil)

	if !sliceContains(paths.Ro, "/home/user") {
		t.Errorf("@base should include HomeDir in ro paths, got: %v", paths.Ro)
	}
}

func Test_BasePreset_Returns_Project_Config_Files_As_ReadOnly(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveBasePreset(ctx, nil)

	if !sliceContains(paths.Ro, "/home/user/project/.agent-sandbox.json") {
		t.Errorf("@base should include .agent-sandbox.json in ro paths, got: %v", paths.Ro)
	}

	if !sliceContains(paths.Ro, "/home/user/project/.agent-sandbox.jsonc") {
		t.Errorf("@base should include .agent-sandbox.jsonc in ro paths, got: %v", paths.Ro)
	}
}

func Test_BasePreset_Returns_LoadedConfigPaths_As_ReadOnly(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
		LoadedConfigPaths: []string{
			"/home/user/.config/agent-sandbox/config.json",
			"/some/custom/config.jsonc",
		},
	}

	paths := resolveBasePreset(ctx, nil)

	if !sliceContains(paths.Ro, "/home/user/.config/agent-sandbox/config.json") {
		t.Errorf("@base should include global config in ro paths, got: %v", paths.Ro)
	}

	if !sliceContains(paths.Ro, "/some/custom/config.jsonc") {
		t.Errorf("@base should include custom config in ro paths, got: %v", paths.Ro)
	}
}

func Test_BasePreset_Excludes_SSH_Directory(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveBasePreset(ctx, nil)

	if !sliceContains(paths.Exclude, "/home/user/.ssh") {
		t.Errorf("@base should exclude ~/.ssh, got: %v", paths.Exclude)
	}
}

func Test_BasePreset_Excludes_GnuPG_Directory(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveBasePreset(ctx, nil)

	if !sliceContains(paths.Exclude, "/home/user/.gnupg") {
		t.Errorf("@base should exclude ~/.gnupg, got: %v", paths.Exclude)
	}
}

func Test_BasePreset_Excludes_AWS_Directory(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveBasePreset(ctx, nil)

	if !sliceContains(paths.Exclude, "/home/user/.aws") {
		t.Errorf("@base should exclude ~/.aws, got: %v", paths.Exclude)
	}
}

func Test_BasePreset_Works_When_WorkDir_Is_Outside_Home(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/opt/project",
	}

	paths := resolveBasePreset(ctx, nil)

	// WorkDir should still be writable
	if !sliceContains(paths.Rw, "/opt/project") {
		t.Errorf("@base should include WorkDir /opt/project in rw paths, got: %v", paths.Rw)
	}

	// Home should still be read-only
	if !sliceContains(paths.Ro, "/home/user") {
		t.Errorf("@base should include HomeDir in ro paths, got: %v", paths.Ro)
	}

	// Project config paths should use the actual WorkDir
	if !sliceContains(paths.Ro, "/opt/project/.agent-sandbox.json") {
		t.Errorf("@base should include .agent-sandbox.json in WorkDir, got: %v", paths.Ro)
	}
}

func Test_BasePreset_Uses_Absolute_Paths(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir:           "/home/user",
		WorkDir:           "/home/user/project",
		LoadedConfigPaths: []string{"/global/config.json"},
	}

	paths := resolveBasePreset(ctx, nil)

	// All paths should be absolute (start with /)
	for _, p := range paths.Rw {
		if p == "" || p[0] != '/' {
			t.Errorf("rw path should be absolute: %q", p)
		}
	}

	for _, p := range paths.Ro {
		if p == "" || p[0] != '/' {
			t.Errorf("ro path should be absolute: %q", p)
		}
	}

	for _, p := range paths.Exclude {
		if p == "" || p[0] != '/' {
			t.Errorf("exclude path should be absolute: %q", p)
		}
	}
}

func Test_BasePreset_Ignores_Disabled_Map(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	// @base is a simple preset, so disabled should have no effect
	disabled := map[string]bool{
		"@base": true, // Even if @base is in disabled, it still resolves
	}

	paths := resolveBasePreset(ctx, disabled)

	// Should still return all paths
	if len(paths.Rw) == 0 {
		t.Error("@base should return rw paths even with disabled map")
	}

	if len(paths.Ro) == 0 {
		t.Error("@base should return ro paths even with disabled map")
	}

	if len(paths.Exclude) == 0 {
		t.Error("@base should return exclude paths even with disabled map")
	}
}

func Test_BasePreset_Handles_Empty_LoadedConfigPaths(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir:           "/home/user",
		WorkDir:           "/home/user/project",
		LoadedConfigPaths: nil, // No configs loaded
	}

	paths := resolveBasePreset(ctx, nil)

	// Should still have at least home + project config file paths
	if len(paths.Ro) < 3 {
		t.Errorf("@base should have at least 3 ro paths (home + 2 project configs), got: %v", paths.Ro)
	}
}

func Test_BasePreset_Returns_All_Three_Secret_Directories(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/testuser",
		WorkDir: "/home/testuser/myproject",
	}

	paths := resolveBasePreset(ctx, nil)

	expectedExcludes := []string{
		"/home/testuser/.ssh",
		"/home/testuser/.gnupg",
		"/home/testuser/.aws",
	}

	if len(paths.Exclude) != len(expectedExcludes) {
		t.Errorf("@base should have exactly %d exclude paths, got %d: %v",
			len(expectedExcludes), len(paths.Exclude), paths.Exclude)
	}

	for _, expected := range expectedExcludes {
		if !sliceContains(paths.Exclude, expected) {
			t.Errorf("@base should exclude %q, got: %v", expected, paths.Exclude)
		}
	}
}
