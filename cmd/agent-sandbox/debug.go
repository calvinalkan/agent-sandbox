package main

import (
	"fmt"
	"io"
	"strings"
)

type DebugLogger struct {
	output    io.Writer
	phase     string
	lastPhase string
}

func NewDebugLogger(output io.Writer) *DebugLogger {
	return &DebugLogger{output: output}
}

func (d *DebugLogger) Enabled() bool {
	return d.output != nil
}

func (d *DebugLogger) Phase(name string) {
	if d.output == nil {
		return
	}

	trimmed := strings.TrimSpace(name)
	trimmed = strings.ToLower(trimmed)
	trimmed = strings.ReplaceAll(trimmed, " ", "-")
	trimmed = strings.ReplaceAll(trimmed, "_", "-")
	trimmed = strings.Trim(trimmed, "-")

	if trimmed != "" && trimmed != d.lastPhase {
		if d.lastPhase != "" {
			_, _ = fmt.Fprintln(d.output)
		}

		heading := phaseHeading(trimmed)
		if heading != "" {
			_, _ = fmt.Fprintln(d.output, heading)
		}
	}

	d.phase = trimmed
	d.lastPhase = trimmed
}

var phaseHeadings = map[string]string{
	"bwrap-args":       "bwrap Arguments",
	"command-wrappers": "Command Wrappers",
	"config-loading":   "Config Loading",
}

func phaseHeading(phase string) string {
	if heading, ok := phaseHeadings[phase]; ok {
		return heading
	}

	parts := strings.Split(phase, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}

		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}

	return strings.Join(parts, " ")
}

func (d *DebugLogger) Logf(format string, args ...any) {
	if d.output == nil {
		return
	}

	if strings.HasPrefix(format, "sandbox(") || strings.HasPrefix(format, "sandbox:") || strings.HasPrefix(format, "sandbox ") {
		_, _ = fmt.Fprintf(d.output, format+"\n", args...)

		return
	}

	prefix := "cli: "
	if d.phase != "" {
		prefix = fmt.Sprintf("cli(%s): ", d.phase)
	}

	_, _ = fmt.Fprintf(d.output, prefix+format+"\n", args...)
}

func debugConfigLoading(debug *DebugLogger, cfg *Config) {
	if debug == nil || !debug.Enabled() {
		return
	}

	debug.Phase("config-loading")

	if len(cfg.LoadedConfigFiles) == 0 {
		debug.Logf("No config files loaded (using defaults)")

		return
	}

	if path, ok := cfg.LoadedConfigFiles["global"]; ok {
		debug.Logf("Global config: %s", path)
	} else {
		debug.Logf("Global config: (not found)")
	}

	if path, ok := cfg.LoadedConfigFiles["explicit"]; ok {
		debug.Logf("Explicit config (--config): %s", path)
	} else if path, ok := cfg.LoadedConfigFiles["project"]; ok {
		debug.Logf("Project config: %s", path)
	} else {
		debug.Logf("Project config: (not found)")
	}
}

type FlagChecker interface {
	Changed(name string) bool
}

func debugConfigMerge(debug *DebugLogger, cfg *Config, flags FlagChecker) {
	if debug == nil || !debug.Enabled() {
		return
	}

	debug.Phase("config-merge")

	networkSource := configSource(cfg.LoadedConfigFiles, "network", flags)

	networkVal := true
	if cfg.Network != nil {
		networkVal = *cfg.Network
	}

	debug.Logf("network: %t (%s)", networkVal, networkSource)

	dockerSource := configSource(cfg.LoadedConfigFiles, "docker", flags)

	dockerVal := false
	if cfg.Docker != nil {
		dockerVal = *cfg.Docker
	}

	debug.Logf("docker: %t (%s)", dockerVal, dockerSource)

	if len(cfg.Filesystem.Presets) > 0 {
		debug.Logf("filesystem.presets: %v", cfg.Filesystem.Presets)
	}

	if len(cfg.Commands) > 0 {
		debug.Logf("commands:")

		for cmd, rule := range cfg.Commands {
			debug.Logf("%s: %s", cmd, commandRuleDescription(rule))
		}
	}
}

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

func commandRuleDescription(rule CommandRule) string {
	switch rule.Kind {
	case CommandRuleExplicitAllow:
		return "raw (no wrapper)"
	case CommandRuleBlock:
		return "blocked"
	case CommandRulePreset:
		return rule.Value + " (built-in wrapper)"
	case CommandRuleScript:
		return rule.Value + " (custom script)"
	default:
		return "unknown"
	}
}

func DebugCommandWrappers(debug *DebugLogger, commands map[string]CommandRule) {
	if debug == nil || !debug.Enabled() {
		return
	}

	debug.Phase("command-wrappers")

	if len(commands) == 0 {
		debug.Logf("No command wrappers configured")

		return
	}

	for cmd, rule := range commands {
		debug.Logf("%s: %s", cmd, commandRuleDescription(rule))
	}
}

func DebugBwrapArgs(debug *DebugLogger, args []string) {
	if debug == nil || !debug.Enabled() {
		return
	}

	debug.Phase("bwrap-args")

	idx := 0
	for idx < len(args) {
		if strings.HasPrefix(args[idx], "--") {
			flagArg := args[idx]
			next := idx + 1

			for next < len(args) && !strings.HasPrefix(args[next], "--") {
				next++
			}

			line := append([]string{flagArg}, args[idx+1:next]...)
			debug.Logf("%s", strings.Join(line, " "))

			idx = next
		} else {
			debug.Logf("%s", args[idx])
			idx++
		}
	}
}

func debugVersion(debug *DebugLogger) {
	if debug == nil || !debug.Enabled() {
		return
	}

	debug.Phase("version")
	debug.Logf("%s", formatVersion())
}
