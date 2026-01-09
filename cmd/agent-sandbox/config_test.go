package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		files       map[string]string // path -> content (relative to workDir)
		globalFiles map[string]string // path -> content (relative to XDG_CONFIG_HOME)
		configPath  string            // --config flag value
		want        Config
		wantErr     string // substring of error message, empty means no error
	}{
		{
			name:  "defaults when no config files",
			files: map[string]string{},
			want: Config{
				Network: boolPtr(true),
				Docker:  boolPtr(false),
			},
		},
		{
			name: "project config .json",
			files: map[string]string{
				".agent-sandbox.json": `{"network": false}`,
			},
			want: Config{
				Network: boolPtr(false),
				Docker:  boolPtr(false),
			},
		},
		{
			name: "project config .jsonc",
			files: map[string]string{
				".agent-sandbox.jsonc": `{
					// comment
					"network": false
				}`,
			},
			want: Config{
				Network: boolPtr(false),
				Docker:  boolPtr(false),
			},
		},
		{
			name: "project config .json with comments",
			files: map[string]string{
				".agent-sandbox.json": `{
					// comments allowed in .json too
					"docker": true
				}`,
			},
			want: Config{
				Network: boolPtr(true),
				Docker:  boolPtr(true),
			},
		},
		{
			name: "error when both .json and .jsonc exist for project",
			files: map[string]string{
				".agent-sandbox.json":  `{"network": false}`,
				".agent-sandbox.jsonc": `{"docker": true}`,
			},
			wantErr: "both",
		},
		{
			name: "global config .json",
			globalFiles: map[string]string{
				"agent-sandbox/config.json": `{"network": false}`,
			},
			want: Config{
				Network: boolPtr(false),
				Docker:  boolPtr(false),
			},
		},
		{
			name: "global config .jsonc",
			globalFiles: map[string]string{
				"agent-sandbox/config.jsonc": `{
					// global comment
					"docker": true
				}`,
			},
			want: Config{
				Network: boolPtr(true),
				Docker:  boolPtr(true),
			},
		},
		{
			name: "error when both .json and .jsonc exist for global",
			globalFiles: map[string]string{
				"agent-sandbox/config.json":  `{"network": false}`,
				"agent-sandbox/config.jsonc": `{"docker": true}`,
			},
			wantErr: "both",
		},
		{
			name: "project overrides global",
			globalFiles: map[string]string{
				"agent-sandbox/config.json": `{"network": false, "docker": true}`,
			},
			files: map[string]string{
				".agent-sandbox.json": `{"network": true}`,
			},
			want: Config{
				Network: boolPtr(true),
				Docker:  boolPtr(true), // from global
			},
		},
		{
			name: "explicit --config replaces project but not global",
			files: map[string]string{
				"custom.json":         `{"network": false}`,
				".agent-sandbox.json": `{"network": true, "docker": false}`,
			},
			globalFiles: map[string]string{
				"agent-sandbox/config.json": `{"docker": true}`,
			},
			configPath: "custom.json",
			want: Config{
				Network: boolPtr(false), // from custom.json
				Docker:  boolPtr(true),  // from global (NOT from project)
			},
		},
		{
			name:       "explicit --config not found is error",
			files:      map[string]string{},
			configPath: "nonexistent.json",
			wantErr:    "no such file",
		},
		{
			name: "invalid json in project config",
			files: map[string]string{
				".agent-sandbox.json": `{invalid}`,
			},
			wantErr: "parsing config",
		},
		{
			name: "invalid json in global config",
			globalFiles: map[string]string{
				"agent-sandbox/config.json": `{invalid}`,
			},
			wantErr: "parsing config",
		},
		{
			name: "filesystem config from project",
			files: map[string]string{
				".agent-sandbox.json": `{
					"filesystem": {
						"presets": ["!@lint/python"],
						"ro": ["/path/ro"],
						"rw": ["/path/rw"],
						"exclude": ["/path/exclude"]
					}
				}`,
			},
			want: Config{
				Network: boolPtr(true),
				Docker:  boolPtr(false),
				Filesystem: FilesystemConfig{
					Presets: []string{"!@lint/python"},
					Ro:      []string{"/path/ro"},
					Rw:      []string{"/path/rw"},
					Exclude: []string{"/path/exclude"},
				},
			},
		},
		{
			name: "filesystem arrays from project are concatenated with global",
			globalFiles: map[string]string{
				"agent-sandbox/config.json": `{
					"filesystem": {
						"ro": ["/global/ro"],
						"rw": ["/global/rw"]
					}
				}`,
			},
			files: map[string]string{
				".agent-sandbox.json": `{
					"filesystem": {
						"ro": ["/project/ro"]
					}
				}`,
			},
			want: Config{
				Network: boolPtr(true),
				Docker:  boolPtr(false),
				Filesystem: FilesystemConfig{
					Ro: []string{"/global/ro", "/project/ro"}, // concatenated
					Rw: []string{"/global/rw"},                // from global
				},
			},
		},
		{
			name: "all filesystem arrays are concatenated",
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
			},
		},
		{
			name: "empty override arrays do not affect base arrays",
			globalFiles: map[string]string{
				"agent-sandbox/config.json": `{
					"filesystem": {
						"ro": ["/global/ro"],
						"rw": ["/global/rw"]
					}
				}`,
			},
			files: map[string]string{
				".agent-sandbox.json": `{
					"network": false
				}`,
			},
			want: Config{
				Network: boolPtr(false),
				Docker:  boolPtr(false),
				Filesystem: FilesystemConfig{
					Ro: []string{"/global/ro"},
					Rw: []string{"/global/rw"},
				},
			},
		},
		{
			name: "three-layer concatenation: default, global, project",
			globalFiles: map[string]string{
				"agent-sandbox/config.json": `{
					"filesystem": {
						"ro": ["/global/secrets"]
					}
				}`,
			},
			files: map[string]string{
				".agent-sandbox.json": `{
					"filesystem": {
						"ro": ["/project/sensitive"]
					}
				}`,
			},
			want: Config{
				Network: boolPtr(true),
				Docker:  boolPtr(false),
				Filesystem: FilesystemConfig{
					Ro: []string{"/global/secrets", "/project/sensitive"},
				},
			},
		},
		{
			name: "block comment style supported",
			files: map[string]string{
				".agent-sandbox.json": `{
					/* block comment */
					"network": false
				}`,
			},
			want: Config{
				Network: boolPtr(false),
				Docker:  boolPtr(false),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create temp directories
			workDir := t.TempDir()
			xdgConfigHome := t.TempDir()

			// Write project files
			for path, content := range tt.files {
				fullPath := filepath.Join(workDir, path)

				err := os.MkdirAll(filepath.Dir(fullPath), 0o750)
				if err != nil {
					t.Fatalf("failed to create dir: %v", err)
				}

				err = os.WriteFile(fullPath, []byte(content), 0o600)
				if err != nil {
					t.Fatalf("failed to write file: %v", err)
				}
			}

			// Write global files
			for path, content := range tt.globalFiles {
				fullPath := filepath.Join(xdgConfigHome, path)

				err := os.MkdirAll(filepath.Dir(fullPath), 0o750)
				if err != nil {
					t.Fatalf("failed to create dir: %v", err)
				}

				err = os.WriteFile(fullPath, []byte(content), 0o600)
				if err != nil {
					t.Fatalf("failed to write file: %v", err)
				}
			}

			input := LoadConfigInput{
				WorkDirOverride: workDir,
				ConfigPath:      tt.configPath,
				Env: map[string]string{
					"XDG_CONFIG_HOME": xdgConfigHome,
				},
			}

			got, err := LoadConfig(input)

			// Check error
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got nil", tt.wantErr)
				}

				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %q", tt.wantErr, err.Error())
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check config values
			if got.Network == nil || tt.want.Network == nil {
				if got.Network != tt.want.Network {
					t.Errorf("Network: got %v, want %v", got.Network, tt.want.Network)
				}
			} else if *got.Network != *tt.want.Network {
				t.Errorf("Network: got %v, want %v", *got.Network, *tt.want.Network)
			}

			if got.Docker == nil || tt.want.Docker == nil {
				if got.Docker != tt.want.Docker {
					t.Errorf("Docker: got %v, want %v", got.Docker, tt.want.Docker)
				}
			} else if *got.Docker != *tt.want.Docker {
				t.Errorf("Docker: got %v, want %v", *got.Docker, *tt.want.Docker)
			}

			// Check filesystem config
			if !slicesEqual(got.Filesystem.Presets, tt.want.Filesystem.Presets) {
				t.Errorf("Filesystem.Presets: got %v, want %v", got.Filesystem.Presets, tt.want.Filesystem.Presets)
			}

			if !slicesEqual(got.Filesystem.Ro, tt.want.Filesystem.Ro) {
				t.Errorf("Filesystem.Ro: got %v, want %v", got.Filesystem.Ro, tt.want.Filesystem.Ro)
			}

			if !slicesEqual(got.Filesystem.Rw, tt.want.Filesystem.Rw) {
				t.Errorf("Filesystem.Rw: got %v, want %v", got.Filesystem.Rw, tt.want.Filesystem.Rw)
			}

			if !slicesEqual(got.Filesystem.Exclude, tt.want.Filesystem.Exclude) {
				t.Errorf("Filesystem.Exclude: got %v, want %v", got.Filesystem.Exclude, tt.want.Filesystem.Exclude)
			}

			// Check EffectiveCwd is set
			if got.EffectiveCwd != workDir {
				t.Errorf("EffectiveCwd: got %q, want %q", got.EffectiveCwd, workDir)
			}
		})
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

