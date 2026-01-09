package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Note: exec import removed - git commands use GitRepo helper from testing_test.go

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

// ============================================================================
// @caches preset tests
// ============================================================================

func Test_CachesPreset_Returns_XDG_Cache_As_Writable(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveCachesPreset(ctx, nil)

	if !sliceContains(paths.Rw, "/home/user/.cache") {
		t.Errorf("@caches should include ~/.cache in rw paths, got: %v", paths.Rw)
	}
}

func Test_CachesPreset_Returns_Bun_Cache_As_Writable(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveCachesPreset(ctx, nil)

	if !sliceContains(paths.Rw, "/home/user/.bun") {
		t.Errorf("@caches should include ~/.bun in rw paths, got: %v", paths.Rw)
	}
}

func Test_CachesPreset_Returns_Go_Path_As_Writable(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveCachesPreset(ctx, nil)

	if !sliceContains(paths.Rw, "/home/user/go") {
		t.Errorf("@caches should include ~/go in rw paths, got: %v", paths.Rw)
	}
}

func Test_CachesPreset_Returns_Npm_Cache_As_Writable(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveCachesPreset(ctx, nil)

	if !sliceContains(paths.Rw, "/home/user/.npm") {
		t.Errorf("@caches should include ~/.npm in rw paths, got: %v", paths.Rw)
	}
}

func Test_CachesPreset_Returns_Cargo_Cache_As_Writable(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveCachesPreset(ctx, nil)

	if !sliceContains(paths.Rw, "/home/user/.cargo") {
		t.Errorf("@caches should include ~/.cargo in rw paths, got: %v", paths.Rw)
	}
}

func Test_CachesPreset_Returns_All_Cache_Directories(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/testuser",
		WorkDir: "/home/testuser/myproject",
	}

	paths := resolveCachesPreset(ctx, nil)

	expectedRw := []string{
		"/home/testuser/.cache",
		"/home/testuser/.bun",
		"/home/testuser/go",
		"/home/testuser/.npm",
		"/home/testuser/.cargo",
	}

	if len(paths.Rw) != len(expectedRw) {
		t.Errorf("@caches should have exactly %d rw paths, got %d: %v",
			len(expectedRw), len(paths.Rw), paths.Rw)
	}

	for _, expected := range expectedRw {
		if !sliceContains(paths.Rw, expected) {
			t.Errorf("@caches should include %q in rw paths, got: %v", expected, paths.Rw)
		}
	}
}

func Test_CachesPreset_Returns_No_ReadOnly_Paths(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveCachesPreset(ctx, nil)

	if len(paths.Ro) != 0 {
		t.Errorf("@caches should not return any ro paths, got: %v", paths.Ro)
	}
}

func Test_CachesPreset_Returns_No_Excluded_Paths(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveCachesPreset(ctx, nil)

	if len(paths.Exclude) != 0 {
		t.Errorf("@caches should not return any exclude paths, got: %v", paths.Exclude)
	}
}

func Test_CachesPreset_Uses_Absolute_Paths(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveCachesPreset(ctx, nil)

	for _, p := range paths.Rw {
		if p == "" || p[0] != '/' {
			t.Errorf("rw path should be absolute: %q", p)
		}
	}
}

func Test_CachesPreset_Ignores_Disabled_Map(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	// @caches is a simple preset, so disabled should have no effect
	disabled := map[string]bool{
		"@caches": true,
	}

	paths := resolveCachesPreset(ctx, disabled)

	// Should still return all paths
	if len(paths.Rw) == 0 {
		t.Error("@caches should return rw paths even with disabled map")
	}
}

func Test_CachesPreset_Works_With_Different_Home_Dir(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/users/alice",
		WorkDir: "/users/alice/project",
	}

	paths := resolveCachesPreset(ctx, nil)

	if !sliceContains(paths.Rw, "/users/alice/.cache") {
		t.Errorf("@caches should use correct home dir, got: %v", paths.Rw)
	}

	if !sliceContains(paths.Rw, "/users/alice/go") {
		t.Errorf("@caches should use correct home dir for go, got: %v", paths.Rw)
	}
}

