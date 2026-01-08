package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tailscale/hujson"
)

// ErrDuplicateConfigFiles is returned when both .json and .jsonc config files exist.
var ErrDuplicateConfigFiles = errors.New("duplicate config files")

// Config holds the application configuration.
type Config struct {
	Network    *bool            `json:"network,omitempty"`
	Docker     *bool            `json:"docker,omitempty"`
	Filesystem FilesystemConfig `json:"filesystem"`

	// Resolved (not serialized)
	EffectiveCwd string `json:"-"`
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
	} else {
		// Load project config
		projectConfigBasePath := filepath.Join(workDir, ".agent-sandbox")

		projectConfigPath, findErr := findConfigFile(projectConfigBasePath, false)
		if findErr == nil {
			projectCfg, loadErr := loadConfigFile(projectConfigPath)
			if loadErr == nil {
				cfg = mergeConfigs(&cfg, &projectCfg)
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
func mergeConfigs(base, override *Config) Config {
	result := *base

	if override.Network != nil {
		result.Network = override.Network
	}

	if override.Docker != nil {
		result.Docker = override.Docker
	}

	// Merge filesystem config
	if len(override.Filesystem.Presets) > 0 {
		result.Filesystem.Presets = override.Filesystem.Presets
	}

	if len(override.Filesystem.Ro) > 0 {
		result.Filesystem.Ro = override.Filesystem.Ro
	}

	if len(override.Filesystem.Rw) > 0 {
		result.Filesystem.Rw = override.Filesystem.Rw
	}

	if len(override.Filesystem.Exclude) > 0 {
		result.Filesystem.Exclude = override.Filesystem.Exclude
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