func Test_DefaultConfig_Has_Git_Preset(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()

	if cfg.Commands == nil {
		t.Fatal("Commands map should not be nil")
	}

	gitRule, ok := cfg.Commands["git"]
	if !ok {
		t.Fatal("git should be in default commands")
	}

	if gitRule.Kind != CommandRulePreset {
		t.Errorf("git should be preset, got kind %v", gitRule.Kind)
	}

	if gitRule.Value != PresetGit {
		t.Errorf("git preset value should be %s, got %q", PresetGit, gitRule.Value)
	}
}

func Test_CommandRule_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    CommandRule
		wantErr string
	}{
		{
			name:  "bool true means raw",
			input: `true`,
			want:  CommandRule{Kind: CommandRuleRaw, Value: ""},
		},
		{
			name:  "bool false means block",
			input: `false`,
			want:  CommandRule{Kind: CommandRuleBlock, Value: ""},
		},
		{
			name:  "string with @ prefix is preset",
			input: `"@git"`,
			want:  CommandRule{Kind: CommandRulePreset, Value: PresetGit},
		},
		{
			name:  "string without @ prefix is script",
			input: `"/path/to/wrapper.sh"`,
			want:  CommandRule{Kind: CommandRuleScript, Value: "/path/to/wrapper.sh"},
		},
		{
			name:  "tilde path is script",
			input: `"~/bin/wrapper"`,
			want:  CommandRule{Kind: CommandRuleScript, Value: "~/bin/wrapper"},
		},
		{
			name:    "number is invalid",
			input:   `123`,
			wantErr: "must be boolean or string",
		},
		{
			name:    "array is invalid",
			input:   `[]`,
			wantErr: "must be boolean or string",
		},
		{
			name:    "object is invalid",
			input:   `{}`,
			wantErr: "must be boolean or string",
		},
		{
			name:    "null is invalid",
			input:   `null`,
			wantErr: "must be boolean or string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got CommandRule

			err := got.UnmarshalJSON([]byte(tt.input))

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}

				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.Kind != tt.want.Kind {
				t.Errorf("Kind: got %v, want %v", got.Kind, tt.want.Kind)
			}

			if got.Value != tt.want.Value {
				t.Errorf("Value: got %q, want %q", got.Value, tt.want.Value)
			}
		})
	}
}

