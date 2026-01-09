package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	testHomeUser        = "/home/user"
	testHomeUserWorkDir = "/home/user/project"
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
		HomeDir:           testHomeUser,
		WorkDir:           testHomeUserWorkDir,
		GitWorktree:       testHomeUserWorkDir + "/.git/worktrees/feature",
		LoadedConfigPaths: []string{testHomeUser + "/.config/agent-sandbox/config.json"},
	}

	if ctx.HomeDir != testHomeUser {
		t.Error("PresetContext.HomeDir field not working correctly")
	}

	if ctx.WorkDir != testHomeUserWorkDir {
		t.Error("PresetContext.WorkDir field not working correctly")
	}

	if ctx.GitWorktree != testHomeUserWorkDir+"/.git/worktrees/feature" {
		t.Error("PresetContext.GitWorktree field not working correctly")
	}

	if len(ctx.LoadedConfigPaths) != 1 || ctx.LoadedConfigPaths[0] != testHomeUser+"/.config/agent-sandbox/config.json" {
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

// ============================================================================
// @lint/ts preset tests
// ============================================================================

func Test_LintTSPreset_Returns_Biome_Configs_As_ReadOnly_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintTSPreset(ctx, nil)

	if !sliceContains(paths.Ro, "/home/user/project/biome.json") {
		t.Errorf("@lint/ts should include biome.json in ro paths, got: %v", paths.Ro)
	}

	if !sliceContains(paths.Ro, "/home/user/project/biome.jsonc") {
		t.Errorf("@lint/ts should include biome.jsonc in ro paths, got: %v", paths.Ro)
	}
}

func Test_LintTSPreset_Returns_ESLint_Legacy_Configs_As_ReadOnly_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintTSPreset(ctx, nil)

	expectedFiles := []string{
		"/home/user/project/.eslintrc",
		"/home/user/project/.eslintrc.js",
		"/home/user/project/.eslintrc.json",
		"/home/user/project/.eslintrc.yml",
		"/home/user/project/.eslintrc.yaml",
	}

	for _, expected := range expectedFiles {
		if !sliceContains(paths.Ro, expected) {
			t.Errorf("@lint/ts should include %q in ro paths, got: %v", expected, paths.Ro)
		}
	}
}

func Test_LintTSPreset_Returns_ESLint_Flat_Configs_As_ReadOnly_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintTSPreset(ctx, nil)

	expectedFiles := []string{
		"/home/user/project/eslint.config.js",
		"/home/user/project/eslint.config.mjs",
		"/home/user/project/eslint.config.cjs",
	}

	for _, expected := range expectedFiles {
		if !sliceContains(paths.Ro, expected) {
			t.Errorf("@lint/ts should include %q in ro paths, got: %v", expected, paths.Ro)
		}
	}
}

func Test_LintTSPreset_Returns_Prettier_Configs_As_ReadOnly_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintTSPreset(ctx, nil)

	expectedFiles := []string{
		"/home/user/project/.prettierrc",
		"/home/user/project/.prettierrc.js",
		"/home/user/project/.prettierrc.json",
		"/home/user/project/.prettierrc.yml",
		"/home/user/project/prettier.config.js",
	}

	for _, expected := range expectedFiles {
		if !sliceContains(paths.Ro, expected) {
			t.Errorf("@lint/ts should include %q in ro paths, got: %v", expected, paths.Ro)
		}
	}
}

func Test_LintTSPreset_Returns_TypeScript_Configs_As_ReadOnly_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintTSPreset(ctx, nil)

	// tsconfig.json and glob pattern for tsconfig.*.json
	if !sliceContains(paths.Ro, "/home/user/project/tsconfig.json") {
		t.Errorf("@lint/ts should include tsconfig.json in ro paths, got: %v", paths.Ro)
	}

	if !sliceContains(paths.Ro, "/home/user/project/tsconfig.*.json") {
		t.Errorf("@lint/ts should include tsconfig.*.json glob in ro paths, got: %v", paths.Ro)
	}
}

func Test_LintTSPreset_Returns_EditorConfig_As_ReadOnly_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintTSPreset(ctx, nil)

	if !sliceContains(paths.Ro, "/home/user/project/.editorconfig") {
		t.Errorf("@lint/ts should include .editorconfig in ro paths, got: %v", paths.Ro)
	}
}

func Test_LintTSPreset_Returns_No_RW_Or_Exclude_Paths_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintTSPreset(ctx, nil)

	if len(paths.Rw) != 0 {
		t.Errorf("@lint/ts should not return any rw paths, got: %v", paths.Rw)
	}

	if len(paths.Exclude) != 0 {
		t.Errorf("@lint/ts should not return any exclude paths, got: %v", paths.Exclude)
	}
}