// ============================================================================
// @git preset tests
// ============================================================================

func Test_GitPreset_Returns_Hooks_And_Config_For_Normal_Repo(t *testing.T) {
	t.Parallel()

	// Create a temp directory with a .git directory
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")

	err := os.MkdirAll(filepath.Join(gitDir, "hooks"), 0o750)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: tmpDir,
	}

	paths := resolveGitPreset(ctx, nil)

	expectedHooks := filepath.Join(gitDir, "hooks")
	expectedConfig := filepath.Join(gitDir, "config")

	if !sliceContains(paths.Ro, expectedHooks) {
		t.Errorf("@git should include .git/hooks in ro paths, got: %v", paths.Ro)
	}

	if !sliceContains(paths.Ro, expectedConfig) {
		t.Errorf("@git should include .git/config in ro paths, got: %v", paths.Ro)
	}
}

func Test_GitPreset_Returns_Empty_For_Non_Git_Directory(t *testing.T) {
	t.Parallel()

	// Create a temp directory without .git
	tmpDir := t.TempDir()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: tmpDir,
	}

	paths := resolveGitPreset(ctx, nil)

	if len(paths.Ro) != 0 {
		t.Errorf("@git should return empty ro paths for non-git directory, got: %v", paths.Ro)
	}

	if len(paths.Rw) != 0 {
		t.Errorf("@git should return empty rw paths for non-git directory, got: %v", paths.Rw)
	}

	if len(paths.Exclude) != 0 {
		t.Errorf("@git should return empty exclude paths for non-git directory, got: %v", paths.Exclude)
	}
}