func Test_CommandRule_MarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input CommandRule
		want  string
	}{
		{
			name:  "raw becomes true",
			input: CommandRule{Kind: CommandRuleRaw},
			want:  "true",
		},
		{
			name:  "block becomes false",
			input: CommandRule{Kind: CommandRuleBlock},
			want:  "false",
		},
		{
			name:  "preset keeps string",
			input: CommandRule{Kind: CommandRulePreset, Value: PresetGit},
			want:  `"@git"`,
		},
		{
			name:  "script keeps string",
			input: CommandRule{Kind: CommandRuleScript, Value: "/path/to/wrapper"},
			want:  `"/path/to/wrapper"`,
		},
		{
			name:  "unset becomes null",
			input: CommandRule{Kind: CommandRuleUnset},
			want:  "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.input.MarshalJSON()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if string(got) != tt.want {
				t.Errorf("got %q, want %q", string(got), tt.want)
			}
		})
	}
}

func Test_Config_Commands_Parse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		files   map[string]string
		want    map[string]CommandRule
		wantErr string
	}{
		{
			name: "parse all command rule types",
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
			want: map[string]CommandRule{
				"git":  {Kind: CommandRulePreset, Value: PresetGit},
				"rm":   {Kind: CommandRuleBlock, Value: ""},
				"npm":  {Kind: CommandRuleRaw, Value: ""},
				"curl": {Kind: CommandRuleScript, Value: "~/bin/curl-wrapper"},
			},
		},
		{
			name: "invalid command rule type is error",
			files: map[string]string{
				".agent-sandbox.json": `{
					"commands": {
						"git": 123
					}
				}`,
			},
			wantErr: "must be boolean or string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			workDir := t.TempDir()
			xdgConfigHome := t.TempDir()

			// Write project files
			for path, content := range tt.files {
				fullPath := filepath.Join(workDir, path)

				err := os.MkdirAll(filepath.Dir(fullPath), 0o750)
				if err != nil {
					t.Fatalf("failed to create dir: %v", err)
				}

				err = os.WriteFile(fullPath, []byte(content), 0o600)
				if err != nil {
					t.Fatalf("failed to write file: %v", err)
				}
			}

			input := LoadConfigInput{
				WorkDirOverride: workDir,
				Env: map[string]string{
					"XDG_CONFIG_HOME": xdgConfigHome,
				},
			}

			got, err := LoadConfig(input)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}

				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for cmdName, wantRule := range tt.want {
				gotRule, ok := got.Commands[cmdName]
				if !ok {
					t.Errorf("command %q not found in config", cmdName)

					continue
				}

				if gotRule.Kind != wantRule.Kind {
					t.Errorf("command %q: Kind got %v, want %v", cmdName, gotRule.Kind, wantRule.Kind)
				}

				if gotRule.Value != wantRule.Value {
					t.Errorf("command %q: Value got %q, want %q", cmdName, gotRule.Value, wantRule.Value)
				}
			}
		})
	}
}

