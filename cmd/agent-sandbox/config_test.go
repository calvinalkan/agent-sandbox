package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// =============================================================================
// Defaults
// =============================================================================

func Test_LoadConfig_Returns_Defaults_When_No_Config_Files_Exist(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		want: Config{
			Network:  boolPtr(true),
			Docker:   boolPtr(false),
			Commands: defaultCommands(),
		},
	}).run(t)
}

// =============================================================================
// Project Config Loading
// =============================================================================

func Test_LoadConfig_Loads_Project_Json_File(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		files: map[string]string{
			".agent-sandbox.json": `{"network": false, "docker": true}`,
		},
		want: Config{
			Network:  boolPtr(false),
			Docker:   boolPtr(true),
			Commands: defaultCommands(),
		},
	}).run(t)
}

func Test_LoadConfig_Loads_Project_Jsonc_File(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		files: map[string]string{
			".agent-sandbox.jsonc": `{
				// this is a comment
				"network": false
			}`,
		},
		want: Config{
			Network:  boolPtr(false),
			Docker:   boolPtr(false),
			Commands: defaultCommands(),
		},
	}).run(t)
}

func Test_LoadConfig_Allows_Comments_In_Json_Files(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		files: map[string]string{
			".agent-sandbox.json": `{
				// line comment
				/* block comment */
				"docker": true
			}`,
		},
		want: Config{
			Network:  boolPtr(true),
			Docker:   boolPtr(true),
			Commands: defaultCommands(),
		},
	}).run(t)
}

// =============================================================================
// Global Config Loading
// =============================================================================

func Test_LoadConfig_Loads_Global_Json_File(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		globalFiles: map[string]string{
			"agent-sandbox/config.json": `{"network": false}`,
		},
		want: Config{
			Network:  boolPtr(false),
			Docker:   boolPtr(false),
			Commands: defaultCommands(),
		},
	}).run(t)
}

func Test_LoadConfig_Loads_Global_Jsonc_File(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		globalFiles: map[string]string{
			"agent-sandbox/config.jsonc": `{
				// global comment
				"docker": true
			}`,
		},
		want: Config{
			Network:  boolPtr(true),
			Docker:   boolPtr(true),
			Commands: defaultCommands(),
		},
	}).run(t)
}

// =============================================================================
// Config Layering
// =============================================================================

func Test_LoadConfig_Project_Overrides_Global(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		globalFiles: map[string]string{
			"agent-sandbox/config.json": `{"network": false, "docker": true}`,
		},
		files: map[string]string{
			".agent-sandbox.json": `{"network": true}`,
		},
		want: Config{
			Network:  boolPtr(true), // project overrides
			Docker:   boolPtr(true), // kept from global
			Commands: defaultCommands(),
		},
	}).run(t)
}

func Test_LoadConfig_Explicit_Config_Replaces_Project_But_Not_Global(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		globalFiles: map[string]string{
			"agent-sandbox/config.json": `{"docker": true}`,
		},
		files: map[string]string{
			"custom.json":         `{"network": false}`,
			".agent-sandbox.json": `{"network": true, "docker": false}`,
		},
		configPath: "custom.json",
		want: Config{
			Network:  boolPtr(false), // from custom.json
			Docker:   boolPtr(true),  // from global (project skipped)
			Commands: defaultCommands(),
		},
	}).run(t)
}

// =============================================================================
// Filesystem Arrays
// =============================================================================

func Test_LoadConfig_Sets_All_Filesystem_Fields_From_Project(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		files: map[string]string{
			".agent-sandbox.json": `{
				"filesystem": {
					"presets": ["!@lint/python"],
					"ro": ["/project/ro"],
					"rw": ["/project/rw"],
					"exclude": ["/project/exclude"]
				}
			}`,
		},
		want: Config{
			Network: boolPtr(true),
			Docker:  boolPtr(false),
			Filesystem: FilesystemConfig{
				Presets: []string{"!@lint/python"},
				Ro:      []string{"/project/ro"},
				Rw:      []string{"/project/rw"},
				Exclude: []string{"/project/exclude"},
			},
			Commands: defaultCommands(),
		},
	}).run(t)
}

func Test_LoadConfig_Concatenates_Global_And_Project_Filesystem_Arrays(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		globalFiles: map[string]string{
			"agent-sandbox/config.json": `{
				"filesystem": {
					"presets": ["!@lint/go"],
					"ro": ["/global/ro"],
					"rw": ["/global/rw"],
					"exclude": ["/global/exclude"]
				}
			}`,
		},
		files: map[string]string{
			".agent-sandbox.json": `{
				"filesystem": {
					"presets": ["!@lint/python"],
					"ro": ["/project/ro"],
					"rw": ["/project/rw"],
					"exclude": ["/project/exclude"]
				}
			}`,
		},
		want: Config{
			Network: boolPtr(true),
			Docker:  boolPtr(false),
			Filesystem: FilesystemConfig{
				Presets: []string{"!@lint/go", "!@lint/python"},
				Ro:      []string{"/global/ro", "/project/ro"},
				Rw:      []string{"/global/rw", "/project/rw"},
				Exclude: []string{"/global/exclude", "/project/exclude"},
			},
			Commands: defaultCommands(),
		},
	}).run(t)
}