func Test_LintTSPreset_Ignores_Disabled_Map_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	disabled := map[string]bool{
		"@lint/ts": true,
	}

	paths := resolveLintTSPreset(ctx, disabled)

	// Simple presets ignore disabled map
	if len(paths.Ro) == 0 {
		t.Error("@lint/ts should return ro paths even with disabled map")
	}
}

func Test_LintTSPreset_Uses_Absolute_Paths_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintTSPreset(ctx, nil)

	for _, p := range paths.Ro {
		if p == "" || p[0] != '/' {
			t.Errorf("ro path should be absolute: %q", p)
		}
	}
}

// ============================================================================
// @lint/go preset tests
// ============================================================================

func Test_LintGoPreset_Returns_Golangci_Configs_As_ReadOnly_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintGoPreset(ctx, nil)

	expectedFiles := []string{
		"/home/user/project/.golangci.yml",
		"/home/user/project/.golangci.yaml",
		"/home/user/project/.golangci.toml",
		"/home/user/project/.golangci.json",
	}

	for _, expected := range expectedFiles {
		if !sliceContains(paths.Ro, expected) {
			t.Errorf("@lint/go should include %q in ro paths, got: %v", expected, paths.Ro)
		}
	}
}

func Test_LintGoPreset_Returns_EditorConfig_As_ReadOnly_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintGoPreset(ctx, nil)

	if !sliceContains(paths.Ro, "/home/user/project/.editorconfig") {
		t.Errorf("@lint/go should include .editorconfig in ro paths, got: %v", paths.Ro)
	}
}

func Test_LintGoPreset_Returns_No_RW_Or_Exclude_Paths_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintGoPreset(ctx, nil)

	if len(paths.Rw) != 0 {
		t.Errorf("@lint/go should not return any rw paths, got: %v", paths.Rw)
	}

	if len(paths.Exclude) != 0 {
		t.Errorf("@lint/go should not return any exclude paths, got: %v", paths.Exclude)
	}
}

func Test_LintGoPreset_Ignores_Disabled_Map_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	disabled := map[string]bool{
		"@lint/go": true,
	}

	paths := resolveLintGoPreset(ctx, disabled)

	// Simple presets ignore disabled map
	if len(paths.Ro) == 0 {
		t.Error("@lint/go should return ro paths even with disabled map")
	}
}

func Test_LintGoPreset_Uses_Absolute_Paths_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintGoPreset(ctx, nil)

	for _, p := range paths.Ro {
		if p == "" || p[0] != '/' {
			t.Errorf("ro path should be absolute: %q", p)
		}
	}
}

func Test_LintGoPreset_Returns_All_Expected_Configs_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/testuser",
		WorkDir: "/home/testuser/myproject",
	}

	paths := resolveLintGoPreset(ctx, nil)

	expectedRo := []string{
		"/home/testuser/myproject/.golangci.yml",
		"/home/testuser/myproject/.golangci.yaml",
		"/home/testuser/myproject/.golangci.toml",
		"/home/testuser/myproject/.golangci.json",
		"/home/testuser/myproject/.editorconfig",
	}

	if len(paths.Ro) != len(expectedRo) {
		t.Errorf("@lint/go should have exactly %d ro paths, got %d: %v",
			len(expectedRo), len(paths.Ro), paths.Ro)
	}

	for _, expected := range expectedRo {
		if !sliceContains(paths.Ro, expected) {
			t.Errorf("@lint/go should include %q in ro paths, got: %v", expected, paths.Ro)
		}
	}
}

// ============================================================================
// @lint/python preset tests
// ============================================================================

func Test_LintPythonPreset_Returns_Pyproject_As_ReadOnly_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintPythonPreset(ctx, nil)

	if !sliceContains(paths.Ro, "/home/user/project/pyproject.toml") {
		t.Errorf("@lint/python should include pyproject.toml in ro paths, got: %v", paths.Ro)
	}
}

func Test_LintPythonPreset_Returns_SetupCfg_As_ReadOnly_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintPythonPreset(ctx, nil)

	if !sliceContains(paths.Ro, "/home/user/project/setup.cfg") {
		t.Errorf("@lint/python should include setup.cfg in ro paths, got: %v", paths.Ro)
	}
}

func Test_LintPythonPreset_Returns_Flake8_Config_As_ReadOnly_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintPythonPreset(ctx, nil)

	if !sliceContains(paths.Ro, "/home/user/project/.flake8") {
		t.Errorf("@lint/python should include .flake8 in ro paths, got: %v", paths.Ro)
	}
}

func Test_LintPythonPreset_Returns_Mypy_Configs_As_ReadOnly_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintPythonPreset(ctx, nil)

	expectedFiles := []string{
		"/home/user/project/mypy.ini",
		"/home/user/project/.mypy.ini",
	}

	for _, expected := range expectedFiles {
		if !sliceContains(paths.Ro, expected) {
			t.Errorf("@lint/python should include %q in ro paths, got: %v", expected, paths.Ro)
		}
	}
}

