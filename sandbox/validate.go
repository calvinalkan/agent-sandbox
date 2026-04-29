//go:build linux

package sandbox

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// validateConfigAndEnv validates user-controlled configuration and environment.
//
// This function is the primary "input boundary" for the sandbox package. The
// rest of the implementation assumes that validated fields satisfy their basic
// invariants (non-empty, absolute paths where required, known enum values, etc.).
// Internal code assumes these invariants; any violation indicates a bug and is
// surfaced as an error from Sandbox methods.
func validateConfigAndEnv(cfg *Config, env Environment) error {
	errs := make([]error, 0, 5)

	errs = append(errs, validateEnvironment(env)...)
	errs = append(errs, validateBaseFS(cfg.BaseFS)...)
	errs = append(errs, validateProcess(cfg.Process)...)
	errs = append(errs, validatePresetNames(cfg.Filesystem.Presets)...)
	errs = append(errs, validateMounts(cfg.Filesystem.Mounts)...)
	errs = append(errs, validateCommandsConfig(cfg.Commands)...)

	return errors.Join(errs...)
}

func validateEnvironment(env Environment) []error {
	var errs []error

	// Environment is part of the public API; keep invariants strict to simplify
	// downstream planning.
	if strings.TrimSpace(env.WorkDir) == "" {
		errs = append(errs, errors.New("environment WorkDir is empty"))
	} else if !filepath.IsAbs(env.WorkDir) {
		errs = append(errs, fmt.Errorf("environment WorkDir %q is not absolute", env.WorkDir))
	}

	if strings.TrimSpace(env.HomeDir) == "" {
		errs = append(errs, errors.New("environment HomeDir is empty"))
	} else if !filepath.IsAbs(env.HomeDir) {
		errs = append(errs, fmt.Errorf("environment HomeDir %q is not absolute", env.HomeDir))
	}

	return errs
}

func validateBaseFS(mode BaseFS) []error {
	if mode == "" {
		return nil
	}

	switch mode {
	case BaseFSHost, BaseFSEmpty:
		return nil
	default:
		return []error{fmt.Errorf("unknown root mode %q", mode)}
	}
}

func validateProcess(process Process) []error {
	var errs []error

	added := make(map[Capability]struct{}, len(process.AddCaps))
	dropped := make(map[Capability]struct{}, len(process.DropCaps))

	for i, capability := range process.AddCaps {
		errs = append(errs, validateCapability(capability, "AddCaps", i)...)
		if _, exists := added[capability]; exists {
			errs = append(errs, fmt.Errorf("process AddCaps[%d] %q is duplicated", i, capability))
		}

		added[capability] = struct{}{}
	}

	for i, capability := range process.DropCaps {
		errs = append(errs, validateCapability(capability, "DropCaps", i)...)
		if _, exists := dropped[capability]; exists {
			errs = append(errs, fmt.Errorf("process DropCaps[%d] %q is duplicated", i, capability))
		}

		if _, exists := added[capability]; exists {
			errs = append(errs, fmt.Errorf("process capability %q cannot appear in both AddCaps and DropCaps", capability))
		}

		dropped[capability] = struct{}{}
	}

	return errs
}

func validateCapability(capability Capability, field string, index int) []error {
	raw := string(capability)

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []error{fmt.Errorf("process %s[%d] is empty", field, index)}
	}

	if raw != trimmed {
		return []error{fmt.Errorf("process %s[%d] %q must not contain leading or trailing whitespace", field, index, raw)}
	}

	for _, r := range raw {
		if unicode.IsSpace(r) {
			return []error{fmt.Errorf("process %s[%d] %q must not contain whitespace", field, index, raw)}
		}
	}

	// The SDK intentionally requires canonical uppercase capability spellings
	// to keep validation simple and deterministic.
	if raw == "ALL" {
		return nil
	}

	if !strings.HasPrefix(raw, "CAP_") {
		return []error{fmt.Errorf("process %s[%d] %q must start with CAP_", field, index, raw)}
	}

	if len(raw) == len("CAP_") {
		return []error{fmt.Errorf("process %s[%d] %q must include a capability name after CAP_", field, index, raw)}
	}

	for _, r := range raw[4:] {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}

		return []error{fmt.Errorf("process %s[%d] %q contains invalid character %q", field, index, raw, r)}
	}

	return nil
}

func validatePresetNames(presets []string) []error {
	// Preset names are pure syntax; validate early.
	_, err := resolvePresetToggles(presets)
	if err != nil {
		return []error{err}
	}

	return nil
}

