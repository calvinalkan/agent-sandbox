package main

import (
	"fmt"
	"io"
	"strings"
)

// DebugLogger provides structured debug output for sandbox startup.
// It is disabled by default (when output is nil) and outputs to stderr when enabled.
type DebugLogger struct {
	output io.Writer
}

// NewDebugLogger creates a new debug logger.
// If output is nil, the logger is disabled and all methods are no-ops.
func NewDebugLogger(output io.Writer) *DebugLogger {
	return &DebugLogger{output: output}
}

// Enabled returns true if debug logging is enabled.
func (d *DebugLogger) Enabled() bool {
	return d.output != nil
}

// Section outputs a section header.
func (d *DebugLogger) Section(name string) {
	if d.output == nil {
		return
	}

	_, _ = fmt.Fprintf(d.output, "\n=== %s ===\n", name)
}

// Logf outputs a formatted debug message.
func (d *DebugLogger) Logf(format string, args ...any) {
	if d.output == nil {
		return
	}

	_, _ = fmt.Fprintf(d.output, format+"\n", args...)
}

// Bulletf outputs an indented bullet point item.
func (d *DebugLogger) Bulletf(format string, args ...any) {
	if d.output == nil {
		return
	}

	_, _ = fmt.Fprintf(d.output, "  • "+format+"\n", args...)
}

// Path outputs a path resolution entry with original, resolved, access, and source.
func (d *DebugLogger) Path(original, resolved string, access PathAccess, source PathSource) {
	if d.output == nil {
		return
	}

	if original == resolved {
		_, _ = fmt.Fprintf(d.output, "  %s [%s] (from %s)\n", resolved, access, source)
	} else {
		_, _ = fmt.Fprintf(d.output, "  %s -> %s [%s] (from %s)\n", original, resolved, access, source)
	}
}

// ConfigFile outputs information about a config file.
func (d *DebugLogger) ConfigFile(label, path string, loaded bool) {
	if d.output == nil {
		return
	}

	if loaded {
		_, _ = fmt.Fprintf(d.output, "  %s: %s\n", label, path)
	} else {
		_, _ = fmt.Fprintf(d.output, "  %s: (not found)\n", label)
	}
}

// BoolSetting outputs a boolean setting value with its source.
func (d *DebugLogger) BoolSetting(name string, value bool, source string) {
	if d.output == nil {
		return
	}

	_, _ = fmt.Fprintf(d.output, "  %s: %t (%s)\n", name, value, source)
}

// PresetList outputs a list of presets (applied or removed).
func (d *DebugLogger) PresetList(label string, presets []string) {
	if d.output == nil {
		return
	}

	if len(presets) == 0 {
		_, _ = fmt.Fprintf(d.output, "  %s: (none)\n", label)
	} else {
		_, _ = fmt.Fprintf(d.output, "  %s: %s\n", label, strings.Join(presets, ", "))
	}
}

// CommandWrapper outputs information about a command wrapper configuration.
func (d *DebugLogger) CommandWrapper(cmd string, rule CommandRule) {
	if d.output == nil {
		return
	}

	var desc string

	switch rule.Kind {
	case CommandRuleUnset:
		desc = "(not configured)"
	case CommandRuleRaw:
		desc = "raw (no wrapper)"
	case CommandRuleBlock:
		desc = "blocked"
	case CommandRulePreset:
		desc = rule.Value + " (built-in wrapper)"
	case CommandRuleScript:
		desc = rule.Value + " (custom script)"
	}

	_, _ = fmt.Fprintf(d.output, "  %s: %s\n", cmd, desc)
}

// BwrapArg outputs a bwrap argument for debugging.
func (d *DebugLogger) BwrapArg(args ...string) {
	if d.output == nil {
		return
	}

	// Format: --flag value
	_, _ = fmt.Fprintf(d.output, "  %s\n", strings.Join(args, " "))
}

// BwrapArgs outputs multiple bwrap arguments for debugging.
// Arguments are grouped by flag (e.g., --ro-bind src dest becomes one line).
func (d *DebugLogger) BwrapArgs(args []string) {
	if d.output == nil {
		return
	}

	// Group arguments by flag for readability
	idx := 0
	for idx < len(args) {
		if strings.HasPrefix(args[idx], "--") {
			// Find how many values follow this flag
			flagArg := args[idx]
			next := idx + 1

			for next < len(args) && !strings.HasPrefix(args[next], "--") {
				next++
			}

			// Output flag with its values
			line := append([]string{flagArg}, args[idx+1:next]...)
			_, _ = fmt.Fprintf(d.output, "  %s\n", strings.Join(line, " "))
			idx = next
		} else {
			_, _ = fmt.Fprintf(d.output, "  %s\n", args[idx])
			idx++
		}
	}
}

