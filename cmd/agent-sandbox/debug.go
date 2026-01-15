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

		_, _ = fmt.Fprintln(d.output, trimmed)
	}

	d.phase = trimmed
	d.lastPhase = trimmed
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

type FlagChecker interface {
	Changed(name string) bool
}

func (d *DebugLogger) Config(cfg *Config, flags FlagChecker) {
	if d == nil || !d.Enabled() {
		return
	}

	d.Phase("config-load")

	if len(cfg.LoadedConfigFiles) == 0 {
		d.Logf("No config files loaded (using defaults)")
	} else {
		if path, ok := cfg.LoadedConfigFiles["global"]; ok {
			d.Logf("Global config: %s", path)
		} else {
			d.Logf("Global config: (not found)")
		}

		if path, ok := cfg.LoadedConfigFiles["explicit"]; ok {
			d.Logf("Explicit config (--config): %s", path)
		} else if path, ok := cfg.LoadedConfigFiles["project"]; ok {
			d.Logf("Project config: %s", path)
		} else {
			d.Logf("Project config: (not found)")
		}
	}

	d.Phase("config-merge")

	networkSource := _configSource(cfg.LoadedConfigFiles, "network", flags)

	networkVal := true
	if cfg.Network != nil {
		networkVal = *cfg.Network
	}

	d.Logf("network: %t (%s)", networkVal, networkSource)

	dockerSource := _configSource(cfg.LoadedConfigFiles, "docker", flags)

	dockerVal := false
	if cfg.Docker != nil {
		dockerVal = *cfg.Docker
	}

	d.Logf("docker: %t (%s)", dockerVal, dockerSource)

	if len(cfg.Filesystem.Presets) > 0 {
		d.Logf("filesystem.presets: %v", cfg.Filesystem.Presets)
	}

	if len(cfg.Commands) > 0 {
		d.Logf("commands:")

		for cmd, rule := range cfg.Commands {
			d.Logf("%s: %s", cmd, _commandRuleDescription(rule))
		}
	}
}

func (d *DebugLogger) LogSandboxCommand(commands map[string]CommandRule, args []string) {
	if d == nil || !d.Enabled() {
		return
	}

	d._logCommandWrappers(commands)
	d._logBwrapArgs(args)
}

func (d *DebugLogger) Version() {
	if d == nil || !d.Enabled() {
		return
	}

	d.Phase("version")
	d.Logf("%s", formatVersion())
}

// Internal helper functions

func _configSource(loadedFiles map[string]string, fieldName string, flags FlagChecker) string {
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

func _commandRuleDescription(rule CommandRule) string {
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

func (d *DebugLogger) _logCommandWrappers(commands map[string]CommandRule) {
	d.Phase("command-wrappers")

	if len(commands) == 0 {
		d.Logf("No command wrappers configured")

		return
	}

	for cmd, rule := range commands {
		d.Logf("%s: %s", cmd, _commandRuleDescription(rule))
	}
}

func (d *DebugLogger) _logBwrapArgs(args []string) {
	d.Phase("bwrap-args")

	idx := 0
	for idx < len(args) {
		if strings.HasPrefix(args[idx], "--") {
			flagArg := args[idx]
			next := idx + 1

			for next < len(args) && !strings.HasPrefix(args[next], "--") {
				next++
			}

			line := append([]string{flagArg}, args[idx+1:next]...)
			d.Logf("%s", strings.Join(line, " "))

			idx = next
		} else {
			d.Logf("%s", args[idx])
			idx++
		}
	}
}