func validateMounts(mounts []Mount) []error {
	var errs []error

	for i, mount := range mounts {
		// Depth is stored internally as int16 for ordering. Validate early so
		// downstream code can treat overflow as impossible.
		if strings.TrimSpace(mount.Dst) != "" {
			cleaned := filepath.Clean(mount.Dst)

			depth := 0
			if cleaned != "/" {
				depth = strings.Count(cleaned, "/")
			}

			if depth > 32767 {
				errs = append(errs, fmt.Errorf("mount %d destination %q is too deeply nested (%d)", i, mount.Dst, depth))
			}
		}

		switch mount.Kind {
		case MountReadOnly, MountReadOnlyTry, MountReadWrite, MountReadWriteTry, MountExclude, MountExcludeTry, MountExcludeFile, MountExcludeDir:
			if strings.TrimSpace(mount.Dst) == "" {
				errs = append(errs, fmt.Errorf("mount %d has empty destination", i))

				continue
			}

			if mount.Kind == MountExcludeFile || mount.Kind == MountExcludeDir {
				if strings.ContainsAny(mount.Dst, "*?[") {
					errs = append(errs, fmt.Errorf("mount %d (%s) does not accept glob patterns", i, mountKindName(mount.Kind)))
				}
			}

			if mount.Src != "" {
				errs = append(errs, fmt.Errorf("mount %d (%s) does not accept a source path", i, mountKindName(mount.Kind)))
			}

			if mount.FD != 0 || mount.Perms != 0 {
				errs = append(errs, fmt.Errorf("mount %d (%s) does not accept FD/Perms", i, mountKindName(mount.Kind)))
			}

		case MountRoBind, MountRoBindTry, MountBind, MountBindTry, MountDevBind, MountDevBindTry:
			if strings.TrimSpace(mount.Dst) == "" {
				errs = append(errs, fmt.Errorf("mount %d (%s) has empty destination", i, mountKindName(mount.Kind)))

				break
			}

			if !filepath.IsAbs(mount.Dst) {
				errs = append(errs, fmt.Errorf("mount %d (%s) destination %q is not absolute", i, mountKindName(mount.Kind), mount.Dst))
			}

			if strings.TrimSpace(mount.Src) == "" {
				errs = append(errs, fmt.Errorf("mount %d (%s) requires a source path", i, mountKindName(mount.Kind)))

				break
			}

			if !filepath.IsAbs(mount.Src) {
				errs = append(errs, fmt.Errorf("mount %d (%s) source %q is not absolute", i, mountKindName(mount.Kind), mount.Src))
			}

		case MountTmpfs, MountDir:
			if strings.TrimSpace(mount.Dst) == "" {
				errs = append(errs, fmt.Errorf("mount %d (%s) has empty destination", i, mountKindName(mount.Kind)))

				break
			}

			if !filepath.IsAbs(mount.Dst) {
				errs = append(errs, fmt.Errorf("mount %d (%s) destination %q is not absolute", i, mountKindName(mount.Kind), mount.Dst))
			}

			if mount.Src != "" {
				errs = append(errs, fmt.Errorf("mount %d (%s) does not accept a source path", i, mountKindName(mount.Kind)))
			}

		case MountRoBindData:
			if strings.TrimSpace(mount.Dst) == "" {
				errs = append(errs, fmt.Errorf("mount %d (%s) has empty destination", i, mountKindName(mount.Kind)))

				break
			}

			if !filepath.IsAbs(mount.Dst) {
				errs = append(errs, fmt.Errorf("mount %d (%s) destination %q is not absolute", i, mountKindName(mount.Kind), mount.Dst))
			}

			if mount.Src != "" {
				errs = append(errs, fmt.Errorf("mount %d (%s) does not accept a source path", i, mountKindName(mount.Kind)))
			}

			if mount.FD <= 0 {
				errs = append(errs, fmt.Errorf("mount %d (%s) requires a positive FD", i, mountKindName(mount.Kind)))
			}

		default:
			errs = append(errs, fmt.Errorf("mount %d has unknown kind %d", i, mount.Kind))
		}
	}

	return errs
}

func validateCommandLauncher(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return errors.New("command wrappers require Launcher to be set")
	}

	if !filepath.IsAbs(trimmed) {
		return fmt.Errorf("command Launcher %q is not absolute", trimmed)
	}

	info, err := os.Stat(trimmed)
	if err != nil {
		return fmt.Errorf("command Launcher %q: %w", trimmed, err)
	}

	if info.IsDir() {
		return fmt.Errorf("command Launcher %q is a directory", trimmed)
	}

	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("command Launcher %q is not executable", trimmed)
	}

	return nil
}

func validateCommandsConfig(cmdsCfg Commands) []error {
	var errs []error

	hasCommands := len(cmdsCfg.Block) > 0 || len(cmdsCfg.Wrappers) > 0

	if hasCommands {
		err := validateCommandLauncher(cmdsCfg.Launcher)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if cmdsCfg.MountPath != "" && !filepath.IsAbs(cmdsCfg.MountPath) {
		errs = append(errs, fmt.Errorf("command MountPath %q is not absolute", cmdsCfg.MountPath))
	}

	for _, cmdName := range cmdsCfg.Block {
		if strings.TrimSpace(cmdName) == "" {
			errs = append(errs, errors.New("blocked command has empty name"))

			continue
		}

		if strings.Contains(cmdName, "/") {
			errs = append(errs, fmt.Errorf("blocked command %q is invalid: command names must not contain '/'", cmdName))
		}
	}

	for cmdName, wrapper := range cmdsCfg.Wrappers {
		if strings.TrimSpace(cmdName) == "" {
			errs = append(errs, errors.New("wrapper has empty command name"))

			continue
		}

		if strings.Contains(cmdName, "/") {
			errs = append(errs, fmt.Errorf("wrapper %q is invalid: command names must not contain '/'", cmdName))

			continue
		}

		hasPath := strings.TrimSpace(wrapper.Path) != ""

		hasInline := strings.TrimSpace(wrapper.InlineScript) != ""
		if !hasPath && !hasInline {
			errs = append(errs, fmt.Errorf("wrapper %q: Path or InlineScript is required", cmdName))
		}
	}

	return errs
}