func Test_LintPythonPreset_Returns_Pylint_Configs_As_ReadOnly_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintPythonPreset(ctx, nil)

	expectedFiles := []string{
		"/home/user/project/.pylintrc",
		"/home/user/project/pylintrc",
	}

	for _, expected := range expectedFiles {
		if !sliceContains(paths.Ro, expected) {
			t.Errorf("@lint/python should include %q in ro paths, got: %v", expected, paths.Ro)
		}
	}
}

func Test_LintPythonPreset_Returns_Ruff_Configs_As_ReadOnly_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintPythonPreset(ctx, nil)

	expectedFiles := []string{
		"/home/user/project/ruff.toml",
		"/home/user/project/.ruff.toml",
	}

	for _, expected := range expectedFiles {
		if !sliceContains(paths.Ro, expected) {
			t.Errorf("@lint/python should include %q in ro paths, got: %v", expected, paths.Ro)
		}
	}
}

func Test_LintPythonPreset_Returns_EditorConfig_As_ReadOnly_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintPythonPreset(ctx, nil)

	if !sliceContains(paths.Ro, "/home/user/project/.editorconfig") {
		t.Errorf("@lint/python should include .editorconfig in ro paths, got: %v", paths.Ro)
	}
}

func Test_LintPythonPreset_Returns_No_RW_Or_Exclude_Paths_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintPythonPreset(ctx, nil)

	if len(paths.Rw) != 0 {
		t.Errorf("@lint/python should not return any rw paths, got: %v", paths.Rw)
	}

	if len(paths.Exclude) != 0 {
		t.Errorf("@lint/python should not return any exclude paths, got: %v", paths.Exclude)
	}
}

func Test_LintPythonPreset_Ignores_Disabled_Map_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	disabled := map[string]bool{
		"@lint/python": true,
	}

	paths := resolveLintPythonPreset(ctx, disabled)

	// Simple presets ignore disabled map
	if len(paths.Ro) == 0 {
		t.Error("@lint/python should return ro paths even with disabled map")
	}
}

func Test_LintPythonPreset_Uses_Absolute_Paths_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintPythonPreset(ctx, nil)

	for _, p := range paths.Ro {
		if p == "" || p[0] != '/' {
			t.Errorf("ro path should be absolute: %q", p)
		}
	}
}

func Test_LintPythonPreset_Returns_All_Expected_Configs_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/testuser",
		WorkDir: "/home/testuser/myproject",
	}

	paths := resolveLintPythonPreset(ctx, nil)

	expectedRo := []string{
		"/home/testuser/myproject/pyproject.toml",
		"/home/testuser/myproject/setup.cfg",
		"/home/testuser/myproject/.flake8",
		"/home/testuser/myproject/mypy.ini",
		"/home/testuser/myproject/.mypy.ini",
		"/home/testuser/myproject/.pylintrc",
		"/home/testuser/myproject/pylintrc",
		"/home/testuser/myproject/ruff.toml",
		"/home/testuser/myproject/.ruff.toml",
		"/home/testuser/myproject/.editorconfig",
	}

	if len(paths.Ro) != len(expectedRo) {
		t.Errorf("@lint/python should have exactly %d ro paths, got %d: %v",
			len(expectedRo), len(paths.Ro), paths.Ro)
	}

	for _, expected := range expectedRo {
		if !sliceContains(paths.Ro, expected) {
			t.Errorf("@lint/python should include %q in ro paths, got: %v", expected, paths.Ro)
		}
	}
}

// ============================================================================
// @lint/all preset tests
// ============================================================================

func Test_LintAllPreset_Combines_All_Lint_Presets_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintAllPreset(ctx, nil)

	// Should include configs from all lint presets
	// From @lint/ts
	if !sliceContains(paths.Ro, "/home/user/project/biome.json") {
		t.Errorf("@lint/all should include biome.json from @lint/ts, got: %v", paths.Ro)
	}

	// From @lint/go
	if !sliceContains(paths.Ro, "/home/user/project/.golangci.yml") {
		t.Errorf("@lint/all should include .golangci.yml from @lint/go, got: %v", paths.Ro)
	}

	// From @lint/python
	if !sliceContains(paths.Ro, "/home/user/project/pyproject.toml") {
		t.Errorf("@lint/all should include pyproject.toml from @lint/python, got: %v", paths.Ro)
	}
}