// debugConfigLoading outputs debug information about config file loading.
func debugConfigLoading(debug *DebugLogger, cfg *Config) {
	if !debug.Enabled() {
		return
	}

	debug.Section("Config Loading")

	// Output which config files were loaded
	if len(cfg.LoadedConfigFiles) == 0 {
		debug.Logf("  No config files loaded (using defaults)")

		return
	}

	if path, ok := cfg.LoadedConfigFiles["global"]; ok {
		debug.ConfigFile("Global config", path, true)
	} else {
		debug.ConfigFile("Global config", "", false)
	}

	if path, ok := cfg.LoadedConfigFiles["explicit"]; ok {
		debug.ConfigFile("Explicit config (--config)", path, true)
	} else if path, ok := cfg.LoadedConfigFiles["project"]; ok {
		debug.ConfigFile("Project config", path, true)
	} else {
		debug.ConfigFile("Project config", "", false)
	}
}

// FlagChecker is an interface for checking if CLI flags were set.
type FlagChecker interface {
	Changed(name string) bool
}

// debugConfigMerge outputs debug information about the final merged config.
// This is called after CLI flags have been applied.
func debugConfigMerge(debug *DebugLogger, cfg *Config, flags FlagChecker) {
	if !debug.Enabled() {
		return
	}

	debug.Section("Config Merge")

	// Network setting
	networkSource := configSource(cfg.LoadedConfigFiles, "network", flags)
	networkVal := true

	if cfg.Network != nil {
		networkVal = *cfg.Network
	}

	debug.BoolSetting("network", networkVal, networkSource)

	// Docker setting
	dockerSource := configSource(cfg.LoadedConfigFiles, "docker", flags)
	dockerVal := false

	if cfg.Docker != nil {
		dockerVal = *cfg.Docker
	}

	debug.BoolSetting("docker", dockerVal, dockerSource)

	// Filesystem presets
	if len(cfg.Filesystem.Presets) > 0 {
		debug.Logf("  filesystem.presets: %v", cfg.Filesystem.Presets)
	}

	// Command wrappers
	if len(cfg.Commands) > 0 {
		debug.Logf("  commands:")

		for cmd, rule := range cfg.Commands {
			debug.CommandWrapper(cmd, rule)
		}
	}
}

// configSource determines the source of a config value.
func configSource(loadedFiles map[string]string, fieldName string, flags FlagChecker) string {
	if flags != nil && flags.Changed(fieldName) {
		return "cli"
	}

	if _, ok := loadedFiles["explicit"]; ok {
		return "explicit config"
	}

	if _, ok := loadedFiles["project"]; ok {
		return "project config"
	}

	if _, ok := loadedFiles["global"]; ok {
		return "global config"
	}

	return "default"
}

// DebugPresetExpansion outputs debug information about preset expansion.
// It shows which presets were applied and which were removed via negation.
func DebugPresetExpansion(debug *DebugLogger, presets []string, applied, removed []string) {
	if !debug.Enabled() {
		return
	}

	debug.Section("Preset Expansion")

	if len(presets) > 0 {
		debug.Logf("  Input presets: %v", presets)
	} else {
		debug.Logf("  Input presets: (none, using default @all)")
	}

	debug.PresetList("Applied presets", applied)
	debug.PresetList("Removed presets", removed)
}

// DebugPathResolution outputs debug information about path resolution.
// It shows original patterns mapped to resolved absolute paths with access levels.
func DebugPathResolution(debug *DebugLogger, paths []ResolvedPath) {
	if !debug.Enabled() {
		return
	}

	debug.Section("Path Resolution")

	if len(paths) == 0 {
		debug.Logf("  No paths resolved")

		return
	}

	for _, resolvedPath := range paths {
		debug.Path(resolvedPath.Original, resolvedPath.Resolved, resolvedPath.Access, resolvedPath.Source)
	}
}

// DebugCommandWrappers outputs debug information about command wrapper setup.
// It shows how each command is configured (blocked, wrapped, or raw).
func DebugCommandWrappers(debug *DebugLogger, commands map[string]CommandRule) {
	if !debug.Enabled() {
		return
	}

	debug.Section("Command Wrappers")

	if len(commands) == 0 {
		debug.Logf("  No command wrappers configured")

		return
	}

	for cmd, rule := range commands {
		debug.CommandWrapper(cmd, rule)
	}
}

// DebugBwrapArgs outputs debug information about generated bwrap arguments.
// It shows the final bwrap command line arguments that will be used.
func DebugBwrapArgs(debug *DebugLogger, args []string) {
	if !debug.Enabled() {
		return
	}

	debug.Section("Generated bwrap Arguments")
	debug.BwrapArgs(args)
}