func Test_Config_Commands_Merge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		globalFiles map[string]string
		files       map[string]string
		want        map[string]CommandRule
	}{
		{
			name: "project overrides global for same key",
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
			want: map[string]CommandRule{
				"git": {Kind: CommandRuleRaw, Value: ""},   // overridden by project
				"rm":  {Kind: CommandRuleBlock, Value: ""}, // kept from global
			},
		},
		{
			name:        "empty commands in override preserves base",
			globalFiles: map[string]string{},
			files: map[string]string{
				".agent-sandbox.json": `{
					"network": false
				}`,
			},
			want: map[string]CommandRule{
				"git": {Kind: CommandRulePreset, Value: PresetGit}, // from defaults
			},
		},
		{
			name: "project adds new command to defaults",
			files: map[string]string{
				".agent-sandbox.json": `{
					"commands": {
						"rm": false
					}
				}`,
			},
			want: map[string]CommandRule{
				"git": {Kind: CommandRulePreset, Value: PresetGit}, // from defaults
				"rm":  {Kind: CommandRuleBlock, Value: ""},         // added by project
			},
		},
		{
			name: "global overrides default, project overrides global",
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
			want: map[string]CommandRule{
				"git": {Kind: CommandRuleBlock, Value: ""}, // global overrode default
				"npm": {Kind: CommandRuleRaw, Value: ""},   // project overrode global
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			workDir := t.TempDir()
			xdgConfigHome := t.TempDir()

			// Write project files
			for path, content := range tt.files {
				fullPath := filepath.Join(workDir, path)

				err := os.MkdirAll(filepath.Dir(fullPath), 0o750)
				if err != nil {
					t.Fatalf("failed to create dir: %v", err)
				}

				err = os.WriteFile(fullPath, []byte(content), 0o600)
				if err != nil {
					t.Fatalf("failed to write file: %v", err)
				}
			}

			// Write global files
			for path, content := range tt.globalFiles {
				fullPath := filepath.Join(xdgConfigHome, path)

				err := os.MkdirAll(filepath.Dir(fullPath), 0o750)
				if err != nil {
					t.Fatalf("failed to create dir: %v", err)
				}

				err = os.WriteFile(fullPath, []byte(content), 0o600)
				if err != nil {
					t.Fatalf("failed to write file: %v", err)
				}
			}

			input := LoadConfigInput{
				WorkDirOverride: workDir,
				Env: map[string]string{
					"XDG_CONFIG_HOME": xdgConfigHome,
				},
			}

			got, err := LoadConfig(input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check all expected commands
			for cmdName, wantRule := range tt.want {
				gotRule, ok := got.Commands[cmdName]
				if !ok {
					t.Errorf("command %q not found in config", cmdName)

					continue
				}

				if gotRule.Kind != wantRule.Kind {
					t.Errorf("command %q: Kind got %v, want %v", cmdName, gotRule.Kind, wantRule.Kind)
				}

				if gotRule.Value != wantRule.Value {
					t.Errorf("command %q: Value got %q, want %q", cmdName, gotRule.Value, wantRule.Value)
				}
			}

			// Check no unexpected commands
			for cmdName := range got.Commands {
				if _, ok := tt.want[cmdName]; !ok {
					t.Errorf("unexpected command %q in config", cmdName)
				}
			}
		})
	}
}