func Test_LintAllPreset_Respects_Disabled_LintTS_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	disabled := map[string]bool{
		"@lint/ts": true,
	}

	paths := resolveLintAllPreset(ctx, disabled)

	// Should NOT include configs from @lint/ts
	if sliceContains(paths.Ro, "/home/user/project/biome.json") {
		t.Errorf("@lint/all should NOT include biome.json when @lint/ts is disabled, got: %v", paths.Ro)
	}

	// Should still include configs from @lint/go and @lint/python
	if !sliceContains(paths.Ro, "/home/user/project/.golangci.yml") {
		t.Errorf("@lint/all should include .golangci.yml from @lint/go, got: %v", paths.Ro)
	}

	if !sliceContains(paths.Ro, "/home/user/project/pyproject.toml") {
		t.Errorf("@lint/all should include pyproject.toml from @lint/python, got: %v", paths.Ro)
	}
}

func Test_LintAllPreset_Respects_Disabled_LintGo_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	disabled := map[string]bool{
		"@lint/go": true,
	}

	paths := resolveLintAllPreset(ctx, disabled)

	// Should NOT include configs from @lint/go
	if sliceContains(paths.Ro, "/home/user/project/.golangci.yml") {
		t.Errorf("@lint/all should NOT include .golangci.yml when @lint/go is disabled, got: %v", paths.Ro)
	}

	// Should still include configs from @lint/ts and @lint/python
	if !sliceContains(paths.Ro, "/home/user/project/biome.json") {
		t.Errorf("@lint/all should include biome.json from @lint/ts, got: %v", paths.Ro)
	}

	if !sliceContains(paths.Ro, "/home/user/project/pyproject.toml") {
		t.Errorf("@lint/all should include pyproject.toml from @lint/python, got: %v", paths.Ro)
	}
}

func Test_LintAllPreset_Respects_Disabled_LintPython_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	disabled := map[string]bool{
		"@lint/python": true,
	}

	paths := resolveLintAllPreset(ctx, disabled)

	// Should NOT include configs from @lint/python
	if sliceContains(paths.Ro, "/home/user/project/pyproject.toml") {
		t.Errorf("@lint/all should NOT include pyproject.toml when @lint/python is disabled, got: %v", paths.Ro)
	}

	// Should still include configs from @lint/ts and @lint/go
	if !sliceContains(paths.Ro, "/home/user/project/biome.json") {
		t.Errorf("@lint/all should include biome.json from @lint/ts, got: %v", paths.Ro)
	}

	if !sliceContains(paths.Ro, "/home/user/project/.golangci.yml") {
		t.Errorf("@lint/all should include .golangci.yml from @lint/go, got: %v", paths.Ro)
	}
}

func Test_LintAllPreset_Returns_Empty_When_All_Disabled(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	disabled := map[string]bool{
		"@lint/ts":     true,
		"@lint/go":     true,
		"@lint/python": true,
	}

	paths := resolveLintAllPreset(ctx, disabled)

	if len(paths.Ro) != 0 {
		t.Errorf("@lint/all should return empty ro paths when all lint presets are disabled, got: %v", paths.Ro)
	}

	if len(paths.Rw) != 0 {
		t.Errorf("@lint/all should return empty rw paths when all lint presets are disabled, got: %v", paths.Rw)
	}

	if len(paths.Exclude) != 0 {
		t.Errorf("@lint/all should return empty exclude paths when all lint presets are disabled, got: %v", paths.Exclude)
	}
}

func Test_LintAllPreset_Returns_No_RW_Or_Exclude_Paths_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintAllPreset(ctx, nil)

	if len(paths.Rw) != 0 {
		t.Errorf("@lint/all should not return any rw paths, got: %v", paths.Rw)
	}

	if len(paths.Exclude) != 0 {
		t.Errorf("@lint/all should not return any exclude paths, got: %v", paths.Exclude)
	}
}

func Test_LintAllPreset_Uses_Absolute_Paths_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveLintAllPreset(ctx, nil)

	for _, p := range paths.Ro {
		if p == "" || p[0] != '/' {
			t.Errorf("ro path should be absolute: %q", p)
		}
	}
}

// ============================================================================
// @all preset tests
// ============================================================================

func Test_AllPreset_Combines_All_Presets_When_Called(t *testing.T) {
	t.Parallel()

	// Create a temp directory with a .git directory for @git to work
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

	paths := resolveAllPreset(ctx, nil)

	// From @base: WorkDir should be writable
	if !sliceContains(paths.Rw, tmpDir) {
		t.Errorf("@all should include WorkDir from @base in rw paths, got: %v", paths.Rw)
	}

	// From @caches: ~/.cache should be writable
	if !sliceContains(paths.Rw, "/home/user/.cache") {
		t.Errorf("@all should include ~/.cache from @caches in rw paths, got: %v", paths.Rw)
	}

	// From @git: .git/hooks should be read-only
	expectedHooks := filepath.Join(gitDir, "hooks")
	if !sliceContains(paths.Ro, expectedHooks) {
		t.Errorf("@all should include .git/hooks from @git in ro paths, got: %v", paths.Ro)
	}

	// From @lint/all: lint configs should be read-only
	if !sliceContains(paths.Ro, tmpDir+"/biome.json") {
		t.Errorf("@all should include biome.json from @lint/all in ro paths, got: %v", paths.Ro)
	}
}

