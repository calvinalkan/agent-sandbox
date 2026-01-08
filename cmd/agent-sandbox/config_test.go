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
			name: "filesystem arrays from project override global",
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
					Ro: []string{"/project/ro"},
					Rw: []string{"/global/rw"}, // kept from global
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