func Test_LoadConfig_Preserves_Global_Filesystem_When_Project_Is_Empty(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		globalFiles: map[string]string{
			"agent-sandbox/config.json": `{
				"filesystem": {
					"ro": ["/global/ro"],
					"rw": ["/global/rw"]
				}
			}`,
		},
		files: map[string]string{
			".agent-sandbox.json": `{"network": false}`,
		},
		want: Config{
			Network: boolPtr(false),
			Docker:  boolPtr(false),
			Filesystem: FilesystemConfig{
				Ro: []string{"/global/ro"},
				Rw: []string{"/global/rw"},
			},
			Commands: defaultCommands(),
		},
	}).run(t)
}

// =============================================================================
// Commands
// =============================================================================

func Test_LoadConfig_Parses_All_Command_Rule_Types(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		files: map[string]string{
			".agent-sandbox.json": `{
				"commands": {
					"git": "@git",
					"rm": false,
					"npm": true,
					"curl": "~/bin/curl-wrapper"
				}
			}`,
		},
		want: Config{
			Network: boolPtr(true),
			Docker:  boolPtr(false),
			Commands: map[string]CommandRule{
				"git":  {Kind: CommandRulePreset, Value: "@git"},
				"rm":   {Kind: CommandRuleBlock},
				"npm":  {Kind: CommandRuleExplicitAllow},
				"curl": {Kind: CommandRuleScript, Value: "~/bin/curl-wrapper"},
			},
		},
	}).run(t)
}

func Test_LoadConfig_Project_Commands_Override_Global_Commands(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		globalFiles: map[string]string{
			"agent-sandbox/config.json": `{
				"commands": {
					"git": "@git",
					"rm": false
				}
			}`,
		},
		files: map[string]string{
			".agent-sandbox.json": `{
				"commands": {
					"git": true
				}
			}`,
		},
		want: Config{
			Network: boolPtr(true),
			Docker:  boolPtr(false),
			Commands: map[string]CommandRule{
				"git": {Kind: CommandRuleExplicitAllow}, // project overrides
				"rm":  {Kind: CommandRuleBlock},         // kept from global
			},
		},
	}).run(t)
}

func Test_LoadConfig_Project_Commands_Add_To_Defaults(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		files: map[string]string{
			".agent-sandbox.json": `{
				"commands": {
					"rm": false
				}
			}`,
		},
		want: Config{
			Network: boolPtr(true),
			Docker:  boolPtr(false),
			Commands: map[string]CommandRule{
				"git": {Kind: CommandRulePreset, Value: "@git"}, // default preserved
				"rm":  {Kind: CommandRuleBlock},                 // added by project
			},
		},
	}).run(t)
}

func Test_LoadConfig_Commands_Three_Layer_Override(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		globalFiles: map[string]string{
			"agent-sandbox/config.json": `{
				"commands": {
					"git": false,
					"npm": "~/npm-wrapper"
				}
			}`,
		},
		files: map[string]string{
			".agent-sandbox.json": `{
				"commands": {
					"npm": true
				}
			}`,
		},
		want: Config{
			Network: boolPtr(true),
			Docker:  boolPtr(false),
			Commands: map[string]CommandRule{
				"git": {Kind: CommandRuleBlock},         // global overrode default
				"npm": {Kind: CommandRuleExplicitAllow}, // project overrode global
			},
		},
	}).run(t)
}

func Test_LoadConfig_Preserves_Default_Commands_When_Commands_Section_Empty(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		files: map[string]string{
			".agent-sandbox.json": `{
				"network": false,
				"commands": {}
			}`,
		},
		want: Config{
			Network:  boolPtr(false),
			Docker:   boolPtr(false),
			Commands: defaultCommands(),
		},
	}).run(t)
}

// =============================================================================
// Errors
// =============================================================================

func Test_LoadConfig_Returns_Error_When_Both_Json_And_Jsonc_Exist_For_Project(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		files: map[string]string{
			".agent-sandbox.json":  `{}`,
			".agent-sandbox.jsonc": `{}`,
		},
		wantErr: "both",
	}).run(t)
}