func Test_AllPreset_Respects_Disabled_Base_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	disabled := map[string]bool{
		"@base": true,
	}

	paths := resolveAllPreset(ctx, disabled)

	// Should NOT include WorkDir from @base
	if sliceContains(paths.Rw, "/home/user/project") {
		t.Errorf("@all should NOT include WorkDir when @base is disabled, got: %v", paths.Rw)
	}

	// Should still include configs from @caches
	if !sliceContains(paths.Rw, "/home/user/.cache") {
		t.Errorf("@all should include ~/.cache from @caches, got: %v", paths.Rw)
	}
}

func Test_AllPreset_Respects_Disabled_Caches_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	disabled := map[string]bool{
		"@caches": true,
	}

	paths := resolveAllPreset(ctx, disabled)

	// Should NOT include cache paths from @caches
	if sliceContains(paths.Rw, "/home/user/.cache") {
		t.Errorf("@all should NOT include ~/.cache when @caches is disabled, got: %v", paths.Rw)
	}

	// Should still include WorkDir from @base
	if !sliceContains(paths.Rw, "/home/user/project") {
		t.Errorf("@all should include WorkDir from @base, got: %v", paths.Rw)
	}
}

func Test_AllPreset_Respects_Disabled_Git_When_Called(t *testing.T) {
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

	disabled := map[string]bool{
		"@git": true,
	}

	paths := resolveAllPreset(ctx, disabled)

	// Should NOT include .git/hooks from @git
	expectedHooks := filepath.Join(gitDir, "hooks")
	if sliceContains(paths.Ro, expectedHooks) {
		t.Errorf("@all should NOT include .git/hooks when @git is disabled, got: %v", paths.Ro)
	}
}

func Test_AllPreset_Respects_Disabled_LintAll_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	disabled := map[string]bool{
		"@lint/all": true,
	}

	paths := resolveAllPreset(ctx, disabled)

	// Should NOT include lint configs from @lint/all
	if sliceContains(paths.Ro, "/home/user/project/biome.json") {
		t.Errorf("@all should NOT include biome.json when @lint/all is disabled, got: %v", paths.Ro)
	}

	// Should still include base paths
	if !sliceContains(paths.Rw, "/home/user/project") {
		t.Errorf("@all should include WorkDir from @base, got: %v", paths.Rw)
	}
}

func Test_AllPreset_Respects_Disabled_SubPreset_Of_LintAll_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	// Disable a specific lint preset (not @lint/all itself)
	disabled := map[string]bool{
		"@lint/python": true,
	}

	paths := resolveAllPreset(ctx, disabled)

	// Should NOT include Python lint configs
	if sliceContains(paths.Ro, "/home/user/project/pyproject.toml") {
		t.Errorf("@all should NOT include pyproject.toml when @lint/python is disabled, got: %v", paths.Ro)
	}

	// Should still include TS and Go lint configs
	if !sliceContains(paths.Ro, "/home/user/project/biome.json") {
		t.Errorf("@all should include biome.json from @lint/ts, got: %v", paths.Ro)
	}

	if !sliceContains(paths.Ro, "/home/user/project/.golangci.yml") {
		t.Errorf("@all should include .golangci.yml from @lint/go, got: %v", paths.Ro)
	}
}

func Test_AllPreset_Returns_Secrets_As_Excluded_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveAllPreset(ctx, nil)

	// From @base: secrets should be excluded
	if !sliceContains(paths.Exclude, "/home/user/.ssh") {
		t.Errorf("@all should include ~/.ssh in exclude paths from @base, got: %v", paths.Exclude)
	}

	if !sliceContains(paths.Exclude, "/home/user/.gnupg") {
		t.Errorf("@all should include ~/.gnupg in exclude paths from @base, got: %v", paths.Exclude)
	}

	if !sliceContains(paths.Exclude, "/home/user/.aws") {
		t.Errorf("@all should include ~/.aws in exclude paths from @base, got: %v", paths.Exclude)
	}
}

func Test_AllPreset_Uses_Absolute_Paths_When_Called(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths := resolveAllPreset(ctx, nil)

	for _, p := range paths.Ro {
		if p == "" || p[0] != '/' {
			t.Errorf("ro path should be absolute: %q", p)
		}
	}

	for _, p := range paths.Rw {
		if p == "" || p[0] != '/' {
			t.Errorf("rw path should be absolute: %q", p)
		}
	}

	for _, p := range paths.Exclude {
		if p == "" || p[0] != '/' {
			t.Errorf("exclude path should be absolute: %q", p)
		}
	}
}

