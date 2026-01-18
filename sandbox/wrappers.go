//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// This file implements command wrapper planning.
//
// Wrappers are implemented using bwrap's `--ro-bind-data`, which allows mounting
// file content provided via an inherited file descriptor. In this library we do
// two-phase construction:
//
//  1. Planning phase (buildPlan): discover wrapper targets on the host (based on
//     PATH) and compute what should be injected.
//  2. Execution phase (Sandbox.Command): create backing files (typically memfd
//     on Linux), write script contents, and attach them to the exec.Cmd via
//     ExtraFiles.
//
// The planner intentionally does not open files/FDs. It only reads wrapper
// scripts from disk (for script mode) and produces deterministic mount intent.

// roBindDataMount describes a single `--ro-bind-data` injection.
//
// dst is an absolute sandbox path to mount over.
// perms is the mode for the injected file (typically 0555 for executable scripts).
// data is the file content written to an inherited FD at execution time.
type roBindDataMount struct {
	dst   string
	data  string
	perms os.FileMode
}

// commandWrapperPlan is the deterministic wrapper intent derived from Config.
//
// The plan is created once per Sandbox (cached in plan) and then used by
// Command() to allocate per-invocation resources (pipes/Fds).
type commandWrapperPlan struct {
	// dirs are created via `--dir` and must be absolute paths inside the sandbox.
	dirs []Mount

	// realBinaryMounts are concrete `--ro-bind` mounts that expose the selected
	// "real" binary at a deterministic path (e.g. `{MountPath}/bin/git`).
	realBinaryMounts []Mount

	// launcherMounts are concrete `--ro-bind` mounts that overlay discovered
	// command targets with a multicall launcher binary.
	launcherMounts []Mount

	// dataMounts are per-command `--ro-bind-data` mounts that are materialized at
	// runtime using exec.Cmd.ExtraFiles.
	dataMounts []roBindDataMount
}

// isEmpty returns true if the plan has no mounts to apply.
func (p *commandWrapperPlan) isEmpty() bool {
	return len(p.dirs) == 0 && len(p.realBinaryMounts) == 0 && len(p.launcherMounts) == 0 && len(p.dataMounts) == 0
}

