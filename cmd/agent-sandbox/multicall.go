// Multicall dispatcher for command wrappers inside the sandbox.
//
// When a command like "git" is wrapped, the agent-sandbox binary is mounted
// over the real git binary. When git is invoked, argv[0] is "git" but the
// actual binary is agent-sandbox. The multicall dispatcher detects this
// (argv[0] != "agent-sandbox") and routes to the appropriate handler.
//
// # Runtime Layout
//
// Inside the sandbox, the runtime is mounted at /run/agent-sandbox:
//
//	/run/agent-sandbox/
//	├── agent-sandbox     # the agent-sandbox binary itself
//	├── bin/              # real binaries (e.g., bin/git)
//	└── policies/         # wrapper scripts or preset markers
//
// # Policy Files
//
// Each wrapped command has a policy file at policies/<cmd>. The content
// determines how the command is handled:
//
//   - "preset:<name>\n": Built-in preset (e.g., "preset:git\n")
//   - Otherwise: Executable script to run
//
// # Nested Sandbox Support
//
// When running a sandbox inside another sandbox, the inner sandbox mounts
// the outer's runtime at /run/agent-sandbox/outer:
//
//	┌─────────────────────────────────────────────────┐
//	│ OUTER SANDBOX                                   │
//	│   /run/agent-sandbox/                           │
//	│     ├── policies/git    ← outer's git policy   │
//	│     └── bin/git                                 │
//	│                                                 │
//	│   ┌─────────────────────────────────────────┐   │
//	│   │ INNER SANDBOX                           │   │
//	│   │   /run/agent-sandbox/                   │   │
//	│   │     ├── policies/rm  ← inner's policy   │   │
//	│   │     └── outer/       ← mount of outer   │   │
//	│   │           ├── policies/git              │   │
//	│   │           └── bin/git                   │   │
//	│   └─────────────────────────────────────────┘   │
//	└─────────────────────────────────────────────────┘
//
// The dispatcher searches inner first, then outer. Since inner sandboxes
// cannot override outer policies (filtered by filterNestedCommandRules),
// outer policies are effectively inherited.
//
// # Dispatch Flow
//
//  1. Read /run/agent-sandbox/policies/<cmd>
//  2. If content starts with "preset:" → run built-in preset
//  3. Otherwise → run as script
//  4. Repeat for /run/agent-sandbox/outer/... (nested only)
//  5. If nothing found → error

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var multicallOuterRuntimeRoot = filepath.Join(agentSandboxRuntimeRoot, "outer")

func runMulticall(ctx context.Context, cmdName string, cmdArgs []string, stdin io.Reader, stdout, stderr io.Writer, env map[string]string) error {
	for _, root := range multicallRuntimeRoots() {
		policy := filepath.Join(root, "policies", cmdName)

		content, err := os.ReadFile(policy)
		if err != nil {
			continue
		}

		if strings.HasPrefix(string(content), "preset:") {
			presetName := strings.TrimPrefix(strings.TrimSpace(string(content)), "preset:")
			if presetName == "git" && cmdName == "git" {
				return runGitPreset(ctx, root, cmdArgs, stdin, stdout, stderr)
			}

			return fmt.Errorf("%s: command not available", cmdName)
		}

		realBinary := filepath.Join(root, "bin", cmdName)

		// Block-only policies do not mount a real binary; allow wrapper execution
		// in that case by clearing AGENT_SANDBOX_REAL when the file is missing.
		_, statErr := os.Stat(realBinary)
		if statErr != nil {
			if !os.IsNotExist(statErr) {
				return fmt.Errorf("statting %q: %w", realBinary, statErr)
			}

			realBinary = ""
		}

		return runPolicy(ctx, &policyRunInput{
			policyPath: policy,
			cmdName:    cmdName,
			realBinary: realBinary,
			cmdArgs:    cmdArgs,
			stdin:      stdin,
			stdout:     stdout,
			stderr:     stderr,
			env:        env,
		})
	}

	return fmt.Errorf("%s: command not available", cmdName)
}

type policyRunInput struct {
	policyPath string
	cmdName    string
	realBinary string
	cmdArgs    []string
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
	env        map[string]string
}

func runPolicy(ctx context.Context, config *policyRunInput) error {
	cmd, err := newExecCmd(config.policyPath, config.cmdArgs)
	if err != nil {
		return fmt.Errorf("running wrapper command %s: %w", config.cmdName, err)
	}

	cmd.Stdin = config.stdin
	cmd.Stdout = config.stdout
	cmd.Stderr = config.stderr

	cmd.Env = envMapToSlice(config.env)
	cmd.Env = append(cmd.Env,
		"AGENT_SANDBOX_CMD="+config.cmdName,
		"AGENT_SANDBOX_REAL="+config.realBinary,
	)

	err = runCommandWithContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("running wrapper command %s: %w", config.cmdName, err)
	}

	return nil
}

func runGitPreset(ctx context.Context, runtimeRoot string, cmdArgs []string, stdin io.Reader, stdout, stderr io.Writer) error {
	realBinary := filepath.Join(runtimeRoot, "bin", "git")

	_, err := os.Stat(realBinary)
	if err != nil {
		return errors.New("git: command not available")
	}

	subcommand, subcommandArgs := parseGitArgs(cmdArgs)

	err = isBlockedGitOperation(subcommand, subcommandArgs)
	if err != nil {
		return err
	}

	cmd, err := newExecCmd(realBinary, cmdArgs)
	if err != nil {
		return err
	}

	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err = runCommandWithContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("running git: %w", err)
	}

	return nil
}