// ============================================================================
// ExpandPresets function tests
// ============================================================================

func Test_ExpandPresets_Applies_All_By_Default_When_Empty_Presets(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths, err := ExpandPresets(nil, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// @all is the default, so we should have:
	// - WorkDir and /tmp writable (from @base)
	// - cache paths writable (from @caches)
	// - secrets excluded (from @base)
	// - lint configs protected (from @lint/all)

	// From @base
	if !sliceContains(paths.Rw, "/home/user/project") {
		t.Errorf("default expansion should include WorkDir from @base, got rw: %v", paths.Rw)
	}

	if !sliceContains(paths.Exclude, "/home/user/.ssh") {
		t.Errorf("default expansion should exclude ~/.ssh from @base, got exclude: %v", paths.Exclude)
	}

	// From @caches
	if !sliceContains(paths.Rw, "/home/user/.cache") {
		t.Errorf("default expansion should include ~/.cache from @caches, got rw: %v", paths.Rw)
	}

	// From @lint/all (via @all)
	if !sliceContains(paths.Ro, "/home/user/project/biome.json") {
		t.Errorf("default expansion should include biome.json from @lint/all, got ro: %v", paths.Ro)
	}
}

func Test_ExpandPresets_Applies_All_By_Default_When_Empty_Slice(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths, err := ExpandPresets([]string{}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be same as nil - @all is default
	if !sliceContains(paths.Rw, "/home/user/project") {
		t.Errorf("empty slice expansion should include WorkDir from @base, got rw: %v", paths.Rw)
	}
}

func Test_ExpandPresets_Removes_Preset_With_Negation(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths, err := ExpandPresets([]string{"!@lint/python"}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should NOT include Python lint configs
	if sliceContains(paths.Ro, "/home/user/project/pyproject.toml") {
		t.Errorf("!@lint/python should exclude pyproject.toml, got ro: %v", paths.Ro)
	}

	// Should still include other lint configs
	if !sliceContains(paths.Ro, "/home/user/project/biome.json") {
		t.Errorf("!@lint/python should still include biome.json from @lint/ts, got ro: %v", paths.Ro)
	}

	if !sliceContains(paths.Ro, "/home/user/project/.golangci.yml") {
		t.Errorf("!@lint/python should still include .golangci.yml from @lint/go, got ro: %v", paths.Ro)
	}
}

func Test_ExpandPresets_Removes_All_Lint_With_LintAll_Negation(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths, err := ExpandPresets([]string{"!@lint/all"}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should NOT include any lint configs
	if sliceContains(paths.Ro, "/home/user/project/biome.json") {
		t.Errorf("!@lint/all should exclude biome.json, got ro: %v", paths.Ro)
	}

	if sliceContains(paths.Ro, "/home/user/project/.golangci.yml") {
		t.Errorf("!@lint/all should exclude .golangci.yml, got ro: %v", paths.Ro)
	}

	if sliceContains(paths.Ro, "/home/user/project/pyproject.toml") {
		t.Errorf("!@lint/all should exclude pyproject.toml, got ro: %v", paths.Ro)
	}

	// Should still include base functionality
	if !sliceContains(paths.Rw, "/home/user/project") {
		t.Errorf("!@lint/all should still include WorkDir from @base, got rw: %v", paths.Rw)
	}

	if !sliceContains(paths.Rw, "/home/user/.cache") {
		t.Errorf("!@lint/all should still include ~/.cache from @caches, got rw: %v", paths.Rw)
	}
}

func Test_ExpandPresets_Removes_Caches_With_Negation(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths, err := ExpandPresets([]string{"!@caches"}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should NOT include cache paths
	if sliceContains(paths.Rw, "/home/user/.cache") {
		t.Errorf("!@caches should exclude ~/.cache, got rw: %v", paths.Rw)
	}

	if sliceContains(paths.Rw, "/home/user/.npm") {
		t.Errorf("!@caches should exclude ~/.npm, got rw: %v", paths.Rw)
	}

	// Should still include base functionality
	if !sliceContains(paths.Rw, "/home/user/project") {
		t.Errorf("!@caches should still include WorkDir from @base, got rw: %v", paths.Rw)
	}
}

func Test_ExpandPresets_Returns_Error_For_Unknown_Preset(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	_, err := ExpandPresets([]string{"@unknown"}, ctx)
	if err == nil {
		t.Fatal("expected error for unknown preset, got nil")
	}

	if !strings.Contains(err.Error(), "unknown preset") {
		t.Errorf("error should mention 'unknown preset', got: %v", err)
	}

	if !strings.Contains(err.Error(), "@unknown") {
		t.Errorf("error should mention the preset name '@unknown', got: %v", err)
	}
}

func Test_ExpandPresets_Returns_Error_For_Unknown_Negated_Preset(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	_, err := ExpandPresets([]string{"!@invalid"}, ctx)
	if err == nil {
		t.Fatal("expected error for unknown negated preset, got nil")
	}

	if !strings.Contains(err.Error(), "unknown preset") {
		t.Errorf("error should mention 'unknown preset', got: %v", err)
	}

	if !strings.Contains(err.Error(), "@invalid") {
		t.Errorf("error should mention the preset name '@invalid', got: %v", err)
	}
}

func Test_ExpandPresets_Handles_Multiple_Negations(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths, err := ExpandPresets([]string{"!@lint/python", "!@lint/go"}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should NOT include Python or Go lint configs
	if sliceContains(paths.Ro, "/home/user/project/pyproject.toml") {
		t.Errorf("should exclude pyproject.toml when @lint/python is negated, got ro: %v", paths.Ro)
	}

	if sliceContains(paths.Ro, "/home/user/project/.golangci.yml") {
		t.Errorf("should exclude .golangci.yml when @lint/go is negated, got ro: %v", paths.Ro)
	}

	// Should still include TypeScript lint configs
	if !sliceContains(paths.Ro, "/home/user/project/biome.json") {
		t.Errorf("should include biome.json from @lint/ts, got ro: %v", paths.Ro)
	}
}

func Test_ExpandPresets_ReEnables_After_Disable(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	// Disable all lint, then re-enable Python
	paths, err := ExpandPresets([]string{"!@lint/all", "@lint/python"}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should include Python lint configs (re-enabled)
	if !sliceContains(paths.Ro, "/home/user/project/pyproject.toml") {
		t.Errorf("@lint/python should be re-enabled after !@lint/all, got ro: %v", paths.Ro)
	}

	// Should NOT include TS or Go lint configs (still disabled)
	if sliceContains(paths.Ro, "/home/user/project/biome.json") {
		t.Errorf("@lint/ts should still be disabled after !@lint/all, got ro: %v", paths.Ro)
	}

	if sliceContains(paths.Ro, "/home/user/project/.golangci.yml") {
		t.Errorf("@lint/go should still be disabled after !@lint/all, got ro: %v", paths.Ro)
	}
}

func Test_ExpandPresets_Last_Mention_Wins_Toggle_Semantics(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	// Disable Python, then re-enable it
	paths, err := ExpandPresets([]string{"!@lint/python", "@lint/python"}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should include Python lint configs (last mention wins)
	if !sliceContains(paths.Ro, "/home/user/project/pyproject.toml") {
		t.Errorf("@lint/python should be enabled (last mention wins), got ro: %v", paths.Ro)
	}
}

func Test_ExpandPresets_Disable_After_Enable_Works(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	// Enable Python explicitly, then disable it
	paths, err := ExpandPresets([]string{"@lint/python", "!@lint/python"}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should NOT include Python lint configs (last mention wins)
	if sliceContains(paths.Ro, "/home/user/project/pyproject.toml") {
		t.Errorf("@lint/python should be disabled (last mention wins), got ro: %v", paths.Ro)
	}
}

func Test_ExpandPresets_Disable_All_Then_Enable_Base(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	// Disable @all entirely, then re-enable @base
	paths, err := ExpandPresets([]string{"!@all", "@base"}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should include base functionality (re-enabled)
	if !sliceContains(paths.Rw, "/home/user/project") {
		t.Errorf("@base should be enabled after !@all + @base, got rw: %v", paths.Rw)
	}

	if !sliceContains(paths.Exclude, "/home/user/.ssh") {
		t.Errorf("@base should exclude ~/.ssh after !@all + @base, got exclude: %v", paths.Exclude)
	}

	// Should NOT include caches (disabled via !@all, not re-enabled)
	if sliceContains(paths.Rw, "/home/user/.cache") {
		t.Errorf("@caches should still be disabled after !@all, got rw: %v", paths.Rw)
	}

	// Should NOT include lint configs (disabled via !@all, not re-enabled)
	if sliceContains(paths.Ro, "/home/user/project/biome.json") {
		t.Errorf("@lint/all should still be disabled after !@all, got ro: %v", paths.Ro)
	}
}

func Test_ExpandPresets_Negating_All_Returns_Empty_Paths(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	paths, err := ExpandPresets([]string{"!@all"}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be completely empty
	if len(paths.Ro) != 0 {
		t.Errorf("!@all should result in empty ro paths, got: %v", paths.Ro)
	}

	if len(paths.Rw) != 0 {
		t.Errorf("!@all should result in empty rw paths, got: %v", paths.Rw)
	}

	if len(paths.Exclude) != 0 {
		t.Errorf("!@all should result in empty exclude paths, got: %v", paths.Exclude)
	}
}

func Test_ExpandPresets_ErrUnknownPreset_Is_Wrapped(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	_, err := ExpandPresets([]string{"@nonexistent"}, ctx)
	if err == nil {
		t.Fatal("expected error for unknown preset, got nil")
	}

	// Error should wrap ErrUnknownPreset
	if !strings.Contains(err.Error(), ErrUnknownPreset.Error()) {
		t.Errorf("error should wrap ErrUnknownPreset, got: %v", err)
	}
}

func Test_ExpandPresets_Explicit_All_Same_As_Default(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	// Explicit @all should be same as default
	pathsExplicit, err := ExpandPresets([]string{"@all"}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pathsDefault, err := ExpandPresets(nil, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have same number of paths
	if len(pathsExplicit.Ro) != len(pathsDefault.Ro) {
		t.Errorf("explicit @all should have same ro paths as default: explicit=%d, default=%d",
			len(pathsExplicit.Ro), len(pathsDefault.Ro))
	}

	if len(pathsExplicit.Rw) != len(pathsDefault.Rw) {
		t.Errorf("explicit @all should have same rw paths as default: explicit=%d, default=%d",
			len(pathsExplicit.Rw), len(pathsDefault.Rw))
	}

	if len(pathsExplicit.Exclude) != len(pathsDefault.Exclude) {
		t.Errorf("explicit @all should have same exclude paths as default: explicit=%d, default=%d",
			len(pathsExplicit.Exclude), len(pathsDefault.Exclude))
	}
}

func Test_ExpandPresets_With_Git_Directory(t *testing.T) {
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

	paths, err := ExpandPresets(nil, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should include git paths from @git
	expectedHooks := filepath.Join(gitDir, "hooks")
	if !sliceContains(paths.Ro, expectedHooks) {
		t.Errorf("default expansion should include .git/hooks from @git, got ro: %v", paths.Ro)
	}

	expectedConfig := filepath.Join(gitDir, "config")
	if !sliceContains(paths.Ro, expectedConfig) {
		t.Errorf("default expansion should include .git/config from @git, got ro: %v", paths.Ro)
	}
}

func Test_ExpandPresets_Disable_Git_Preset(t *testing.T) {
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

	paths, err := ExpandPresets([]string{"!@git"}, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should NOT include git paths
	expectedHooks := filepath.Join(gitDir, "hooks")
	if sliceContains(paths.Ro, expectedHooks) {
		t.Errorf("!@git should exclude .git/hooks, got ro: %v", paths.Ro)
	}

	expectedConfig := filepath.Join(gitDir, "config")
	if sliceContains(paths.Ro, expectedConfig) {
		t.Errorf("!@git should exclude .git/config, got ro: %v", paths.Ro)
	}

	// Should still include base functionality
	if !sliceContains(paths.Rw, tmpDir) {
		t.Errorf("!@git should still include WorkDir from @base, got rw: %v", paths.Rw)
	}
}

func Test_ExpandPresets_Unknown_Preset_Lists_Available(t *testing.T) {
	t.Parallel()

	ctx := PresetContext{
		HomeDir: "/home/user",
		WorkDir: "/home/user/project",
	}

	_, err := ExpandPresets([]string{"@nonexistent"}, ctx)
	if err == nil {
		t.Fatal("expected error for unknown preset, got nil")
	}

	// Error should list available presets
	errMsg := err.Error()
	if !strings.Contains(errMsg, "available:") {
		t.Errorf("error should include 'available:', got: %v", errMsg)
	}

	// Error should mention specific available presets
	for _, preset := range []string{"@all", "@base", "@git", "@caches"} {
		if !strings.Contains(errMsg, preset) {
			t.Errorf("error should mention available preset %s, got: %v", preset, errMsg)
		}
	}
}

func Test_AvailablePresets_Returns_Sorted_List(t *testing.T) {
	t.Parallel()

	presets := AvailablePresets()

	if len(presets) == 0 {
		t.Fatal("AvailablePresets should return non-empty list")
	}

	// Check that it includes expected presets
	expected := []string{"@all", "@base", "@caches", "@git", "@lint/all", "@lint/go", "@lint/python", "@lint/ts"}
	for _, preset := range expected {
		if !sliceContains(presets, preset) {
			t.Errorf("AvailablePresets should include %s, got: %v", preset, presets)
		}
	}

	// Check that it's sorted
	for i := 1; i < len(presets); i++ {
		if presets[i] < presets[i-1] {
			t.Errorf("AvailablePresets should be sorted, but %s comes after %s", presets[i-1], presets[i])
		}
	}
}