// buildCommandWrapperPlan computes wrapper mounts for the configured command rules.
//
// Wrapper discovery is PATH-based: for each configured command name, it searches
// each unique PATH directory (in order) for an executable file named <cmd>.
// Symlinks are resolved and deduplicated by resolved target path.
func buildCommandWrapperPlan(cmdsCfg Commands, env Environment, paths pathResolver, debugf Debugf) (*commandWrapperPlan, error) {
	if len(cmdsCfg.Block) == 0 && len(cmdsCfg.Wrappers) == 0 {
		return &commandWrapperPlan{}, nil
	}

	mountDir := cmdsCfg.MountPath
	if mountDir == "" {
		// Auto-derive from launcher basename.
		//
		// Launcher is required when wrappers/blocking are configured and is
		// validated during Sandbox construction; reaching an empty Launcher here is
		// an internal invariant violation.
		if cmdsCfg.Launcher == "" {
			return nil, internalErrorf("buildCommandWrapperPlan", "commands configured but Launcher is empty")
		}

		mountDir = "/run/" + filepath.Base(cmdsCfg.Launcher)
	}

	allCmdNames := make([]string, 0, len(cmdsCfg.Block)+len(cmdsCfg.Wrappers))

	allCmdNames = append(allCmdNames, cmdsCfg.Block...)
	for name := range cmdsCfg.Wrappers {
		allCmdNames = append(allCmdNames, name)
	}

	sort.Strings(allCmdNames)

	if debugf != nil {
		debugf("wrappers: blocked=%d wrapped=%d", len(cmdsCfg.Block), len(cmdsCfg.Wrappers))
	}

	pathVar := ""
	if env.HostEnv != nil {
		pathVar = env.HostEnv["PATH"]
	}

	if strings.TrimSpace(pathVar) == "" {
		return nil, fmt.Errorf("cannot apply command wrappers: PATH is empty (commands: %s)", strings.Join(allCmdNames, ", "))
	}

	pathDirs := parsePathDirs(pathVar, env.WorkDir)
	if len(pathDirs) == 0 {
		return nil, fmt.Errorf("cannot apply command wrappers: PATH has no usable entries (commands: %s)", strings.Join(allCmdNames, ", "))
	}

	if debugf != nil {
		debugf("wrappers: PATH=%q dirs=%d", pathVar, len(pathDirs))
	}

	plan := &commandWrapperPlan{}

	needRunDir := false
	needWrappersDir := false
	needRealDir := false

	denyScript := generateDenyWrapperScript()

	for _, cmdName := range cmdsCfg.Block {
		if strings.TrimSpace(cmdName) == "" || strings.Contains(cmdName, "/") {
			return nil, internalErrorf("buildCommandWrapperPlan", "invalid blocked command name %q", cmdName)
		}

		targets, err := findCommandTargets(cmdName, pathDirs)
		if err != nil {
			return nil, fmt.Errorf("discover command targets for blocked %q: %w", cmdName, err)
		}

		if len(targets) == 0 {
			return nil, fmt.Errorf("cannot block command %q: command not found in PATH %q", cmdName, pathVar)
		}

		if debugf != nil {
			debugf("wrappers: blocked %q targets=%d", cmdName, len(targets))
		}

		needRunDir = true
		needWrappersDir = true

		wrapperDst := filepath.Join(mountDir, "wrappers", cmdName)
		plan.dataMounts = append(plan.dataMounts, roBindDataMount{dst: wrapperDst, perms: 0o555, data: denyScript})

		// Track target basenames that differ from cmdName (e.g., bunx -> bun).
		// We need wrapper markers for these too, so the multicall dispatcher
		// recognizes invocations by the target name.
		seenTargetNames := make(map[string]bool)
		seenTargetNames[cmdName] = true

		for _, dst := range targets {
			plan.launcherMounts = append(plan.launcherMounts, RoBind(cmdsCfg.Launcher, dst))

			// If the resolved target has a different basename, create an alias
			// wrapper marker so the multicall dispatcher blocks it too.
			targetName := filepath.Base(dst)
			if !seenTargetNames[targetName] {
				seenTargetNames[targetName] = true
				aliasWrapperDst := filepath.Join(mountDir, "wrappers", targetName)
				plan.dataMounts = append(plan.dataMounts, roBindDataMount{dst: aliasWrapperDst, perms: 0o555, data: denyScript})
			}
		}
	}

	wrapperNames := make([]string, 0, len(cmdsCfg.Wrappers))
	for name := range cmdsCfg.Wrappers {
		wrapperNames = append(wrapperNames, name)
	}

	sort.Strings(wrapperNames)

	for _, cmdName := range wrapperNames {
		wrapper := cmdsCfg.Wrappers[cmdName]

		if strings.TrimSpace(cmdName) == "" || strings.Contains(cmdName, "/") {
			return nil, internalErrorf("buildCommandWrapperPlan", "invalid wrapper command name %q", cmdName)
		}

		targets, err := findCommandTargets(cmdName, pathDirs)
		if err != nil {
			return nil, fmt.Errorf("discover command targets for wrapper %q: %w", cmdName, err)
		}

		if len(targets) == 0 {
			return nil, fmt.Errorf("cannot wrap command %q: command not found in PATH %q", cmdName, pathVar)
		}

		if debugf != nil {
			debugf("wrappers: wrapped %q targets=%d", cmdName, len(targets))
		}

		var contents string

		switch {
		case strings.TrimSpace(wrapper.InlineScript) != "":
			contents = wrapper.InlineScript
		case strings.TrimSpace(wrapper.Path) != "":
			scriptHostPath := paths.Resolve(wrapper.Path)

			info, err := os.Stat(scriptHostPath)
			if err != nil {
				return nil, fmt.Errorf("stat wrapper script %q for %q: %w", scriptHostPath, cmdName, err)
			}

			if info.IsDir() {
				return nil, fmt.Errorf("wrapper script %q for %q is a directory", scriptHostPath, cmdName)
			}

			data, err := os.ReadFile(scriptHostPath)
			if err != nil {
				return nil, fmt.Errorf("read wrapper script %q for %q: %w", scriptHostPath, cmdName, err)
			}

			contents = string(data)
		default:
			return nil, internalErrorf("buildCommandWrapperPlan", "wrapper %q has empty Path and InlineScript", cmdName)
		}

		needRunDir = true
		needWrappersDir = true
		needRealDir = true

		// The user-provided wrapper script is mounted into the sandbox and then
		// the launcher is mounted over each real binary location.
		wrapperDst := filepath.Join(mountDir, "wrappers", cmdName)
		plan.dataMounts = append(plan.dataMounts, roBindDataMount{dst: wrapperDst, perms: 0o555, data: contents})

		// Wrappers always expose the real binary.
		realBinaryDst := filepath.Join(mountDir, "bin", cmdName)
		// Use the first PATH match as the canonical "real" binary.
		plan.realBinaryMounts = append(plan.realBinaryMounts, RoBind(targets[0], realBinaryDst))

		// Track target basenames that differ from cmdName (e.g., bunx -> bun).
		// We need wrapper markers for these too, so the multicall dispatcher
		// recognizes invocations by the target name.
		seenTargetNames := make(map[string]bool)
		seenTargetNames[cmdName] = true

		for _, dst := range targets {
			plan.launcherMounts = append(plan.launcherMounts, RoBind(cmdsCfg.Launcher, dst))

			// If the resolved target has a different basename (e.g., bunx symlink
			// resolves to bun), create an alias wrapper marker so the multicall
			// dispatcher recognizes invocations as "bun" too.
			targetName := filepath.Base(dst)
			if !seenTargetNames[targetName] {
				seenTargetNames[targetName] = true
				aliasWrapperDst := filepath.Join(mountDir, "wrappers", targetName)
				plan.dataMounts = append(plan.dataMounts, roBindDataMount{dst: aliasWrapperDst, perms: 0o555, data: contents})

				// Also expose the real binary under the alias name.
				aliasRealDst := filepath.Join(mountDir, "bin", targetName)
				plan.realBinaryMounts = append(plan.realBinaryMounts, RoBind(targets[0], aliasRealDst))
			}
		}
	}

	if len(plan.dataMounts) == 0 && len(plan.realBinaryMounts) == 0 && len(plan.launcherMounts) == 0 {
		return &commandWrapperPlan{}, nil
	}

	// Ensure directories exist inside /run tmpfs.
	//
	// We chmod these runtime directories to search-only (0111) to prevent easy
	// discovery via directory listing (e.g. `ls {mountDir}/bin`).
	const runtimeDirPerms = 0o111

	if needRunDir || needWrappersDir || needRealDir {
		plan.dirs = append(plan.dirs, Dir(mountDir, runtimeDirPerms))
	}

	if needRealDir {
		plan.dirs = append(plan.dirs, Dir(filepath.Join(mountDir, "bin"), runtimeDirPerms))
	}

	if needWrappersDir {
		plan.dirs = append(plan.dirs, Dir(filepath.Join(mountDir, "wrappers"), runtimeDirPerms))
	}

	return plan, nil
}