func newExecCmd(path string, args []string) (*exec.Cmd, error) {
	if path == "" {
		return nil, errors.New("missing command path")
	}

	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("command path must be absolute: %s", path)
	}

	cmd := &exec.Cmd{
		Path: path,
		Args: append([]string{path}, args...),
	}

	return cmd, nil
}

func runCommandWithContext(ctx context.Context, cmd *exec.Cmd) error {
	if ctx == nil {
		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("running command: %w", err)
		}

		return nil
	}

	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("starting command: %w", err)
	}

	done := make(chan error, 1)

	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("waiting for command: %w", err)
		}

		return nil
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}

		err := <-done
		if err != nil {
			return fmt.Errorf("waiting for command: %w", err)
		}

		return fmt.Errorf("command cancelled: %w", ctx.Err())
	}
}

func envMapToSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		if k == "" {
			continue
		}

		out = append(out, k+"="+v)
	}

	return out
}

// parseGitArgs extracts the git subcommand, skipping global flags like -C.
func parseGitArgs(args []string) (string, []string) {
	flagsWithValue := map[string]bool{
		"-C": true, "-c": true, "--git-dir": true, "--work-tree": true,
		"--namespace": true, "--super-prefix": true, "--config-env": true,
		"--exec-path": true, "--html-path": true, "--man-path": true,
		"--info-path": true, "--list-cmds": true, "--attr-source": true,
		"-p": true, "--paginate": false, "-P": false, "--no-pager": false,
		"--no-replace-obj": false, "--bare": false, "--literal-pathspecs": false,
		"--glob-pathspecs": false, "--noglob-pathspecs": false,
		"--icase-pathspecs": false, "--no-optional-locks": false,
	}

	i := 0
	for i < len(args) {
		arg := args[i]

		if !strings.HasPrefix(arg, "-") {
			return arg, args[i+1:]
		}

		if strings.Contains(arg, "=") {
			flagName := strings.SplitN(arg, "=", 2)[0]
			if _, known := flagsWithValue[flagName]; known || strings.HasPrefix(flagName, "--") {
				i++

				continue
			}
		}

		if takesValue, known := flagsWithValue[arg]; known && takesValue {
			i += 2

			continue
		}

		i++
	}

	return "", nil
}

func isBlockedGitOperation(subcommand string, args []string) error {
	inTemp, err := isInTempDir()
	if err != nil {
		return err
	}

	if inTemp {
		return nil
	}

	switch subcommand {
	case "checkout":
		return errors.New("git checkout blocked: can discard uncommitted changes; use 'git switch' for branches")
	case "restore":
		return errors.New("git restore blocked: discards uncommitted changes; commit or stash first")
	case "reset":
		if hasFlag(args, "--hard") {
			return errors.New("git reset --hard blocked: discards commits and changes; use 'git reset --soft' or 'git revert'")
		}
	case "clean":
		if hasFlag(args, "-f", "--force") {
			return errors.New("git clean -f blocked: deletes untracked files; review manually")
		}
	case "commit":
		if hasFlag(args, "--no-verify", "-n") {
			return errors.New("git commit --no-verify blocked: bypasses safety hooks; fix the hook issues")
		}
	case "stash":
		if len(args) > 0 {
			switch args[0] {
			case "drop":
				return errors.New("git stash drop blocked: permanently deletes stash; keep stashes or export first")
			case "clear":
				return errors.New("git stash clear blocked: deletes all stashes; export important stashes first")
			case "pop":
				return errors.New("git stash pop blocked: can cause merge conflicts that lose stash; use 'git stash apply'")
			}
		}
	case "branch":
		if hasFlag(args, "-D") {
			return errors.New("git branch -D blocked: force deletes unmerged branch; use 'git branch -d' (safe delete)")
		}
	case "push":
		if hasFlag(args, "--force", "-f") && !hasFlag(args, "--force-with-lease") {
			return errors.New("git push --force blocked: rewrites remote history; use 'git push --force-with-lease'")
		}
	}

	return nil
}

// multicallRuntimeRoots returns the runtime directories to search for policies,
// in priority order: inner sandbox first, then outer (if nested).
//
// In a nested sandbox, /run/agent-sandbox/outer contains the outer sandbox's
// runtime. We search inner first so inner-specific policies take precedence,
// but since filterNestedCommandRules prevents inner from overriding outer
// policies, outer policies are effectively inherited.
func multicallRuntimeRoots() []string {
	roots := []string{agentSandboxRuntimeRoot}

	_, statErr := os.Stat(multicallOuterRuntimeRoot)
	if statErr == nil {
		roots = append(roots, multicallOuterRuntimeRoot)
	}

	return roots
}

func hasFlag(args []string, flags ...string) bool {
	flagSet := make(map[string]bool, len(flags))
	for _, f := range flags {
		flagSet[f] = true
	}

	for _, arg := range args {
		if idx := strings.Index(arg, "="); idx > 0 {
			arg = arg[:idx]
		}

		if flagSet[arg] {
			return true
		}
	}

	return false
}

// isInTempDir checks if the current working directory is inside /tmp.
//
// The sandbox normalizes the host's temp directory to /tmp (via Config.TempDir),
// so we only need to check /tmp here. Agents can still manipulate TMPDIR, but it
// doesn't matter since we check against the hardcoded /tmp path, not os.TempDir().
func isInTempDir() (bool, error) {
	pwd, err := os.Getwd()
	if err != nil {
		return false, fmt.Errorf("getting working directory: %w", err)
	}

	pwdReal, err := filepath.EvalSymlinks(pwd)
	if err != nil {
		return false, fmt.Errorf("resolving working directory: %w", err)
	}

	tmpReal, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		return false, fmt.Errorf("resolving /tmp: %w", err)
	}

	return strings.HasPrefix(pwdReal, tmpReal), nil
}
