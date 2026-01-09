package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/tailscale/hujson"
)

// ErrDuplicateConfigFiles is returned when both .json and .jsonc config files exist.
var ErrDuplicateConfigFiles = errors.New("duplicate config files")

// ErrInvalidCommandRule is returned when a command rule has an invalid type.
var ErrInvalidCommandRule = errors.New("command rule must be boolean or string")

// CommandRuleKind represents the type of command wrapper rule.
type CommandRuleKind int

const (
	// CommandRuleUnset indicates no rule has been set.
	CommandRuleUnset CommandRuleKind = iota
	// CommandRuleRaw allows the command to run without any wrapper (true in config).
	CommandRuleRaw
	// CommandRuleBlock prevents the command from running (false in config).
	CommandRuleBlock
	// CommandRulePreset uses a built-in smart wrapper ("@git" in config).
	CommandRulePreset
	// CommandRuleScript uses a custom wrapper script ("/path/to/script" in config).
	CommandRuleScript
)

// CommandRule represents a command wrapper configuration.
// It can be a boolean (true = raw, false = block) or a string
// (starting with @ = preset, otherwise = script path).
type CommandRule struct {
	Kind  CommandRuleKind
	Value string // used for Preset (e.g., "@git") and Script (e.g., "/path/to/wrapper")
}

// UnmarshalJSON implements custom JSON unmarshaling for CommandRule.
// Accepts boolean or string values as per spec.
func (r *CommandRule) UnmarshalJSON(data []byte) error {
	// Check for null explicitly (json.Unmarshal would accept null for bool/string)
	if string(data) == "null" {
		return fmt.Errorf("%w: got %s", ErrInvalidCommandRule, string(data))
	}

	// Try string first (string must be quoted, bool cannot be)
	var strVal string

	err := json.Unmarshal(data, &strVal)
	if err == nil {
		if strings.HasPrefix(strVal, "@") {
			r.Kind = CommandRulePreset
			r.Value = strVal
		} else {
			r.Kind = CommandRuleScript
			r.Value = strVal
		}

		return nil
	}

	// Try boolean
	var boolVal bool

	err = json.Unmarshal(data, &boolVal)
	if err == nil {
		if boolVal {
			r.Kind = CommandRuleRaw
		} else {
			r.Kind = CommandRuleBlock
		}

		r.Value = ""

		return nil
	}

	return fmt.Errorf("%w: got %s", ErrInvalidCommandRule, string(data))
}

// MarshalJSON implements custom JSON marshaling for CommandRule.
func (r CommandRule) MarshalJSON() ([]byte, error) {
	var val any

	switch r.Kind {
	case CommandRuleUnset:
		val = nil
	case CommandRuleRaw:
		val = true
	case CommandRuleBlock:
		val = false
	case CommandRulePreset, CommandRuleScript:
		val = r.Value
	}

	data, err := json.Marshal(val)
	if err != nil {
		return nil, fmt.Errorf("marshaling command rule: %w", err)
	}

	return data, nil
}

// Config holds the application configuration.
type Config struct {
	Network    *bool                  `json:"network,omitempty"`
	Docker     *bool                  `json:"docker,omitempty"`
	Filesystem FilesystemConfig       `json:"filesystem"`
	Commands   map[string]CommandRule `json:"commands,omitempty"`

	// Resolved (not serialized)
	EffectiveCwd string `json:"-"`

	// LoadedConfigFiles tracks which config files were loaded (for debug output).
	// Key is the config type (global, project, explicit), value is the path.
	LoadedConfigFiles map[string]string `json:"-"`
}