// parsePathDirs splits PATH into a de-duplicated list of absolute host directories.
//
// Empty PATH entries (meaning "current directory") are ignored for wrapper
// discovery.
func parsePathDirs(pathVar, workDir string) []string {
	parts := strings.Split(pathVar, ":")
	seen := make(map[string]struct{})
	out := make([]string, 0, len(parts))

	for _, dir := range parts {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}

		if !filepath.IsAbs(dir) {
			dir = filepath.Join(workDir, dir)
		}

		dir = filepath.Clean(dir)
		if dir == "" {
			continue
		}

		if _, ok := seen[dir]; ok {
			continue
		}

		seen[dir] = struct{}{}
		out = append(out, dir)
	}

	return out
}

// findCommandTargets returns host absolute paths that should be wrapped for cmdName.
//
// Targets are returned in PATH order and deduplicated by resolved symlink target.
func findCommandTargets(cmdName string, pathDirs []string) ([]string, error) {
	seen := make(map[string]struct{})
	out := make([]string, 0, 4)

	for _, dir := range pathDirs {
		candidate := filepath.Join(dir, cmdName)

		info, err := os.Stat(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, fmt.Errorf("stat %q: %w", candidate, err)
		}

		if info.IsDir() {
			continue
		}

		if info.Mode()&0o111 == 0 {
			continue
		}

		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, fmt.Errorf("resolve symlinks %q: %w", candidate, err)
		}

		resolved = filepath.Clean(resolved)
		if resolved == "" {
			continue
		}

		if _, ok := seen[resolved]; ok {
			continue
		}

		seen[resolved] = struct{}{}
		out = append(out, resolved)
	}

	return out, nil
}

// generateDenyWrapperScript returns an executable script that denies the command.
func generateDenyWrapperScript() string {
	return `#!/bin/sh
echo "command '$(basename "$0")' is blocked in this sandbox" >&2
exit 1
`
}