func Test_GitPreset_Parses_Worktree_Gitdir_Line(t *testing.T) {
	t.Parallel()

	// Create main repo structure
	tmpDir := t.TempDir()
	mainRepoDir := filepath.Join(tmpDir, "main-repo")
	mainGitDir := filepath.Join(mainRepoDir, ".git")

	// Create main repo .git directory structure
	err := os.MkdirAll(filepath.Join(mainGitDir, "hooks"), 0o750)
	if err != nil {
		t.Fatal(err)
	}

	err = os.MkdirAll(filepath.Join(mainGitDir, "worktrees", "feature-branch"), 0o750)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(mainGitDir, "config"), []byte("[core]\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	// Create worktree gitdir structure
	worktreeGitDir := filepath.Join(mainGitDir, "worktrees", "feature-branch")

	err = os.WriteFile(filepath.Join(worktreeGitDir, "config"), []byte("[worktree]\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	// commondir points back to main .git directory
	err = os.WriteFile(filepath.Join(worktreeGitDir, "commondir"), []byte("../.."), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	// Create worktree directory with .git file
	worktreeDir := filepath.Join(tmpDir, "worktree")

	err = os.MkdirAll(worktreeDir, 0o750)
	if err != nil {
		t.Fatal(err)
	}

	// .git file in worktree points to worktree gitdir
	gitFile := filepath.Join(worktreeDir, ".git")
	gitdirContent := "gitdir: " + worktreeGitDir + "\n"

	err = os.WriteFile(gitFile, []byte(gitdirContent), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: worktreeDir,
	}

	paths := resolveGitPreset(ctx, nil)

	// Should have 4 ro paths: worktree hooks/config + main hooks/config
	if len(paths.Ro) != 4 {
		t.Errorf("@git for worktree should have 4 ro paths, got %d: %v", len(paths.Ro), paths.Ro)
	}
}

func Test_GitPreset_Protects_Worktree_Hooks_And_Config(t *testing.T) {
	t.Parallel()

	// Create main repo structure
	tmpDir := t.TempDir()
	mainRepoDir := filepath.Join(tmpDir, "main-repo")
	mainGitDir := filepath.Join(mainRepoDir, ".git")

	// Create main repo .git directory structure
	err := os.MkdirAll(filepath.Join(mainGitDir, "hooks"), 0o750)
	if err != nil {
		t.Fatal(err)
	}

	worktreeGitDir := filepath.Join(mainGitDir, "worktrees", "feature")

	err = os.MkdirAll(filepath.Join(worktreeGitDir, "hooks"), 0o750)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(mainGitDir, "config"), []byte("[core]\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(worktreeGitDir, "config"), []byte("[worktree]\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(worktreeGitDir, "commondir"), []byte("../.."), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	// Create worktree directory with .git file
	worktreeDir := filepath.Join(tmpDir, "worktree")

	err = os.MkdirAll(worktreeDir, 0o750)
	if err != nil {
		t.Fatal(err)
	}

	gitFile := filepath.Join(worktreeDir, ".git")

	err = os.WriteFile(gitFile, []byte("gitdir: "+worktreeGitDir+"\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: worktreeDir,
	}

	paths := resolveGitPreset(ctx, nil)

	expectedWorktreeHooks := filepath.Join(worktreeGitDir, "hooks")
	expectedWorktreeConfig := filepath.Join(worktreeGitDir, "config")

	if !sliceContains(paths.Ro, expectedWorktreeHooks) {
		t.Errorf("@git should include worktree hooks in ro paths, got: %v", paths.Ro)
	}

	if !sliceContains(paths.Ro, expectedWorktreeConfig) {
		t.Errorf("@git should include worktree config in ro paths, got: %v", paths.Ro)
	}
}

func Test_GitPreset_Protects_Main_Repo_Hooks_And_Config_From_Worktree(t *testing.T) {
	t.Parallel()

	// Create main repo structure
	tmpDir := t.TempDir()
	mainRepoDir := filepath.Join(tmpDir, "main-repo")
	mainGitDir := filepath.Join(mainRepoDir, ".git")

	// Create main repo .git directory structure
	err := os.MkdirAll(filepath.Join(mainGitDir, "hooks"), 0o750)
	if err != nil {
		t.Fatal(err)
	}

	worktreeGitDir := filepath.Join(mainGitDir, "worktrees", "feature")

	err = os.MkdirAll(worktreeGitDir, 0o750)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(mainGitDir, "config"), []byte("[core]\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(worktreeGitDir, "config"), []byte("[worktree]\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(worktreeGitDir, "commondir"), []byte("../.."), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	// Create worktree directory with .git file
	worktreeDir := filepath.Join(tmpDir, "worktree")

	err = os.MkdirAll(worktreeDir, 0o750)
	if err != nil {
		t.Fatal(err)
	}

	gitFile := filepath.Join(worktreeDir, ".git")

	err = os.WriteFile(gitFile, []byte("gitdir: "+worktreeGitDir+"\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: worktreeDir,
	}

	paths := resolveGitPreset(ctx, nil)

	expectedMainHooks := filepath.Join(mainGitDir, "hooks")
	expectedMainConfig := filepath.Join(mainGitDir, "config")

	if !sliceContains(paths.Ro, expectedMainHooks) {
		t.Errorf("@git should include main repo hooks in ro paths, got: %v", paths.Ro)
	}

	if !sliceContains(paths.Ro, expectedMainConfig) {
		t.Errorf("@git should include main repo config in ro paths, got: %v", paths.Ro)
	}
}

func Test_GitPreset_Returns_Error_For_Invalid_Git_File_Format(t *testing.T) {
	t.Parallel()

	// Create a temp directory with an invalid .git file
	tmpDir := t.TempDir()
	gitFile := filepath.Join(tmpDir, ".git")

	// Write invalid content (not starting with "gitdir: ")
	err := os.WriteFile(gitFile, []byte("invalid content"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	result, err := resolveGitPaths(tmpDir)
	if err == nil {
		t.Errorf("resolveGitPaths should return error for invalid .git file format")
	}

	// Result should be empty GitPaths on error
	if result.Hooks != "" || result.Config != "" || result.MainHooks != "" || result.MainConfig != "" {
		t.Errorf("resolveGitPaths should return empty GitPaths for invalid .git file, got: %+v", result)
	}

	if !strings.Contains(err.Error(), "invalid .git file format") {
		t.Errorf("error should mention invalid format, got: %v", err)
	}
}

func Test_GitPreset_Ignores_Disabled_Map(t *testing.T) {
	t.Parallel()

	// Create a temp directory with a .git directory
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")

	err := os.MkdirAll(filepath.Join(gitDir, "hooks"), 0o750)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: tmpDir,
	}

	// @git is a simple preset, so disabled should have no effect
	disabled := map[string]bool{
		"@git": true,
	}

	paths := resolveGitPreset(ctx, disabled)

	// Should still return paths
	if len(paths.Ro) == 0 {
		t.Error("@git should return ro paths even with disabled map")
	}
}

func Test_GitPreset_Returns_No_RW_Or_Exclude_Paths(t *testing.T) {
	t.Parallel()

	// Create a temp directory with a .git directory
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")

	err := os.MkdirAll(filepath.Join(gitDir, "hooks"), 0o750)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: tmpDir,
	}

	paths := resolveGitPreset(ctx, nil)

	if len(paths.Rw) != 0 {
		t.Errorf("@git should not return any rw paths, got: %v", paths.Rw)
	}

	if len(paths.Exclude) != 0 {
		t.Errorf("@git should not return any exclude paths, got: %v", paths.Exclude)
	}
}

func Test_GitPreset_Uses_Absolute_Paths(t *testing.T) {
	t.Parallel()

	// Create a temp directory with a .git directory
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")

	err := os.MkdirAll(filepath.Join(gitDir, "hooks"), 0o750)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: tmpDir,
	}

	paths := resolveGitPreset(ctx, nil)

	for _, p := range paths.Ro {
		if p == "" || p[0] != '/' {
			t.Errorf("ro path should be absolute: %q", p)
		}
	}
}

func Test_GitPreset_Handles_Relative_Gitdir_Path(t *testing.T) {
	t.Parallel()

	// Create main repo structure
	tmpDir := t.TempDir()
	mainRepoDir := filepath.Join(tmpDir, "main-repo")
	mainGitDir := filepath.Join(mainRepoDir, ".git")

	err := os.MkdirAll(filepath.Join(mainGitDir, "hooks"), 0o750)
	if err != nil {
		t.Fatal(err)
	}

	worktreeGitDir := filepath.Join(mainGitDir, "worktrees", "feature")

	err = os.MkdirAll(worktreeGitDir, 0o750)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(mainGitDir, "config"), []byte("[core]\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(worktreeGitDir, "config"), []byte("[worktree]\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(worktreeGitDir, "commondir"), []byte("../.."), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	// Create worktree directory with .git file using RELATIVE path
	worktreeDir := filepath.Join(tmpDir, "worktree")

	err = os.MkdirAll(worktreeDir, 0o750)
	if err != nil {
		t.Fatal(err)
	}

	// Use relative path in gitdir line
	relativePath := "../main-repo/.git/worktrees/feature"
	gitFile := filepath.Join(worktreeDir, ".git")

	err = os.WriteFile(gitFile, []byte("gitdir: "+relativePath+"\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: worktreeDir,
	}

	paths := resolveGitPreset(ctx, nil)

	// Should still resolve correctly
	if len(paths.Ro) != 4 {
		t.Errorf("@git with relative gitdir should have 4 ro paths, got %d: %v", len(paths.Ro), paths.Ro)
	}

	// Verify paths are absolute
	for _, p := range paths.Ro {
		if p == "" || p[0] != '/' {
			t.Errorf("ro path should be absolute even with relative gitdir: %q", p)
		}
	}
}

func Test_GitPreset_Handles_Missing_Commondir(t *testing.T) {
	t.Parallel()

	// Create main repo structure
	tmpDir := t.TempDir()
	mainRepoDir := filepath.Join(tmpDir, "main-repo")
	mainGitDir := filepath.Join(mainRepoDir, ".git")

	worktreeGitDir := filepath.Join(mainGitDir, "worktrees", "feature")

	err := os.MkdirAll(worktreeGitDir, 0o750)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(worktreeGitDir, "config"), []byte("[worktree]\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	// Note: No commondir file

	// Create worktree directory with .git file
	worktreeDir := filepath.Join(tmpDir, "worktree")

	err = os.MkdirAll(worktreeDir, 0o750)
	if err != nil {
		t.Fatal(err)
	}

	gitFile := filepath.Join(worktreeDir, ".git")

	err = os.WriteFile(gitFile, []byte("gitdir: "+worktreeGitDir+"\n"), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: worktreeDir,
	}

	paths := resolveGitPreset(ctx, nil)

	// Should only have 2 ro paths (worktree hooks/config, no main repo)
	if len(paths.Ro) != 2 {
		t.Errorf("@git without commondir should have 2 ro paths, got %d: %v", len(paths.Ro), paths.Ro)
	}

	// Verify worktree paths are present
	expectedWorktreeHooks := filepath.Join(worktreeGitDir, "hooks")
	expectedWorktreeConfig := filepath.Join(worktreeGitDir, "config")

	if !sliceContains(paths.Ro, expectedWorktreeHooks) {
		t.Errorf("@git should include worktree hooks in ro paths, got: %v", paths.Ro)
	}

	if !sliceContains(paths.Ro, expectedWorktreeConfig) {
		t.Errorf("@git should include worktree config in ro paths, got: %v", paths.Ro)
	}
}

// ============================================================================
// @git preset integration tests with real git
// ============================================================================

func Test_GitPreset_Integration_Real_Git_Init(t *testing.T) {
	t.Parallel()

	// Create a real git repository
	repo := NewGitRepo(t)

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: repo.Dir,
	}

	paths := resolveGitPreset(ctx, nil)

	// Should have 2 ro paths for a normal git repo
	if len(paths.Ro) != 2 {
		t.Errorf("@git for real git repo should have 2 ro paths, got %d: %v", len(paths.Ro), paths.Ro)
	}

	expectedHooks := filepath.Join(repo.Dir, ".git", "hooks")
	expectedConfig := filepath.Join(repo.Dir, ".git", "config")

	if !sliceContains(paths.Ro, expectedHooks) {
		t.Errorf("@git should include .git/hooks in ro paths, got: %v", paths.Ro)
	}

	if !sliceContains(paths.Ro, expectedConfig) {
		t.Errorf("@git should include .git/config in ro paths, got: %v", paths.Ro)
	}
}

func Test_GitPreset_Integration_Real_Git_Worktree(t *testing.T) {
	t.Parallel()

	// Create a real git repository with an initial commit
	repo := NewGitRepo(t)
	repo.WriteFile("README.md", "# Test")
	repo.Commit("Initial commit")

	// Create a worktree
	worktreeDir := filepath.Join(t.TempDir(), "worktree")
	repo.AddWorktree(worktreeDir, "feature-branch")

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: worktreeDir,
	}

	paths := resolveGitPreset(ctx, nil)

	// Should have 4 ro paths for a worktree (worktree + main repo)
	if len(paths.Ro) != 4 {
		t.Errorf("@git for real worktree should have 4 ro paths, got %d: %v", len(paths.Ro), paths.Ro)
	}

	// Verify main repo paths are included
	mainGitDir := filepath.Join(repo.Dir, ".git")
	expectedMainHooks := filepath.Join(mainGitDir, "hooks")
	expectedMainConfig := filepath.Join(mainGitDir, "config")

	if !sliceContains(paths.Ro, expectedMainHooks) {
		t.Errorf("@git should include main repo hooks in ro paths, got: %v", paths.Ro)
	}

	if !sliceContains(paths.Ro, expectedMainConfig) {
		t.Errorf("@git should include main repo config in ro paths, got: %v", paths.Ro)
	}
}