// FilesystemConfig holds filesystem access rules.
type FilesystemConfig struct {
	Presets []string `json:"presets,omitempty"`
	Ro      []string `json:"ro,omitempty"`
	Rw      []string `json:"rw,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		Network: boolPtr(true),
		Docker:  boolPtr(false),
		Commands: map[string]CommandRule{
			"git": {Kind: CommandRulePreset, Value: "@git"},
		},
	}
}

func boolPtr(b bool) *bool {
	return &b
}

// LoadConfigInput holds the inputs for LoadConfig.
type LoadConfigInput struct {
	WorkDirOverride string            // -C/--cwd flag value; if empty, os.Getwd() is used
	ConfigPath      string            // --config flag value
	Env             map[string]string // Environment variables (for XDG_CONFIG_HOME)
}

// LoadConfig loads configuration with the following precedence (later overrides earlier):
//  1. Built-in defaults
//  2. Global config: $XDG_CONFIG_HOME/agent-sandbox/config.json or config.jsonc
//     (defaults to ~/.config/agent-sandbox/) - always loaded if exists
//  3. Project config OR --config path (not both):
//     - Without --config: .agent-sandbox.json or .agent-sandbox.jsonc in workDir
//     - With --config: uses that path instead of project config
//
// Both .json and .jsonc files support comments via tailscale/hujson.
// If both .json and .jsonc exist at the same location, it's an error.
func LoadConfig(input LoadConfigInput) (Config, error) {
	// Resolve effective working directory
	workDir := input.WorkDirOverride
	if workDir == "" {
		var err error

		workDir, err = os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("cannot get working directory: %w", err)
		}
	}

	// Make workDir absolute
	if !filepath.IsAbs(workDir) {
		cwd, err := os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("cannot get working directory: %w", err)
		}

		workDir = filepath.Join(cwd, workDir)
	}

	// Start with defaults
	cfg := DefaultConfig()
	cfg.LoadedConfigFiles = make(map[string]string)

	// Load global config (always loaded if exists)
	globalConfigBasePath, err := getUserConfigBasePath(input.Env)
	if err != nil {
		return Config{}, err
	}

	if globalConfigBasePath != "" {
		globalConfigPath, findErr := findConfigFile(globalConfigBasePath, false)
		if findErr == nil {
			globalCfg, loadErr := loadConfigFile(globalConfigPath)
			if loadErr == nil {
				cfg = mergeConfigs(&cfg, &globalCfg)
				cfg.LoadedConfigFiles["global"] = globalConfigPath
			} else {
				// File exists but is invalid - this is an error
				return Config{}, loadErr
			}
		} else if !errors.Is(findErr, os.ErrNotExist) {
			// Error finding config (e.g., both .json and .jsonc exist)
			return Config{}, findErr
		}
		// If os.ErrNotExist, silently skip (per spec)
	}

	// Load project config OR --config path (not both)
	if input.ConfigPath != "" {
		// Explicit --config path replaces project config
		configPath := input.ConfigPath
		if !filepath.IsAbs(configPath) {
			configPath = filepath.Join(workDir, configPath)
		}

		explicitCfg, err := loadConfigFile(configPath)
		if err != nil {
			return Config{}, err
		}

		cfg = mergeConfigs(&cfg, &explicitCfg)
		cfg.LoadedConfigFiles["explicit"] = configPath
	} else {
		// Load project config
		projectConfigBasePath := filepath.Join(workDir, ".agent-sandbox")

		projectConfigPath, findErr := findConfigFile(projectConfigBasePath, false)
		if findErr == nil {
			projectCfg, loadErr := loadConfigFile(projectConfigPath)
			if loadErr == nil {
				cfg = mergeConfigs(&cfg, &projectCfg)
				cfg.LoadedConfigFiles["project"] = projectConfigPath
			} else {
				// File exists but is invalid - this is an error
				return Config{}, loadErr
			}
		} else if !errors.Is(findErr, os.ErrNotExist) {
			// Error finding config (e.g., both .json and .jsonc exist)
			return Config{}, findErr
		}
		// If os.ErrNotExist, silently skip (per spec: project config is optional)
	}

	cfg.EffectiveCwd = workDir

	return cfg, nil
}

// findConfigFile finds a config file at the given base path.
// It checks for both .json and .jsonc extensions and returns an error if both exist.
// basePath should be either the full path (for --config) or the directory + base name without extension.
func findConfigFile(basePath string, isExplicitPath bool) (string, error) {
	if isExplicitPath {
		// Explicit --config path: use as-is
		_, err := os.Stat(basePath)
		if err != nil {
			return "", fmt.Errorf("config file %s: %w", basePath, err)
		}

		return basePath, nil
	}

	// Check for both .json and .jsonc
	jsonPath := basePath + ".json"
	jsoncPath := basePath + ".jsonc"

	jsonExists, jsonErr := fileExists(jsonPath)
	jsoncExists, jsoncErr := fileExists(jsoncPath)

	// Return errors that aren't "not found"
	if jsonErr != nil && !errors.Is(jsonErr, os.ErrNotExist) {
		return "", fmt.Errorf("checking %s: %w", jsonPath, jsonErr)
	}

	if jsoncErr != nil && !errors.Is(jsoncErr, os.ErrNotExist) {
		return "", fmt.Errorf("checking %s: %w", jsoncPath, jsoncErr)
	}

	if jsonExists && jsoncExists {
		return "", fmt.Errorf("%w: both %s and %s exist; remove one", ErrDuplicateConfigFiles, jsonPath, jsoncPath)
	}

	if jsonExists {
		return jsonPath, nil
	}

	if jsoncExists {
		return jsoncPath, nil
	}

	return "", os.ErrNotExist
}

// fileExists checks if a file exists and is not a directory.
// Returns (true, nil) if file exists, (false, nil) if not found,
// or (false, error) for other errors (e.g., permission denied).
func fileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}

		return false, fmt.Errorf("checking file %s: %w", path, err)
	}

	if info.IsDir() {
		return false, nil
	}

	return true, nil
}

// loadConfigFile loads and parses a JSON/JSONC config file.
// Both .json and .jsonc files support comments via hujson.
func loadConfigFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("reading config %s: %w", path, err)
	}

	// Standardize JSONC to JSON (handles comments in both .json and .jsonc)
	standardized, err := hujson.Standardize(data)
	if err != nil {
		return Config{}, fmt.Errorf("parsing config %s: %w", path, err)
	}

	var cfg Config

	err = json.Unmarshal(standardized, &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("parsing config %s: %w", path, err)
	}

	return cfg, nil
}

// mergeConfigs merges override into base, with override taking precedence.
// Empty/zero values in override do not override base values.
// Note: LoadedConfigFiles from base is preserved (caller updates it after merge).
func mergeConfigs(base, override *Config) Config {
	result := *base

	// Preserve LoadedConfigFiles map from base (it's updated by caller)
	if base.LoadedConfigFiles != nil {
		result.LoadedConfigFiles = base.LoadedConfigFiles
	}

	if override.Network != nil {
		result.Network = override.Network
	}

	if override.Docker != nil {
		result.Docker = override.Docker
	}

	// Merge filesystem config: arrays are concatenated per spec
	// Order matters: base paths first, then override paths (for specificity tie-breaking)
	result.Filesystem.Presets = append(result.Filesystem.Presets, override.Filesystem.Presets...)
	result.Filesystem.Ro = append(result.Filesystem.Ro, override.Filesystem.Ro...)
	result.Filesystem.Rw = append(result.Filesystem.Rw, override.Filesystem.Rw...)
	result.Filesystem.Exclude = append(result.Filesystem.Exclude, override.Filesystem.Exclude...)

	// Merge commands map (later values override earlier for same key)
	if len(override.Commands) > 0 {
		if result.Commands == nil {
			result.Commands = make(map[string]CommandRule)
		}

		maps.Copy(result.Commands, override.Commands)
	}

	return result
}

// getUserConfigBasePath returns the user config base path (without extension).
// Uses env map for XDG_CONFIG_HOME instead of os.Getenv().
func getUserConfigBasePath(env map[string]string) (string, error) {
	if xdg, ok := env["XDG_CONFIG_HOME"]; ok && xdg != "" {
		return filepath.Join(xdg, "agent-sandbox", "config"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}

	return filepath.Join(home, ".config", "agent-sandbox", "config"), nil
}