func Test_LoadConfig_Returns_Error_When_Both_Json_And_Jsonc_Exist_For_Global(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		globalFiles: map[string]string{
			"agent-sandbox/config.json":  `{}`,
			"agent-sandbox/config.jsonc": `{}`,
		},
		wantErr: "both",
	}).run(t)
}

func Test_LoadConfig_Returns_Error_When_Explicit_Config_Not_Found(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		configPath: "nonexistent.json",
		wantErr:    "no such file",
	}).run(t)
}

func Test_LoadConfig_Returns_Error_When_Project_Json_Invalid(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		files: map[string]string{
			".agent-sandbox.json": `{invalid}`,
		},
		wantErr: "parsing config",
	}).run(t)
}

func Test_LoadConfig_Returns_Error_When_Global_Json_Invalid(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		globalFiles: map[string]string{
			"agent-sandbox/config.json": `{not valid json}`,
		},
		wantErr: "parsing config",
	}).run(t)
}

func Test_LoadConfig_Returns_Error_When_Command_Rule_Is_Number(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		files: map[string]string{
			".agent-sandbox.json": `{"commands": {"git": 123}}`,
		},
		wantErr: "must be boolean or string",
	}).run(t)
}

func Test_LoadConfig_Returns_Error_When_Command_Rule_Is_Array(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		files: map[string]string{
			".agent-sandbox.json": `{"commands": {"git": []}}`,
		},
		wantErr: "must be boolean or string",
	}).run(t)
}

func Test_LoadConfig_Returns_Error_When_Command_Rule_Is_Object(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		files: map[string]string{
			".agent-sandbox.json": `{"commands": {"git": {}}}`,
		},
		wantErr: "must be boolean or string",
	}).run(t)
}

func Test_LoadConfig_Returns_Error_When_Command_Rule_Is_Null(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		files: map[string]string{
			".agent-sandbox.json": `{"commands": {"git": null}}`,
		},
		wantErr: "must be boolean or string",
	}).run(t)
}

func Test_LoadConfig_Error_Message_Includes_File_Path(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		files: map[string]string{
			".agent-sandbox.json": `{invalid}`,
		},
		wantErr: ".agent-sandbox.json",
	}).run(t)
}

func Test_LoadConfig_Returns_Error_When_Top_Level_Field_Is_Unknown(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		files: map[string]string{
			".agent-sandbox.json": `{"readonly": ["Makefile"]}`,
		},
		wantErr: `unknown field "readonly"`,
	}).run(t)
}

func Test_LoadConfig_Returns_Error_When_Global_Config_Has_Unknown_Field(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		globalFiles: map[string]string{
			"agent-sandbox/config.json": `{"unknown_option": true}`,
		},
		wantErr: `unknown field "unknown_option"`,
	}).run(t)
}

func Test_LoadConfig_Returns_Error_When_Filesystem_Has_Unknown_Nested_Field(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		files: map[string]string{
			".agent-sandbox.json": `{"filesystem": {"readonly": ["Makefile"]}}`,
		},
		wantErr: `unknown field "readonly"`,
	}).run(t)
}

func Test_LoadConfig_Returns_Error_When_Field_Name_Is_Misspelled(t *testing.T) {
	t.Parallel()

	(&configTestCase{
		files: map[string]string{
			".agent-sandbox.json": `{"netwrok": true}`,
		},
		wantErr: `unknown field "netwrok"`,
	}).run(t)
}

// =============================================================================
// Metadata Tracking (path-dependent, tested separately)
// =============================================================================

