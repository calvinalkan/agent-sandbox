package main

import (
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