func Test_MergeConfigs_Concatenates_Filesystem_Arrays(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		base     Config
		override Config
		want     FilesystemConfig
	}{
		{
			name: "ro arrays are concatenated",
			base: Config{
				Filesystem: FilesystemConfig{
					Ro: []string{"/base/path1", "/base/path2"},
				},
			},
			override: Config{
				Filesystem: FilesystemConfig{
					Ro: []string{"/override/path1"},
				},
			},
			want: FilesystemConfig{
				Ro: []string{"/base/path1", "/base/path2", "/override/path1"},
			},
		},
		{
			name: "rw arrays are concatenated",
			base: Config{
				Filesystem: FilesystemConfig{
					Rw: []string{"/base/rw"},
				},
			},
			override: Config{
				Filesystem: FilesystemConfig{
					Rw: []string{"/override/rw1", "/override/rw2"},
				},
			},
			want: FilesystemConfig{
				Rw: []string{"/base/rw", "/override/rw1", "/override/rw2"},
			},
		},
		{
			name: "exclude arrays are concatenated",
			base: Config{
				Filesystem: FilesystemConfig{
					Exclude: []string{"/base/exclude"},
				},
			},
			override: Config{
				Filesystem: FilesystemConfig{
					Exclude: []string{"/override/exclude"},
				},
			},
			want: FilesystemConfig{
				Exclude: []string{"/base/exclude", "/override/exclude"},
			},
		},
		{
			name: "presets arrays are concatenated",
			base: Config{
				Filesystem: FilesystemConfig{
					Presets: []string{"!@lint/go"},
				},
			},
			override: Config{
				Filesystem: FilesystemConfig{
					Presets: []string{"!@lint/python"},
				},
			},
			want: FilesystemConfig{
				Presets: []string{"!@lint/go", "!@lint/python"},
			},
		},
		{
			name: "empty override arrays do not affect base",
			base: Config{
				Filesystem: FilesystemConfig{
					Ro:      []string{"/base/ro"},
					Rw:      []string{"/base/rw"},
					Exclude: []string{"/base/exclude"},
					Presets: []string{"!@lint/go"},
				},
			},
			override: Config{
				Filesystem: FilesystemConfig{
					Ro:      nil,
					Rw:      nil,
					Exclude: nil,
					Presets: nil,
				},
			},
			want: FilesystemConfig{
				Ro:      []string{"/base/ro"},
				Rw:      []string{"/base/rw"},
				Exclude: []string{"/base/exclude"},
				Presets: []string{"!@lint/go"},
			},
		},
		{
			name: "order preserved: base first then override",
			base: Config{
				Filesystem: FilesystemConfig{
					Ro: []string{"/a", "/b"},
				},
			},
			override: Config{
				Filesystem: FilesystemConfig{
					Ro: []string{"/c", "/d"},
				},
			},
			want: FilesystemConfig{
				Ro: []string{"/a", "/b", "/c", "/d"},
			},
		},
		{
			name: "all arrays concatenated in single merge",
			base: Config{
				Filesystem: FilesystemConfig{
					Presets: []string{"!@lint/ts"},
					Ro:      []string{"/ro1"},
					Rw:      []string{"/rw1"},
					Exclude: []string{"/ex1"},
				},
			},
			override: Config{
				Filesystem: FilesystemConfig{
					Presets: []string{"!@lint/go"},
					Ro:      []string{"/ro2"},
					Rw:      []string{"/rw2"},
					Exclude: []string{"/ex2"},
				},
			},
			want: FilesystemConfig{
				Presets: []string{"!@lint/ts", "!@lint/go"},
				Ro:      []string{"/ro1", "/ro2"},
				Rw:      []string{"/rw1", "/rw2"},
				Exclude: []string{"/ex1", "/ex2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := mergeConfigs(&tt.base, &tt.override)

			if !slicesEqual(got.Filesystem.Presets, tt.want.Presets) {
				t.Errorf("Presets: got %v, want %v", got.Filesystem.Presets, tt.want.Presets)
			}

			if !slicesEqual(got.Filesystem.Ro, tt.want.Ro) {
				t.Errorf("Ro: got %v, want %v", got.Filesystem.Ro, tt.want.Ro)
			}

			if !slicesEqual(got.Filesystem.Rw, tt.want.Rw) {
				t.Errorf("Rw: got %v, want %v", got.Filesystem.Rw, tt.want.Rw)
			}

			if !slicesEqual(got.Filesystem.Exclude, tt.want.Exclude) {
				t.Errorf("Exclude: got %v, want %v", got.Filesystem.Exclude, tt.want.Exclude)
			}
		})
	}
}