func Test_LoadConfig_Tracks_Global_Config_Path(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	xdgConfigHome := t.TempDir()

	globalPath := filepath.Join(xdgConfigHome, "agent-sandbox", "config.json")
	mustMkdir(t, filepath.Dir(globalPath))
	mustWriteFile(t, globalPath, `{}`)

	got, err := LoadConfig(LoadConfigInput{
		WorkDirOverride: workDir,
		EnvVars:         map[string]string{"XDG_CONFIG_HOME": xdgConfigHome},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.LoadedConfigFiles["global"] != globalPath {
		t.Errorf("LoadedConfigFiles[global] = %q, want %q", got.LoadedConfigFiles["global"], globalPath)
	}
}

func Test_LoadConfig_Tracks_Project_Config_Path(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	xdgConfigHome := t.TempDir()

	projectPath := filepath.Join(workDir, ".agent-sandbox.json")
	mustWriteFile(t, projectPath, `{}`)

	got, err := LoadConfig(LoadConfigInput{
		WorkDirOverride: workDir,
		EnvVars:         map[string]string{"XDG_CONFIG_HOME": xdgConfigHome},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.LoadedConfigFiles["project"] != projectPath {
		t.Errorf("LoadedConfigFiles[project] = %q, want %q", got.LoadedConfigFiles["project"], projectPath)
	}
}

func Test_LoadConfig_Tracks_Explicit_Config_And_Skips_Project(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	xdgConfigHome := t.TempDir()

	explicitPath := filepath.Join(workDir, "custom.json")
	mustWriteFile(t, explicitPath, `{}`)
	mustWriteFile(t, filepath.Join(workDir, ".agent-sandbox.json"), `{}`)

	got, err := LoadConfig(LoadConfigInput{
		WorkDirOverride: workDir,
		ConfigPath:      "custom.json",
		EnvVars:         map[string]string{"XDG_CONFIG_HOME": xdgConfigHome},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.LoadedConfigFiles["explicit"] != explicitPath {
		t.Errorf("LoadedConfigFiles[explicit] = %q, want %q", got.LoadedConfigFiles["explicit"], explicitPath)
	}

	if _, ok := got.LoadedConfigFiles["project"]; ok {
		t.Error("LoadedConfigFiles[project] should not be set when using --config")
	}
}

func Test_LoadConfig_Tracks_Global_And_Project_Filesystem_Separately(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	xdgConfigHome := t.TempDir()

	globalPath := filepath.Join(xdgConfigHome, "agent-sandbox", "config.json")
	mustMkdir(t, filepath.Dir(globalPath))
	mustWriteFile(t, globalPath, `{"filesystem":{"ro":["/global/path"]}}`)
	mustWriteFile(t, filepath.Join(workDir, ".agent-sandbox.json"), `{"filesystem":{"ro":["/project/path"]}}`)

	got, err := LoadConfig(LoadConfigInput{
		WorkDirOverride: workDir,
		EnvVars:         map[string]string{"XDG_CONFIG_HOME": xdgConfigHome},
	})
	if err != nil {
		t.Fatal(err)
	}

	wantGlobal := FilesystemConfig{Ro: []string{"/global/path"}}
	wantProject := FilesystemConfig{Ro: []string{"/project/path"}}
	wantMerged := FilesystemConfig{Ro: []string{"/global/path", "/project/path"}}

	if diff := cmp.Diff(wantGlobal, got.GlobalFilesystem); diff != "" {
		t.Errorf("GlobalFilesystem mismatch (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(wantProject, got.ProjectFilesystem); diff != "" {
		t.Errorf("ProjectFilesystem mismatch (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(wantMerged, got.Filesystem); diff != "" {
		t.Errorf("Filesystem (merged) mismatch (-want +got):\n%s", diff)
	}
}

// =============================================================================
// Test Helpers
// =============================================================================

// cmpConfig compares Config structs, ignoring fields that vary per test (paths, etc.)
var cmpConfig = cmp.Options{
	cmpopts.IgnoreFields(Config{}, "EffectiveCwd", "LoadedConfigFiles", "GlobalFilesystem", "ProjectFilesystem"),
}

// configTestCase defines a single LoadConfig test.
type configTestCase struct {
	files       map[string]string // relative to workDir
	globalFiles map[string]string // relative to XDG_CONFIG_HOME
	configPath  string            // --config flag
	want        Config
	wantErr     string
}

// run executes the test case.
func (tc *configTestCase) run(t *testing.T) {
	t.Helper()

	workDir := t.TempDir()
	xdgConfigHome := t.TempDir()

	for path, content := range tc.files {
		fullPath := filepath.Join(workDir, path)
		mustMkdir(t, filepath.Dir(fullPath))
		mustWriteFile(t, fullPath, content)
	}

	for path, content := range tc.globalFiles {
		fullPath := filepath.Join(xdgConfigHome, path)
		mustMkdir(t, filepath.Dir(fullPath))
		mustWriteFile(t, fullPath, content)
	}

	got, err := LoadConfig(LoadConfigInput{
		WorkDirOverride: workDir,
		ConfigPath:      tc.configPath,
		EnvVars:         map[string]string{"XDG_CONFIG_HOME": xdgConfigHome},
	})

	if tc.wantErr != "" {
		if err == nil {
			t.Fatalf("want error containing %q, got nil", tc.wantErr)
		}

		if !strings.Contains(err.Error(), tc.wantErr) {
			t.Fatalf("want error containing %q, got %q", tc.wantErr, err.Error())
		}

		return
	}

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if diff := cmp.Diff(tc.want, got, cmpConfig); diff != "" {
		t.Errorf("Config mismatch (-want +got):\n%s", diff)
	}
}

// defaultCommands returns the default commands config for test expectations.
func defaultCommands() map[string]CommandRule {
	return map[string]CommandRule{
		"git": {Kind: CommandRulePreset, Value: "@git"},
	}
}

func boolPtr(b bool) *bool {
	return &b
}
