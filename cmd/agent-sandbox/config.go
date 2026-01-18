package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
	"github.com/tailscale/hujson"
)

// LoadConfigInput holds the inputs for LoadConfig.
type LoadConfigInput struct {
	WorkDirOverride string
	ConfigPath      string
	EnvVars         map[string]string
	CLIFlags        *pflag.FlagSet
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

	// Source-tracked filesystem paths for correct debug output labeling.
	// These are populated during config loading to preserve path sources.
	GlobalFilesystem  FilesystemConfig `json:"-"`
	ProjectFilesystem FilesystemConfig `json:"-"`
	CLIFilesystem     FilesystemConfig `json:"-"`
}

// FilesystemConfig holds filesystem access rules.
type FilesystemConfig struct {
	Presets []string `json:"presets,omitempty"`
	Ro      []string `json:"ro,omitempty"`
	Rw      []string `json:"rw,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

// CommandRuleKind represents the type of command wrapper rule.
type CommandRuleKind int

const (
	// CommandRuleExplicitAllow allows the command to run without any wrapper (true in config).
	// Useful to override a block from a previous config layer (e.g., project config
	// sets "rm": true to re-enable a command blocked by global config).
	CommandRuleExplicitAllow CommandRuleKind = iota + 1
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

const errInvalidCommandPresetMessage = "command preset can only be used for its matching command"

// UnmarshalJSON implements custom JSON unmarshaling for commandRule.
// Accepts boolean or string values as per spec.
func (r *CommandRule) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return fmt.Errorf("command rule must be boolean or string: got %s", string(data))
	}

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

	var boolVal bool

	err = json.Unmarshal(data, &boolVal)
	if err == nil {
		if boolVal {
			r.Kind = CommandRuleExplicitAllow
		} else {
			r.Kind = CommandRuleBlock
		}

		r.Value = ""

		return nil
	}

	return fmt.Errorf("command rule must be boolean or string: got %s", string(data))
}

// MarshalJSON implements custom JSON marshaling for commandRule.
func (r CommandRule) MarshalJSON() ([]byte, error) {
	var val any

	switch r.Kind {
	case CommandRuleExplicitAllow:
		val = true
	case CommandRuleBlock:
		val = false
	case CommandRulePreset, CommandRuleScript:
		val = r.Value
	default:
		val = nil
	}

	data, err := json.Marshal(val)
	if err != nil {
		return nil, fmt.Errorf("marshaling command rule: %w", err)
	}

	return data, nil
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
	workDir := input.WorkDirOverride
	if workDir == "" {
		var err error

		workDir, err = os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("cannot get working directory: %w", err)
		}
	}

	if !filepath.IsAbs(workDir) {
		cwd, err := os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("cannot get working directory: %w", err)
		}

		workDir = filepath.Join(cwd, workDir)
	}

	cfg := DefaultConfig()
	cfg.LoadedConfigFiles = make(map[string]string)

	globalConfigBasePath, err := getUserConfigBasePath(input.EnvVars)
	if err != nil {
		return Config{}, err
	}

	if globalConfigBasePath != "" {
		globalConfigPath, findErr := findConfigFile(globalConfigBasePath, false)
		if findErr == nil {
			globalCfg, loadErr := parseConfigFile(globalConfigPath)
			if loadErr != nil {
				return Config{}, loadErr
			}
			// Store global filesystem paths separately for source tracking
			cfg.GlobalFilesystem = globalCfg.Filesystem
			cfg = mergeConfigs(&cfg, &globalCfg)
			cfg.LoadedConfigFiles["global"] = globalConfigPath
		} else if !errors.Is(findErr, os.ErrNotExist) {
			return Config{}, findErr
		}
		// If os.ErrNotExist, silently skip (per spec)
	}

	if input.ConfigPath != "" {
		configPath := input.ConfigPath
		if !filepath.IsAbs(configPath) {
			configPath = filepath.Join(workDir, configPath)
		}

		explicitCfg, parseErr := parseConfigFile(configPath)
		if parseErr != nil {
			return Config{}, parseErr
		}

		// Store explicit config filesystem paths as "project" for source tracking
		cfg.ProjectFilesystem = explicitCfg.Filesystem
		cfg = mergeConfigs(&cfg, &explicitCfg)
		cfg.LoadedConfigFiles["explicit"] = configPath
	} else {
		projectConfigBasePath := filepath.Join(workDir, ".agent-sandbox")

		projectConfigPath, findErr := findConfigFile(projectConfigBasePath, false)
		if findErr == nil {
			projectCfg, loadErr := parseConfigFile(projectConfigPath)
			if loadErr != nil {
				return Config{}, loadErr
			}
			// Store project filesystem paths separately for source tracking
			cfg.ProjectFilesystem = projectCfg.Filesystem
			cfg = mergeConfigs(&cfg, &projectCfg)
			cfg.LoadedConfigFiles["project"] = projectConfigPath
		} else if !errors.Is(findErr, os.ErrNotExist) {
			return Config{}, findErr
		}
		// If os.ErrNotExist, silently skip (per spec: project config is optional)
	}

	cfg.EffectiveCwd = workDir

	if input.CLIFlags != nil {
		err = applyCLIFlags(&cfg, input.CLIFlags)
		if err != nil {
			return Config{}, err
		}
	}

	err = validateCommandRules(cfg.Commands)
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// --- internal helpers ---

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	t, f := true, false

	return Config{
		Network: &t,
		Docker:  &f,
		Commands: map[string]CommandRule{
			"git": {Kind: CommandRulePreset, Value: "@git"},
		},
	}
}

// applyCLIFlags applies CLI flag overrides to the config.
// This is the final layer of config merging (highest precedence).
func applyCLIFlags(cfg *Config, flags *pflag.FlagSet) error {
	if flags.Changed("network") {
		val, _ := flags.GetBool("network")
		cfg.Network = &val
	}

	if flags.Changed("docker") {
		val, _ := flags.GetBool("docker")
		cfg.Docker = &val
	}

	// Extract and store CLI filesystem paths for source tracking
	var ro, rw, exclude []string
	if flags.Changed("ro") {
		ro, _ = flags.GetStringArray("ro")
	}

	if flags.Changed("rw") {
		rw, _ = flags.GetStringArray("rw")
	}

	if flags.Changed("exclude") {
		exclude, _ = flags.GetStringArray("exclude")
	}

	cfg.CLIFilesystem = FilesystemConfig{Ro: ro, Rw: rw, Exclude: exclude}
	cfg.Filesystem.Ro = append(cfg.Filesystem.Ro, ro...)
	cfg.Filesystem.Rw = append(cfg.Filesystem.Rw, rw...)
	cfg.Filesystem.Exclude = append(cfg.Filesystem.Exclude, exclude...)

	if flags.Changed("cmd") {
		cmdVals, _ := flags.GetStringArray("cmd")

		if cfg.Commands == nil {
			cfg.Commands = make(map[string]CommandRule)
		}

		for _, v := range cmdVals {
			err := parseCmdFlag(cfg.Commands, v)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// parseCmdFlag parses a single --cmd KEY=VALUE flag and adds it to the commands map.
// Supports comma-separated values: --cmd git=true,rm=false.
func parseCmdFlag(commands map[string]CommandRule, value string) error {
	pairs := strings.SplitSeq(value, ",")

	for pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}

		key, val, ok := strings.Cut(pair, "=")
		if !ok {
			return fmt.Errorf("invalid --cmd format: expected KEY=VALUE, got %q", pair)
		}

		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		if key == "" {
			return fmt.Errorf("invalid --cmd format: empty key in %q", pair)
		}

		var rule CommandRule

		switch val {
		case "true":
			rule = CommandRule{Kind: CommandRuleExplicitAllow}
		case "false":
			rule = CommandRule{Kind: CommandRuleBlock}
		default:
			if strings.HasPrefix(val, "@") {
				rule = CommandRule{Kind: CommandRulePreset, Value: val}
			} else {
				rule = CommandRule{Kind: CommandRuleScript, Value: val}
			}
		}

		commands[key] = rule
	}

	return nil
}

// validateCommandRules checks that command presets are used correctly.
// For example, @git can only be used with the "git" command.
func validateCommandRules(commands map[string]CommandRule) error {
	for cmdName, rule := range commands {
		if rule.Kind != CommandRulePreset {
			continue
		}

		presetCmd := strings.TrimPrefix(rule.Value, "@")
		if cmdName != presetCmd {
			return fmt.Errorf("%s: %s preset can only be used for '%s' command, not '%s'",
				errInvalidCommandPresetMessage, rule.Value, presetCmd, cmdName)
		}
	}

	return nil
}

// findConfigFile finds a config file at the given base path.
// It checks for both .json and .jsonc extensions and returns an error if both exist.
// basePath should be either the full path (for --config) or the directory + base name without extension.
func findConfigFile(basePath string, isExplicitPath bool) (string, error) {
	if isExplicitPath {
		_, err := os.Stat(basePath)
		if err != nil {
			return "", fmt.Errorf("config file %s: %w", basePath, err)
		}

		return basePath, nil
	}

	jsonPath := basePath + ".json"
	jsoncPath := basePath + ".jsonc"

	jsonExists, jsonErr := fileExists(jsonPath)
	jsoncExists, jsoncErr := fileExists(jsoncPath)

	if jsonErr != nil && !errors.Is(jsonErr, os.ErrNotExist) {
		return "", fmt.Errorf("checking %s: %w", jsonPath, jsonErr)
	}

	if jsoncErr != nil && !errors.Is(jsoncErr, os.ErrNotExist) {
		return "", fmt.Errorf("checking %s: %w", jsoncPath, jsoncErr)
	}

	if jsonExists && jsoncExists {
		return "", fmt.Errorf("duplicate config files found: both %s and %s exist; remove one", jsonPath, jsoncPath)
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

// parseConfigFile loads and parses a JSON/JSONC config file.
// Both .json and .jsonc files support comments via hujson.
// Returns an error if the config contains unknown fields.
func parseConfigFile(path string) (Config, error) {
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

	decoder := json.NewDecoder(bytes.NewReader(standardized))
	decoder.DisallowUnknownFields()

	err = decoder.Decode(&cfg)
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
