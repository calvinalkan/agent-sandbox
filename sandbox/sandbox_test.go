//go:build linux

package sandbox_test

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/calvinalkan/agent-sandbox/sandbox"
)

const (
	firstExtraFileFD     = 3
	testLauncherPath     = "/bin/true"
	testRuntimeMountPath = "/run/agent-sandbox"
)

func Test_Sandbox_CommandWrappers_BuildMounts_When_Configured(t *testing.T) {
	t.Parallel()

	t.Run("Adds_RoBindData_When_DenyRule_Configured", func(t *testing.T) {
		t.Parallel()

		env := newTestEnv(t, testEnvConfig{
			Block: []string{"rm"},
		})

		rmPath := env.mustWriteBinFile(t, "rm", []byte("#!/bin/sh\nexit 0\n"))

		cmd := env.mustCommand(t, "rm", "-rf", "/")

		if got := len(cmd.ExtraFiles); got != 1 {
			t.Fatalf("expected 1 ExtraFile, got %d", got)
		}

		// ELF launcher is mounted at target path, deny script at runtime path
		mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", rmPath})
		mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), "/run/agent-sandbox/wrappers/rm"})
	})

	t.Run("Mounts_Wrapper_And_Real_Binary_When_ScriptRule_Configured", func(t *testing.T) {
		t.Parallel()

		workDir := t.TempDir()
		homeDir := t.TempDir()

		binDir := filepath.Join(workDir, "bin")
		mustCreateDir(t, binDir)

		npmPath := filepath.Join(binDir, "npm")
		mustWriteFile(t, npmPath, []byte("#!/bin/sh\nexit 0\n"), 0o755)

		wrapperHost := filepath.Join(workDir, "npm-wrapper.sh")
		mustWriteFile(t, wrapperHost, []byte("#!/bin/sh\necho wrapper\n"), 0o644)

		s := mustNewSandboxWithPathCommands(t, workDir, homeDir, binDir, map[string]sandbox.Wrapper{
			"npm": sandbox.Wrap(wrapperHost),
		})

		cmd, cleanup, err := s.Command(t.Context(), []string{"npm", "--version"})
		if err != nil {
			t.Fatalf("Command: %v", err)
		}

		defer func() { _ = cleanup() }()

		// With ELF launcher: 1 ExtraFile (script content), launcher mounted at target
		if got := len(cmd.ExtraFiles); got != 1 {
			t.Fatalf("expected 1 ExtraFile, got %d", got)
		}

		mustContainSubsequence(t, cmd.Args, []string{"--dir", "/run/agent-sandbox"})
		mustContainSubsequence(t, cmd.Args, []string{"--dir", "/run/agent-sandbox/bin"})
		mustContainSubsequence(t, cmd.Args, []string{"--dir", "/run/agent-sandbox/wrappers"})
		mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", npmPath, "/run/agent-sandbox/bin/npm"})
		mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), "/run/agent-sandbox/wrappers/npm"})
		mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", npmPath})
	})

	t.Run("Avoids_Deadlock_When_WrapperScript_Large", func(t *testing.T) {
		t.Parallel()

		workDir := t.TempDir()
		homeDir := t.TempDir()

		binDir := filepath.Join(workDir, "bin")
		mustCreateDir(t, binDir)

		npmPath := filepath.Join(binDir, "npm")
		mustWriteFile(t, npmPath, []byte("#!/bin/sh\nexit 0\n"), 0o755)

		wrapperHost := filepath.Join(workDir, "npm-wrapper.sh")
		big := bytes.Repeat([]byte("x"), 1024*1024)
		data := append([]byte("#!/bin/sh\n"), big...)
		mustWriteFile(t, wrapperHost, data, 0o644)

		env := sandbox.Environment{
			HomeDir: homeDir,
			WorkDir: workDir,
			HostEnv: map[string]string{"PATH": binDir},
		}

		cfg := sandbox.Config{
			Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
			Commands: sandbox.Commands{
				Wrappers: map[string]sandbox.Wrapper{
					"npm": sandbox.Wrap(wrapperHost),
				},
				Launcher: "/bin/true",
			},
		}

		s := mustNewSandbox(t, &cfg, env)

		errCh := make(chan error, 1)

		go func() {
			_, cleanup, err := s.Command(t.Context(), []string{"npm", "--version"})
			if err == nil {
				_ = cleanup()
			}

			errCh <- err
		}()

		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("Command: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Command hung (possible ro-bind-data pipe deadlock)")
		}
	})

	t.Run("Returns_Error_When_Path_Missing", func(t *testing.T) {
		t.Parallel()

		env := newTestEnv(t, testEnvConfig{
			Block:       []string{"rm"},
			IncludePath: boolPtr(false),
		})

		_, err := sandbox.NewWithEnvironment(&env.cfg, env.env)
		if err == nil {
			t.Fatal("expected NewWithEnvironment to fail when PATH is missing")
		}

		if !strings.Contains(err.Error(), "PATH") {
			t.Fatalf("expected error to mention PATH, got: %v", err)
		}
	})
}

// ============================================================================
// Command wrapper PATH discovery and mount behavior
// ============================================================================

func Test_Sandbox_CommandWrappers_Skip_Mounts_When_Commands_Are_Nil(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testEnvConfig{})

	env.mustWriteBinFile(t, "rm", []byte("#!/bin/sh\nexit 0\n"))

	cmd := env.mustCommand(t, "rm", "-rf", "/")

	if got := len(cmd.ExtraFiles); got != 0 {
		t.Fatalf("expected 0 ExtraFiles, got %d", got)
	}

	if slices.Contains(cmd.Args, "--ro-bind-data") {
		t.Fatalf("did not expect --ro-bind-data when no wrappers configured; args: %v", cmd.Args)
	}
}

func Test_Sandbox_CommandWrappers_Skip_Mounts_When_Commands_Empty(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	binDir := filepath.Join(workDir, "bin")
	mustCreateDir(t, binDir)

	rmPath := filepath.Join(binDir, "rm")
	mustCreateExecutable(t, rmPath)

	s := mustNewSandbox(t, &sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
	}, sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": binDir},
	})

	cmd, cleanup, err := s.Command(t.Context(), []string{"rm"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	if got := len(cmd.ExtraFiles); got != 0 {
		t.Fatalf("expected 0 ExtraFiles, got %d", got)
	}

	if slices.Contains(cmd.Args, "--ro-bind-data") {
		t.Fatalf("did not expect --ro-bind-data when no wrappers configured; args: %v", cmd.Args)
	}
}

func Test_Sandbox_CommandWrappers_Wraps_All_Commands_When_Multiple_Deny_Rules_Configured(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	binDir := filepath.Join(workDir, "bin")
	mustCreateDir(t, binDir)

	curlPath := filepath.Join(binDir, "curl")
	rmPath := filepath.Join(binDir, "rm")

	mustCreateExecutable(t, curlPath)
	mustCreateExecutable(t, rmPath)

	s := mustNewSandbox(t, &sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Block: []string{"curl", "rm"},
		},
	}, sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": binDir},
	})

	cmd, cleanup, err := s.Command(t.Context(), []string{"rm"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	if got := len(cmd.ExtraFiles); got != 2 {
		t.Fatalf("expected 2 ExtraFiles, got %d", got)
	}

	// Launcher mounted at target paths, wrappers at runtime paths
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", curlPath})
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", rmPath})
	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), "/run/agent-sandbox/wrappers/curl"})
	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD + 1), "/run/agent-sandbox/wrappers/rm"})
}

func Test_Sandbox_CommandWrappers_Mounts_All_Wrappers_When_Deny_And_Script_Rules_Configured(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	binDir := filepath.Join(workDir, "bin")
	mustCreateDir(t, binDir)

	npmPath := filepath.Join(binDir, "npm")
	rmPath := filepath.Join(binDir, "rm")

	mustCreateExecutable(t, npmPath)
	mustCreateExecutable(t, rmPath)

	wrapperHost := filepath.Join(workDir, "npm-wrapper.sh")
	mustWriteFile(t, wrapperHost, []byte("#!/bin/sh\necho wrapper\n"), 0o644)

	s := mustNewSandbox(t, &sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Block: []string{"rm"},
			Wrappers: map[string]sandbox.Wrapper{
				"npm": sandbox.Wrap(wrapperHost),
			},
		},
	}, sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": binDir},
	})

	cmd, cleanup, err := s.Command(t.Context(), []string{"npm", "--version"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	// 2 ExtraFiles: npm script + rm deny script (launcher is mounted from disk, not via ro-bind-data)
	if got := len(cmd.ExtraFiles); got != 2 {
		t.Fatalf("expected 2 ExtraFiles, got %d", got)
	}

	mustContainSubsequence(t, cmd.Args, []string{"--dir", "/run/agent-sandbox"})
	mustContainSubsequence(t, cmd.Args, []string{"--dir", "/run/agent-sandbox/bin"})
	mustContainSubsequence(t, cmd.Args, []string{"--dir", "/run/agent-sandbox/wrappers"})
	// Launcher at targets, real binary + wrappers at runtime paths
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", npmPath})
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", rmPath})
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", npmPath, "/run/agent-sandbox/bin/npm"})
	// Block commands are processed first, then wrappers (both sorted alphabetically)
	// So rm (blocked) is at FD 3, npm (wrapped) is at FD 4
	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), "/run/agent-sandbox/wrappers/rm"})
	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD + 1), "/run/agent-sandbox/wrappers/npm"})
}

func Test_Sandbox_CommandWrappers_Wraps_All_Path_Hits_When_Deny_Rule_Configured(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	bin1 := filepath.Join(workDir, "bin1")
	bin2 := filepath.Join(workDir, "bin2")

	mustCreateDir(t, bin1)
	mustCreateDir(t, bin2)

	rm1 := filepath.Join(bin1, "rm")
	rm2 := filepath.Join(bin2, "rm")

	mustCreateExecutable(t, rm1)
	mustCreateExecutable(t, rm2)

	pathEnv := bin1 + ":" + bin2

	s := mustNewSandbox(t, &sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Block: []string{"rm"},
		},
	}, sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": pathEnv},
	})

	cmd, cleanup, err := s.Command(t.Context(), []string{"rm"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	// 1 ExtraFile: deny script (shared for all rm targets)
	if got := len(cmd.ExtraFiles); got != 1 {
		t.Fatalf("expected 1 ExtraFile, got %d", got)
	}

	// Launcher at both target paths, single wrapper at runtime path
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", rm1})
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", rm2})
	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), "/run/agent-sandbox/wrappers/rm"})
}

func Test_Sandbox_CommandWrappers_Dedupes_Path_Entries_When_Path_Has_Duplicates(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	binDir := filepath.Join(workDir, "bin")
	mustCreateDir(t, binDir)

	rmPath := filepath.Join(binDir, "rm")
	mustCreateExecutable(t, rmPath)

	pathEnv := binDir + ":" + binDir

	s := mustNewSandbox(t, &sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Block: []string{"rm"},
		},
	}, sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": pathEnv},
	})

	cmd, cleanup, err := s.Command(t.Context(), []string{"rm"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	if got := len(cmd.ExtraFiles); got != 1 {
		t.Fatalf("expected 1 ExtraFile, got %d", got)
	}

	// Launcher at target, wrapper at runtime path
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", rmPath})
	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), "/run/agent-sandbox/wrappers/rm"})
}

func Test_Sandbox_CommandWrappers_Dedupes_Targets_When_Path_Uses_Symlink(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	realDir := filepath.Join(workDir, "real")
	linkDir := filepath.Join(workDir, "link")

	mustCreateDir(t, realDir)
	mustCreateDir(t, linkDir)

	realPath := filepath.Join(realDir, "mybin")
	mustCreateExecutable(t, realPath)

	link := filepath.Join(linkDir, "mybin")
	mustSymlink(t, realPath, link)

	pathEnv := realDir + ":" + linkDir

	s := mustNewSandbox(t, &sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Block: []string{"mybin"},
		},
	}, sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": pathEnv},
	})

	cmd, cleanup, err := s.Command(t.Context(), []string{"mybin"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	if got := len(cmd.ExtraFiles); got != 1 {
		t.Fatalf("expected 1 ExtraFile, got %d", got)
	}

	// Launcher at real target, wrapper at runtime path
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", realPath})
	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), "/run/agent-sandbox/wrappers/mybin"})

	if slices.Contains(cmd.Args, link) {
		t.Fatalf("did not expect wrapper mount to symlink path %q; args: %v", link, cmd.Args)
	}
}

func Test_Sandbox_CommandWrappers_Dedupes_Targets_When_Path_Uses_Duplicate_Symlinks(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	realDir := filepath.Join(workDir, "real")
	linkDir1 := filepath.Join(workDir, "link1")
	linkDir2 := filepath.Join(workDir, "link2")

	mustCreateDir(t, realDir)
	mustCreateDir(t, linkDir1)
	mustCreateDir(t, linkDir2)

	realPath := filepath.Join(realDir, "mybin")
	mustCreateExecutable(t, realPath)

	link1 := filepath.Join(linkDir1, "mybin")
	link2 := filepath.Join(linkDir2, "mybin")

	mustSymlink(t, realPath, link1)
	mustSymlink(t, realPath, link2)

	pathEnv := linkDir1 + ":" + linkDir2

	s := mustNewSandbox(t, &sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Block: []string{"mybin"},
		},
	}, sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": pathEnv},
	})

	cmd, cleanup, err := s.Command(t.Context(), []string{"mybin"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	if got := len(cmd.ExtraFiles); got != 1 {
		t.Fatalf("expected 1 ExtraFile, got %d", got)
	}

	// Launcher at real target, wrapper at runtime path
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", realPath})
	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), "/run/agent-sandbox/wrappers/mybin"})

	if slices.Contains(cmd.Args, link1) || slices.Contains(cmd.Args, link2) {
		t.Fatalf("did not expect wrapper mount to symlink paths; args: %v", cmd.Args)
	}
}

func Test_Sandbox_CommandWrappers_Resolves_Targets_When_Path_Uses_Chained_Symlinks(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	realDir := filepath.Join(workDir, "real")
	linkDir := filepath.Join(workDir, "link")

	mustCreateDir(t, realDir)
	mustCreateDir(t, linkDir)

	realPath := filepath.Join(realDir, "mybin")
	mustCreateExecutable(t, realPath)

	intermediate := filepath.Join(linkDir, "intermediate")
	finalLink := filepath.Join(linkDir, "mybin")

	mustSymlink(t, realPath, intermediate)
	mustSymlink(t, intermediate, finalLink)

	s := mustNewSandbox(t, &sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Block: []string{"mybin"},
		},
	}, sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": linkDir},
	})

	cmd, cleanup, err := s.Command(t.Context(), []string{"mybin"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	if got := len(cmd.ExtraFiles); got != 1 {
		t.Fatalf("expected 1 ExtraFile, got %d", got)
	}

	// Launcher at real target, wrapper at runtime path
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", realPath})
	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), "/run/agent-sandbox/wrappers/mybin"})

	if slices.Contains(cmd.Args, intermediate) || slices.Contains(cmd.Args, finalLink) {
		t.Fatalf("did not expect wrapper mount to symlink paths; args: %v", cmd.Args)
	}
}

func Test_Sandbox_CommandWrappers_Resolves_Targets_When_Path_Uses_Relative_Symlink(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	realPath := filepath.Join(workDir, "realbin")
	mustCreateExecutable(t, realPath)

	linkDir := filepath.Join(workDir, "bin")
	mustCreateDir(t, linkDir)

	link := filepath.Join(linkDir, "mybin")
	mustSymlink(t, "../realbin", link)

	s := mustNewSandbox(t, &sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Block: []string{"mybin"},
		},
	}, sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": linkDir},
	})

	cmd, cleanup, err := s.Command(t.Context(), []string{"mybin"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	if got := len(cmd.ExtraFiles); got != 1 {
		t.Fatalf("expected 1 ExtraFile, got %d", got)
	}

	// Launcher at real target, wrapper at runtime path
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", realPath})
	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), "/run/agent-sandbox/wrappers/mybin"})

	if slices.Contains(cmd.Args, link) {
		t.Fatalf("did not expect wrapper mount to symlink path %q; args: %v", link, cmd.Args)
	}
}

func Test_Sandbox_CommandWrappers_Uses_Resolved_Real_Binary_When_Script_Target_Symlinked(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	realDir := filepath.Join(workDir, "real")
	linkDir := filepath.Join(workDir, "link")

	mustCreateDir(t, realDir)
	mustCreateDir(t, linkDir)

	realPath := filepath.Join(realDir, "mybin")
	mustCreateExecutable(t, realPath)

	link := filepath.Join(linkDir, "mybin")
	mustSymlink(t, realPath, link)

	wrapperHost := filepath.Join(workDir, "mybin-wrapper.sh")
	mustWriteFile(t, wrapperHost, []byte("#!/bin/sh\nexit 0\n"), 0o644)

	pathEnv := linkDir + ":" + realDir

	s := mustNewSandbox(t, &sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Wrappers: map[string]sandbox.Wrapper{
				"mybin": sandbox.Wrap(wrapperHost),
			},
		},
	}, sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": pathEnv},
	})

	cmd, cleanup, err := s.Command(t.Context(), []string{"mybin"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	// 1 ExtraFile: script content (launcher mounted from disk)
	if got := len(cmd.ExtraFiles); got != 1 {
		t.Fatalf("expected 1 ExtraFile, got %d", got)
	}

	// Launcher at target, real binary at runtime bin path
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", realPath})
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", realPath, "/run/agent-sandbox/bin/mybin"})

	if slices.Contains(cmd.Args, link) {
		t.Fatalf("did not expect real binary mount to symlink path %q; args: %v", link, cmd.Args)
	}
}

func Test_Sandbox_CommandWrappers_Returns_Error_When_Path_Symlink_Broken(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	binDir := filepath.Join(workDir, "bin")
	mustCreateDir(t, binDir)

	broken := filepath.Join(binDir, "rm")
	mustSymlink(t, "/nonexistent/target/that/does/not/exist", broken)

	cfg := sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Block: []string{"rm"},
		},
	}
	// mustNewSandbox auto-fills Launcher in many tests; do it manually here since
	// we expect construction to fail.
	cfg.Commands.Launcher = testLauncherPath
	cfg.Commands.MountPath = testRuntimeMountPath

	env := sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": binDir},
	}

	_, err := sandbox.NewWithEnvironment(&cfg, env)
	if err == nil {
		t.Fatal("expected NewWithEnvironment to fail for broken symlink in PATH")
	}

	if !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("expected error to mention PATH, got: %v", err)
	}
}

func Test_Sandbox_CommandWrappers_Ignores_NonExecutable_When_Executable_Present(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	nonexecDir := filepath.Join(workDir, "nonexec")
	execDir := filepath.Join(workDir, "exec")

	mustCreateDir(t, nonexecDir)
	mustCreateDir(t, execDir)

	nonexecPath := filepath.Join(nonexecDir, "rm")
	mustWriteFile(t, nonexecPath, []byte("not executable"), 0o644)

	execPath := filepath.Join(execDir, "rm")
	mustCreateExecutable(t, execPath)

	pathEnv := nonexecDir + ":" + execDir

	s := mustNewSandbox(t, &sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Block: []string{"rm"},
		},
	}, sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": pathEnv},
	})

	cmd, cleanup, err := s.Command(t.Context(), []string{"rm"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	if got := len(cmd.ExtraFiles); got != 1 {
		t.Fatalf("expected 1 ExtraFile, got %d", got)
	}

	// Launcher at target, wrapper at runtime path
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", execPath})
	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), "/run/agent-sandbox/wrappers/rm"})

	if slices.Contains(cmd.Args, nonexecPath) {
		t.Fatalf("did not expect wrapper mount to non-executable path %q; args: %v", nonexecPath, cmd.Args)
	}
}

func Test_Sandbox_CommandWrappers_Ignores_Directory_When_Executable_Present(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	dirPath := filepath.Join(workDir, "dir")
	execDir := filepath.Join(workDir, "exec")

	mustCreateDir(t, dirPath)
	mustCreateDir(t, execDir)

	mustCreateDir(t, filepath.Join(dirPath, "rm"))

	execPath := filepath.Join(execDir, "rm")
	mustCreateExecutable(t, execPath)

	pathEnv := dirPath + ":" + execDir

	s := mustNewSandbox(t, &sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Block: []string{"rm"},
		},
	}, sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": pathEnv},
	})

	cmd, cleanup, err := s.Command(t.Context(), []string{"rm"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	if got := len(cmd.ExtraFiles); got != 1 {
		t.Fatalf("expected 1 ExtraFile, got %d", got)
	}

	// Launcher at target, wrapper at runtime path
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", execPath})
	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), "/run/agent-sandbox/wrappers/rm"})

	if slices.Contains(cmd.Args, filepath.Join(dirPath, "rm")) {
		t.Fatalf("did not expect wrapper mount to directory path %q; args: %v", filepath.Join(dirPath, "rm"), cmd.Args)
	}
}

func Test_Sandbox_CommandWrappers_Ignores_Empty_Path_Entries_When_Path_Has_Executable(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	binDir := filepath.Join(workDir, "bin")
	mustCreateDir(t, binDir)

	rmPath := filepath.Join(binDir, "rm")
	mustCreateExecutable(t, rmPath)

	pathEnv := ":" + binDir + "::"

	s := mustNewSandbox(t, &sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Block: []string{"rm"},
		},
	}, sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": pathEnv},
	})

	cmd, cleanup, err := s.Command(t.Context(), []string{"rm"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	if got := len(cmd.ExtraFiles); got != 1 {
		t.Fatalf("expected 1 ExtraFile, got %d", got)
	}

	// Launcher at target, wrapper at runtime path
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", rmPath})
	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), "/run/agent-sandbox/wrappers/rm"})
}

func Test_Sandbox_CommandWrappers_Wraps_All_Targets_When_Path_Has_Mixed_Symlinks(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	binDir := filepath.Join(workDir, "bin")
	altDir := filepath.Join(workDir, "alt")

	mustCreateDir(t, binDir)
	mustCreateDir(t, altDir)

	primary := filepath.Join(binDir, "mybin")
	alternateTarget := filepath.Join(altDir, "realbin")

	mustCreateExecutable(t, primary)
	mustCreateExecutable(t, alternateTarget)

	symlinkPath := filepath.Join(altDir, "mybin")
	mustSymlink(t, alternateTarget, symlinkPath)

	pathEnv := binDir + ":" + altDir

	s := mustNewSandbox(t, &sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Block: []string{"mybin"},
		},
	}, sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": pathEnv},
	})

	cmd, cleanup, err := s.Command(t.Context(), []string{"mybin"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	// 1 ExtraFile: shared wrapper for mybin command
	if got := len(cmd.ExtraFiles); got != 1 {
		t.Fatalf("expected 1 ExtraFile, got %d", got)
	}

	// Launcher at both target paths, single wrapper at runtime path
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", primary})
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", alternateTarget})
	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), "/run/agent-sandbox/wrappers/mybin"})

	if slices.Contains(cmd.Args, symlinkPath) {
		t.Fatalf("did not expect wrapper mount to symlink path %q; args: %v", symlinkPath, cmd.Args)
	}
}

func Test_Sandbox_CommandWrappers_Preserves_Path_Order_When_Multiple_Targets(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	bin1 := filepath.Join(workDir, "bin1")
	bin2 := filepath.Join(workDir, "bin2")
	bin3 := filepath.Join(workDir, "bin3")

	mustCreateDir(t, bin1)
	mustCreateDir(t, bin2)
	mustCreateDir(t, bin3)

	path1 := filepath.Join(bin1, "mybin")
	path2 := filepath.Join(bin2, "mybin")
	path3 := filepath.Join(bin3, "mybin")

	mustCreateExecutable(t, path1)
	mustCreateExecutable(t, path2)
	mustCreateExecutable(t, path3)

	pathEnv := strings.Join([]string{bin1, bin2, bin3}, ":")

	s := mustNewSandbox(t, &sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Block: []string{"mybin"},
		},
	}, sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": pathEnv},
	})

	cmd, cleanup, err := s.Command(t.Context(), []string{"mybin"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	// 1 ExtraFile: shared wrapper for mybin command
	if got := len(cmd.ExtraFiles); got != 1 {
		t.Fatalf("expected 1 ExtraFile, got %d", got)
	}

	// Launcher mounts at all targets in PATH order
	idx1 := indexOfSubsequence(cmd.Args, []string{"--ro-bind", "/bin/true", path1})
	idx2 := indexOfSubsequence(cmd.Args, []string{"--ro-bind", "/bin/true", path2})
	idx3 := indexOfSubsequence(cmd.Args, []string{"--ro-bind", "/bin/true", path3})

	if idx1 == -1 || idx2 == -1 || idx3 == -1 {
		t.Fatalf("expected args to contain launcher mounts; args: %v", cmd.Args)
	}

	if idx1 >= idx2 || idx2 >= idx3 {
		t.Fatalf("expected launcher mounts in PATH order; args: %v", cmd.Args)
	}

	// Single wrapper at runtime path
	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), "/run/agent-sandbox/wrappers/mybin"})
}

func Test_Sandbox_CommandWrappers_Returns_Error_When_Path_Has_No_Executable(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	nonexecDir := filepath.Join(workDir, "nonexec")
	dirDir := filepath.Join(workDir, "dir")

	mustCreateDir(t, nonexecDir)
	mustCreateDir(t, dirDir)

	mustWriteFile(t, filepath.Join(nonexecDir, "rm"), []byte("not executable"), 0o644)
	mustCreateDir(t, filepath.Join(dirDir, "rm"))

	pathEnv := nonexecDir + ":" + dirDir

	cfg := sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Block: []string{"rm"},
		},
	}
	cfg.Commands.Launcher = testLauncherPath
	cfg.Commands.MountPath = testRuntimeMountPath

	env := sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": pathEnv},
	}

	_, err := sandbox.NewWithEnvironment(&cfg, env)
	if err == nil {
		t.Fatalf("expected NewWithEnvironment to fail when PATH contains no runnable %q", "rm")
	}

	if !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("expected error to mention PATH, got: %v", err)
	}
}

func Test_Sandbox_CommandWrappers_Mounts_Wrapper_And_Real_Binary_When_Script_Path_Absolute(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	binDir := filepath.Join(workDir, "bin")
	mustCreateDir(t, binDir)

	npmPath := filepath.Join(binDir, "npm")
	mustCreateExecutable(t, npmPath)

	wrapperHost := filepath.Join(workDir, "npm-wrapper.sh")
	mustWriteFile(t, wrapperHost, []byte("#!/bin/sh\necho wrapper\n"), 0o644)

	s := mustNewSandbox(t, &sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Wrappers: map[string]sandbox.Wrapper{
				"npm": sandbox.Wrap(wrapperHost),
			},
		},
	}, sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": binDir},
	})

	cmd, cleanup, err := s.Command(t.Context(), []string{"npm", "--version"})
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	// 1 ExtraFile: script content (launcher mounted from disk)
	if got := len(cmd.ExtraFiles); got != 1 {
		t.Fatalf("expected 1 ExtraFile, got %d", got)
	}

	mustContainSubsequence(t, cmd.Args, []string{"--dir", "/run/agent-sandbox"})
	mustContainSubsequence(t, cmd.Args, []string{"--dir", "/run/agent-sandbox/bin"})
	mustContainSubsequence(t, cmd.Args, []string{"--dir", "/run/agent-sandbox/wrappers"})
	// Launcher at target, real binary + wrapper at runtime paths
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", npmPath})
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", npmPath, "/run/agent-sandbox/bin/npm"})
	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), "/run/agent-sandbox/wrappers/npm"})
}

func Test_Sandbox_CommandWrappers_Returns_Error_When_Path_Missing_Or_Empty(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	homeDir := t.TempDir()

	cfg := sandbox.Config{
		Network:    boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Block: []string{"rm"},
			// Set explicitly since we expect construction to fail.
			Launcher:  "/bin/true",
			MountPath: "/run/agent-sandbox",
		},
	}

	_, err := sandbox.NewWithEnvironment(&cfg, sandbox.Environment{HomeDir: homeDir, WorkDir: workDir, HostEnv: map[string]string{}})
	if err == nil {
		t.Fatal("expected NewWithEnvironment to fail when PATH is missing")
	}

	if !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("expected error to mention PATH, got: %v", err)
	}

	_, err = sandbox.NewWithEnvironment(&cfg, sandbox.Environment{HomeDir: homeDir, WorkDir: workDir, HostEnv: map[string]string{"PATH": ""}})
	if err == nil {
		t.Fatal("expected NewWithEnvironment to fail when PATH is empty")
	}

	if !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("expected error to mention PATH, got: %v", err)
	}
}

func Test_Sandbox_ExcludeRules_MaskPaths_When_Configured(t *testing.T) {
	t.Parallel()

	t.Run("ExcludeFile_Uses_RoBindData_When_Source_Empty", func(t *testing.T) {
		t.Parallel()

		env := newTestEnv(t, testEnvConfig{
			Mounts: []sandbox.Mount{sandbox.Exclude("secret.txt")},
		})

		secretPath := env.mustWriteWorkFile(t, "secret.txt", []byte("top secret\n"), 0o600)

		cmd := env.mustCommand(t, "true")

		if got := len(cmd.ExtraFiles); got != 1 {
			t.Fatalf("expected 1 ExtraFile for empty exclusion source, got %d", got)
		}

		mustContainSubsequence(t, cmd.Args, []string{"--perms", "0000", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), secretPath})
	})

	t.Run("ExcludeFile_Orders_ExtraFiles_When_Wrappers_Configured", func(t *testing.T) {
		t.Parallel()

		env := newTestEnv(t, testEnvConfig{
			Block:  []string{"rm"},
			Mounts: []sandbox.Mount{sandbox.Exclude("secret.txt")},
		})

		rmPath := env.mustWriteBinFile(t, "rm", []byte("#!/bin/sh\nexit 0\n"))

		secretPath := env.mustWriteWorkFile(t, "secret.txt", []byte("top secret\n"), 0o600)

		cmd := env.mustCommand(t, "rm")

		if got := len(cmd.ExtraFiles); got != 2 {
			t.Fatalf("expected 2 ExtraFiles (empty exclusion + deny script), got %d", got)
		}

		// Launcher at target, exclude + wrapper via ro-bind-data
		mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", rmPath})
		mustContainSubsequence(t, cmd.Args, []string{"--perms", "0000", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), secretPath})
		mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD + 1), "/run/agent-sandbox/wrappers/rm"})
	})

	t.Run("ExcludeFile_Allows_Missing_Path_When_Not_Found", func(t *testing.T) {
		t.Parallel()

		env := newTestEnv(t, testEnvConfig{
			Mounts: []sandbox.Mount{sandbox.ExcludeFile("missing.txt")},
		})
		missingPath := filepath.Join(env.workDir, "missing.txt")

		cmd := env.mustCommand(t, "true")

		if got := len(cmd.ExtraFiles); got != 1 {
			t.Fatalf("expected 1 ExtraFile for missing exclude file, got %d", got)
		}

		mustContainSubsequence(t, cmd.Args, []string{"--perms", "0000", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), missingPath})
	})

	t.Run("ExcludeDir_Allows_Missing_Path_When_Not_Found", func(t *testing.T) {
		t.Parallel()

		env := newTestEnv(t, testEnvConfig{
			Mounts: []sandbox.Mount{sandbox.ExcludeDir("missing-dir")},
		})
		missingPath := filepath.Join(env.workDir, "missing-dir")

		cmd := env.mustCommand(t, "true")

		if got := len(cmd.ExtraFiles); got != 0 {
			t.Fatalf("expected 0 ExtraFiles for missing exclude dir, got %d", got)
		}

		mustContainSubsequence(t, cmd.Args, []string{"--tmpfs", missingPath})
	})
}

func Test_Sandbox_PolicyMounts_AllowOverride_When_ChildOverridesExcludedDir(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testEnvConfig{
		Mounts: []sandbox.Mount{sandbox.Exclude("parent"), sandbox.RW("parent/child")},
	})

	parent := filepath.Join(env.workDir, "parent")
	child := filepath.Join(parent, "child")
	mustCreateDir(t, child)

	cmd := env.mustCommand(t, "true")

	idxTmpfs := indexOfSubsequence(cmd.Args, []string{"--tmpfs", parent})

	idxBind := indexOfSubsequence(cmd.Args, []string{"--bind", child, child})
	if idxTmpfs == -1 || idxBind == -1 {
		t.Fatalf("expected args to contain tmpfs and bind mounts; args: %v", cmd.Args)
	}

	if idxTmpfs > idxBind {
		t.Fatalf("expected tmpfs(%q) before bind(%q); args: %v", parent, child, cmd.Args)
	}
}

func Test_Sandbox_Presets_ControlDefaults_When_PresetsConfigured(t *testing.T) {
	t.Parallel()

	t.Run("Uses_Defaults_When_Presets_Nil", func(t *testing.T) {
		t.Parallel()

		env := newTestEnv(t, testEnvConfig{
			Config:      &sandbox.Config{},
			IncludePath: boolPtr(false),
		})

		cmd := env.mustCommand(t, "true")

		if !containsSubsequence(cmd.Args, []string{"--bind", env.workDir, env.workDir}) {
			t.Fatalf("expected workdir to be mounted read-write by default; args: %v", cmd.Args)
		}
	})

	t.Run("Uses_None_When_Presets_Empty", func(t *testing.T) {
		t.Parallel()

		env := newTestEnv(t, testEnvConfig{
			Config:      &sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{}}},
			IncludePath: boolPtr(false),
		})

		cmd := env.mustCommand(t, "true")

		if containsSubsequence(cmd.Args, []string{"--bind", env.workDir, env.workDir}) {
			t.Fatalf("expected no workdir mount when presets disabled; args: %v", cmd.Args)
		}
	})
}

func Test_Sandbox_RootMode_UsesTmpfs_When_Configured(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testEnvConfig{
		Config: &sandbox.Config{
			BaseFS:     sandbox.BaseFSEmpty,
			Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		},
		IncludePath: boolPtr(false),
	})

	cmd := env.mustCommand(t, "true")

	if !slices.Contains(cmd.Args, "--tmpfs") || !slices.Contains(cmd.Args, "/") {
		t.Fatalf("expected args to include tmpfs root; args: %v", cmd.Args)
	}

	for i := 0; i+2 < len(cmd.Args); i++ {
		if cmd.Args[i] == "--ro-bind" && cmd.Args[i+1] == "/" && cmd.Args[i+2] == "/" {
			t.Fatalf("did not expect host root ro-bind in tmpfs mode; args: %v", cmd.Args)
		}
	}
}

func Test_Sandbox_Command_Returns_Error_When_Uninitialized(t *testing.T) {
	t.Parallel()

	var s sandbox.Sandbox

	cmd, cleanup, err := s.Command(t.Context(), []string{"true"})
	if cleanup != nil {
		_ = cleanup()
	}

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if cmd != nil {
		t.Fatal("expected nil cmd when sandbox is uninitialized")
	}
}

func Test_Sandbox_Command_Returns_Error_When_Args_Empty(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testEnvConfig{})

	_, cleanup, err := env.mustSandbox(t).Command(t.Context(), nil)
	if cleanup != nil {
		_ = cleanup()
	}

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func Test_Sandbox_Command_Uses_Environment_When_Configured(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	workDir := t.TempDir()

	cfg := sandbox.Config{
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
	}
	env := sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{
			"FOO":  "bar",
			"HOME": homeDir,
			"PATH": "/bin",
		},
	}

	s := mustNewSandbox(t, &cfg, env)

	cmd, cleanup, err := s.Command(t.Context(), []string{"true"})
	if cleanup != nil {
		_ = cleanup()
	}

	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cmd.Dir != workDir {
		t.Fatalf("expected cmd.Dir %q, got %q", workDir, cmd.Dir)
	}

	if !slices.Contains(cmd.Env, "FOO=bar") {
		t.Fatalf("expected FOO in env, got %v", cmd.Env)
	}

	if !slices.Contains(cmd.Env, "HOME="+homeDir) {
		t.Fatalf("expected HOME in env, got %v", cmd.Env)
	}

	if !slices.Contains(cmd.Env, "PATH=/bin") {
		t.Fatalf("expected PATH in env, got %v", cmd.Env)
	}
}

func Test_Sandbox_Command_Uses_Defaults_When_Network_And_Docker_Nil(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testEnvConfig{
		Config: &sandbox.Config{
			Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		},
	})

	cmd := env.mustCommand(t, "true")

	mustContainSubsequence(t, cmd.Args, []string{"--share-net"})

	expectedDockerSock := "/var/run/docker.sock"

	resolvedDir, evalErr := filepath.EvalSymlinks(filepath.Dir(expectedDockerSock))
	if evalErr == nil && filepath.IsAbs(resolvedDir) {
		expectedDockerSock = filepath.Join(resolvedDir, filepath.Base(expectedDockerSock))
	}

	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/dev/null", expectedDockerSock})
}

func Test_Sandbox_TryMounts_SkipMissing_When_Paths_Do_Not_Exist(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testEnvConfig{
		Mounts: []sandbox.Mount{
			sandbox.ROTry("missing-ro"),
			sandbox.RWTry("missing-rw"),
			sandbox.ExcludeTry("missing-exclude"),
		},
	})

	cmd := env.mustCommand(t, "true")

	missingRo := filepath.Join(env.workDir, "missing-ro")
	missingRw := filepath.Join(env.workDir, "missing-rw")
	missingExclude := filepath.Join(env.workDir, "missing-exclude")

	if slices.Contains(cmd.Args, missingRo) {
		t.Fatalf("expected ROTry path to be skipped, args: %v", cmd.Args)
	}

	if slices.Contains(cmd.Args, missingRw) {
		t.Fatalf("expected RWTry path to be skipped, args: %v", cmd.Args)
	}

	if slices.Contains(cmd.Args, missingExclude) {
		t.Fatalf("expected ExcludeTry path to be skipped, args: %v", cmd.Args)
	}
}

func Test_Sandbox_DirectMounts_Appear_When_Configured(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testEnvConfig{
		Mounts: []sandbox.Mount{
			sandbox.RoBind("/bin", "/mnt/ro"),
			sandbox.Bind("/bin", "/mnt/rw"),
			sandbox.Tmpfs("/mnt/tmp"),
			sandbox.Dir("/mnt/dir"),
		},
	})

	cmd := env.mustCommand(t, "true")

	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin", "/mnt/ro"})
	mustContainSubsequence(t, cmd.Args, []string{"--bind", "/bin", "/mnt/rw"})
	mustContainSubsequence(t, cmd.Args, []string{"--tmpfs", "/mnt/tmp"})
	mustContainSubsequence(t, cmd.Args, []string{"--dir", "/mnt/dir"})
}

func Test_Sandbox_DirectMounts_DirPerms_Emits_Chmod(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testEnvConfig{
		Mounts: []sandbox.Mount{
			sandbox.Dir("/mnt/dir", 0o111),
		},
	})

	cmd := env.mustCommand(t, "true")

	mustContainSubsequence(t, cmd.Args, []string{"--dir", "/mnt/dir"})
	mustContainSubsequence(t, cmd.Args, []string{"--chmod", "0111", "/mnt/dir"})
}

func Test_Sandbox_DirectMounts_SkipMissing_When_Try(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	workDir := t.TempDir()

	missingRo := filepath.Join(workDir, "missing-ro")
	missingRw := filepath.Join(workDir, "missing-rw")

	cfg := sandbox.Config{
		Filesystem: sandbox.Filesystem{
			Presets: []string{"!@all"},
			Mounts: []sandbox.Mount{
				sandbox.RoBindTry(missingRo, "/mnt/ro"),
				sandbox.BindTry(missingRw, "/mnt/rw"),
			},
		},
	}
	env := sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": "/bin"},
	}

	s := mustNewSandbox(t, &cfg, env)

	cmd, cleanup, err := s.Command(t.Context(), []string{"true"})
	if cleanup != nil {
		_ = cleanup()
	}

	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if slices.Contains(cmd.Args, missingRo) {
		t.Fatalf("expected RoBindTry source to be skipped, args: %v", cmd.Args)
	}

	if slices.Contains(cmd.Args, missingRw) {
		t.Fatalf("expected BindTry source to be skipped, args: %v", cmd.Args)
	}

	if slices.Contains(cmd.Args, "/mnt/ro") {
		t.Fatalf("expected RoBindTry destination to be skipped, args: %v", cmd.Args)
	}

	if slices.Contains(cmd.Args, "/mnt/rw") {
		t.Fatalf("expected BindTry destination to be skipped, args: %v", cmd.Args)
	}
}

func Test_Sandbox_ExcludeFile_Returns_Error_When_GlobPattern(t *testing.T) {
	t.Parallel()

	cfg := sandbox.Config{
		Filesystem: sandbox.Filesystem{
			Presets: []string{"!@all"},
			Mounts:  []sandbox.Mount{sandbox.ExcludeFile("*.txt")},
		},
	}
	env := sandbox.Environment{
		HomeDir: t.TempDir(),
		WorkDir: t.TempDir(),
		HostEnv: map[string]string{"PATH": "/bin"},
	}

	_, err := sandbox.NewWithEnvironment(&cfg, env)
	if err == nil {
		t.Fatal("expected error for ExcludeFile glob pattern")
	}

	if !strings.Contains(err.Error(), "does not accept glob patterns") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func Test_Sandbox_ExcludeDir_Returns_Error_When_GlobPattern(t *testing.T) {
	t.Parallel()

	cfg := sandbox.Config{
		Filesystem: sandbox.Filesystem{
			Presets: []string{"!@all"},
			Mounts:  []sandbox.Mount{sandbox.ExcludeDir("*.txt")},
		},
	}
	env := sandbox.Environment{
		HomeDir: t.TempDir(),
		WorkDir: t.TempDir(),
		HostEnv: map[string]string{"PATH": "/bin"},
	}

	_, err := sandbox.NewWithEnvironment(&cfg, env)
	if err == nil {
		t.Fatal("expected error for ExcludeDir glob pattern")
	}

	if !strings.Contains(err.Error(), "does not accept glob patterns") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func Test_Sandbox_NewWithEnvironment_Returns_Error_When_WorkDir_Relative(t *testing.T) {
	t.Parallel()

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}
	env := sandbox.Environment{
		HomeDir: t.TempDir(),
		WorkDir: "relative",
		HostEnv: map[string]string{"PATH": "/bin"},
	}

	_, err := sandbox.NewWithEnvironment(&cfg, env)
	if err == nil {
		t.Fatal("expected error for relative WorkDir")
	}

	if !strings.Contains(err.Error(), "WorkDir") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func Test_Sandbox_NewWithEnvironment_Returns_Error_When_HomeDir_Relative(t *testing.T) {
	t.Parallel()

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}
	env := sandbox.Environment{
		HomeDir: "relative",
		WorkDir: t.TempDir(),
		HostEnv: map[string]string{"PATH": "/bin"},
	}

	_, err := sandbox.NewWithEnvironment(&cfg, env)
	if err == nil {
		t.Fatal("expected error for relative HomeDir")
	}

	if !strings.Contains(err.Error(), "HomeDir") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func Test_Sandbox_NewWithEnvironment_Returns_Error_When_RootMode_Invalid(t *testing.T) {
	t.Parallel()

	cfg := sandbox.Config{
		BaseFS:     sandbox.BaseFS("invalid"),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
	}
	env := sandbox.Environment{
		HomeDir: t.TempDir(),
		WorkDir: t.TempDir(),
		HostEnv: map[string]string{"PATH": "/bin"},
	}

	_, err := sandbox.NewWithEnvironment(&cfg, env)
	if err == nil {
		t.Fatal("expected error for invalid root mode")
	}

	if !strings.Contains(err.Error(), "root mode") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func Test_Sandbox_CommandWrappers_Returns_Error_When_Command_Missing(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testEnvConfig{
		Block: []string{"git"},
	})

	_, err := sandbox.NewWithEnvironment(&env.cfg, env.env)
	if err == nil {
		t.Fatal("expected NewWithEnvironment to fail when command is missing from PATH")
	}

	if !strings.Contains(err.Error(), "command not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func Test_Sandbox_CommandWrappers_Returns_Error_When_Script_Missing(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testEnvConfig{
		Wrappers: map[string]sandbox.Wrapper{"tool": sandbox.Wrap("tool-wrapper.sh")},
	})

	env.mustWriteBinFile(t, "tool", []byte("#!/bin/sh\nexit 0\n"))

	_, err := sandbox.NewWithEnvironment(&env.cfg, env.env)
	if err == nil {
		t.Fatal("expected NewWithEnvironment to fail when wrapper script is missing")
	}

	if !strings.Contains(err.Error(), "wrapper script") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func Test_Sandbox_CommandWrappers_Returns_Error_When_Script_Is_Directory(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testEnvConfig{
		Wrappers: map[string]sandbox.Wrapper{"tool": sandbox.Wrap("tool-wrapper")},
	})

	env.mustWriteBinFile(t, "tool", []byte("#!/bin/sh\nexit 0\n"))
	mustCreateDir(t, filepath.Join(env.workDir, "tool-wrapper"))

	_, err := sandbox.NewWithEnvironment(&env.cfg, env.env)
	if err == nil {
		t.Fatal("expected NewWithEnvironment to fail when wrapper script is a directory")
	}

	if !strings.Contains(err.Error(), "directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func Test_Sandbox_CommandWrappers_Always_Expose_RealBinary_When_Wrapped(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testEnvConfig{
		Wrappers: map[string]sandbox.Wrapper{"npm": sandbox.Wrap("npm-wrapper.sh")},
	})

	npmPath := env.mustWriteBinFile(t, "npm", []byte("#!/bin/sh\nexit 0\n"))
	env.mustWriteWorkFile(t, "npm-wrapper.sh", []byte("#!/bin/sh\nexit 0\n"), 0o755)

	cmd := env.mustCommand(t, "npm", "--version")

	// 1 ExtraFile: script content (launcher mounted from disk)
	if got := len(cmd.ExtraFiles); got != 1 {
		t.Fatalf("expected 1 ExtraFile, got %d", got)
	}

	// Launcher at target, real binary always exposed at runtime path
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", "/bin/true", npmPath})
	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0555", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), "/run/agent-sandbox/wrappers/npm"})
	mustContainSubsequence(t, cmd.Args, []string{"--ro-bind", npmPath, "/run/agent-sandbox/bin/npm"})
}

func Test_Sandbox_DefaultEnvironment_Returns_AbsolutePaths_When_Called(t *testing.T) {
	t.Parallel()

	env, err := sandbox.DefaultEnvironment()
	if err != nil {
		t.Fatalf("DefaultEnvironment: %v", err)
	}

	if env.WorkDir == "" {
		t.Fatal("expected WorkDir to be set")
	}

	if !filepath.IsAbs(env.WorkDir) {
		t.Fatalf("expected WorkDir to be absolute, got %q", env.WorkDir)
	}

	if env.HomeDir == "" {
		t.Fatal("expected HomeDir to be set")
	}

	if !filepath.IsAbs(env.HomeDir) {
		t.Fatalf("expected HomeDir to be absolute, got %q", env.HomeDir)
	}

	if env.HostEnv == nil {
		t.Fatal("expected HostEnv to be initialized")
	}

	workDir, err := os.Getwd()
	if err == nil && env.WorkDir != workDir {
		t.Fatalf("expected WorkDir %q, got %q", workDir, env.WorkDir)
	}

	homeDir, err := os.UserHomeDir()
	if err == nil && env.HomeDir != homeDir {
		t.Fatalf("expected HomeDir %q, got %q", homeDir, env.HomeDir)
	}

	pathVar := os.Getenv("PATH")
	if pathVar != "" && env.HostEnv["PATH"] != pathVar {
		t.Fatalf("expected PATH %q, got %q", pathVar, env.HostEnv["PATH"])
	}
}

func Test_Sandbox_New_Uses_DefaultEnvironment_When_Called(t *testing.T) {
	t.Parallel()

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}

	sandboxInstance, err := sandbox.New(&cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cmd, cleanup, err := sandboxInstance.Command(t.Context(), []string{"true"})
	if cleanup != nil {
		_ = cleanup()
	}

	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	workDir, err := os.Getwd()
	if err == nil && cmd.Dir != workDir {
		t.Fatalf("expected cmd.Dir %q, got %q", workDir, cmd.Dir)
	}

	pathVar := os.Getenv("PATH")
	if pathVar != "" && !slices.Contains(cmd.Env, "PATH="+pathVar) {
		t.Fatalf("expected PATH to be propagated, env: %v", cmd.Env)
	}
}

func Test_Sandbox_DirectMounts_Include_RoBindData_When_Configured(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t, testEnvConfig{
		Mounts: []sandbox.Mount{{Kind: sandbox.MountRoBindData, Dst: "/run/data", FD: 9, Perms: 0o644}},
	})

	cmd := env.mustCommand(t, "true")

	if got := len(cmd.ExtraFiles); got != 0 {
		t.Fatalf("expected no ExtraFiles for ro-bind-data, got %d", got)
	}

	mustContainSubsequence(t, cmd.Args, []string{"--perms", "0644", "--ro-bind-data", "9", "/run/data"})
}

func Test_Sandbox_NewWithEnvironment_Returns_Error_When_Command_Invalid(t *testing.T) {
	t.Parallel()

	t.Run("BlockedEmptyName", func(t *testing.T) {
		t.Parallel()

		env := sandbox.Environment{
			HomeDir: t.TempDir(),
			WorkDir: t.TempDir(),
			HostEnv: map[string]string{"PATH": "/bin"},
		}
		cfg := sandbox.Config{
			Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
			Commands: sandbox.Commands{
				Block:    []string{""},
				Launcher: "/bin/true",
			},
		}

		_, err := sandbox.NewWithEnvironment(&cfg, env)
		if err == nil {
			t.Fatal("expected error for empty blocked command name")
		}

		if !strings.Contains(err.Error(), "empty") {
			t.Fatalf("expected error about empty name, got %v", err)
		}
	})

	t.Run("BlockedNameHasSlash", func(t *testing.T) {
		t.Parallel()

		env := sandbox.Environment{
			HomeDir: t.TempDir(),
			WorkDir: t.TempDir(),
			HostEnv: map[string]string{"PATH": "/bin"},
		}
		cfg := sandbox.Config{
			Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
			Commands: sandbox.Commands{
				Block:    []string{"git/evil"},
				Launcher: "/bin/true",
			},
		}

		_, err := sandbox.NewWithEnvironment(&cfg, env)
		if err == nil {
			t.Fatal("expected error for blocked command name with slash")
		}

		if !strings.Contains(err.Error(), "must not contain") {
			t.Fatalf("expected error about slash, got %v", err)
		}
	})

	t.Run("WrapperEmptyName", func(t *testing.T) {
		t.Parallel()

		env := sandbox.Environment{
			HomeDir: t.TempDir(),
			WorkDir: t.TempDir(),
			HostEnv: map[string]string{"PATH": "/bin"},
		}
		cfg := sandbox.Config{
			Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
			Commands: sandbox.Commands{
				Wrappers: map[string]sandbox.Wrapper{"": sandbox.Wrap("script.sh")},
				Launcher: "/bin/true",
			},
		}

		_, err := sandbox.NewWithEnvironment(&cfg, env)
		if err == nil {
			t.Fatal("expected error for empty wrapper command name")
		}

		if !strings.Contains(err.Error(), "empty") {
			t.Fatalf("expected error about empty name, got %v", err)
		}
	})

	t.Run("WrapperNameHasSlash", func(t *testing.T) {
		t.Parallel()

		env := sandbox.Environment{
			HomeDir: t.TempDir(),
			WorkDir: t.TempDir(),
			HostEnv: map[string]string{"PATH": "/bin"},
		}
		cfg := sandbox.Config{
			Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
			Commands: sandbox.Commands{
				Wrappers: map[string]sandbox.Wrapper{"git/evil": sandbox.Wrap("script.sh")},
				Launcher: "/bin/true",
			},
		}

		_, err := sandbox.NewWithEnvironment(&cfg, env)
		if err == nil {
			t.Fatal("expected error for wrapper command name with slash")
		}

		if !strings.Contains(err.Error(), "must not contain") {
			t.Fatalf("expected error about slash, got %v", err)
		}
	})

	t.Run("WrapperMissingScript", func(t *testing.T) {
		t.Parallel()

		env := sandbox.Environment{
			HomeDir: t.TempDir(),
			WorkDir: t.TempDir(),
			HostEnv: map[string]string{"PATH": "/bin"},
		}
		cfg := sandbox.Config{
			Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
			Commands: sandbox.Commands{
				Wrappers: map[string]sandbox.Wrapper{"git": {}},
				Launcher: "/bin/true",
			},
		}

		_, err := sandbox.NewWithEnvironment(&cfg, env)
		if err == nil {
			t.Fatal("expected error for wrapper without script")
		}

		if !strings.Contains(err.Error(), "Path or InlineScript is required") {
			t.Fatalf("expected error about missing script, got %v", err)
		}
	})
}

func Test_Sandbox_NewWithEnvironment_Returns_Error_When_Mount_Invalid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mounts []sandbox.Mount
		want   string
	}{
		{
			name:   "RoBindRelativeDest",
			mounts: []sandbox.Mount{sandbox.RoBind("/bin", "relative")},
			want:   "not absolute",
		},
		{
			name:   "RoBindMissingSource",
			mounts: []sandbox.Mount{sandbox.RoBind("", "/mnt")},
			want:   "requires a source path",
		},
		{
			name:   "RoBindDataMissingFD",
			mounts: []sandbox.Mount{{Kind: sandbox.MountRoBindData, Dst: "/data", FD: 0, Perms: 0o644}},
			want:   "requires a positive FD",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			env := sandbox.Environment{
				HomeDir: t.TempDir(),
				WorkDir: t.TempDir(),
				HostEnv: map[string]string{"PATH": "/bin"},
			}
			cfg := sandbox.Config{
				Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: testCase.mounts},
			}

			_, err := sandbox.NewWithEnvironment(&cfg, env)
			if err == nil {
				t.Fatalf("expected error for %s", testCase.name)
			}

			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("expected error containing %q, got %v", testCase.want, err)
			}
		})
	}
}

func mustNewSandbox(t *testing.T, cfg *sandbox.Config, env sandbox.Environment) *sandbox.Sandbox {
	t.Helper()

	// Auto-set launcher binary for tests that use command wrappers.
	// We use /bin/true as a placeholder because:
	// 1. It exists on all Linux systems and passes file validation
	// 2. Unit tests only verify mount arguments, not actual wrapper execution
	// 3. Actual wrapper behavior (blocking, scripts) is tested via CLI E2E tests
	//    which use the real agent-sandbox binary with RunBinary()
	if (len(cfg.Commands.Block) > 0 || len(cfg.Commands.Wrappers) > 0) && cfg.Commands.Launcher == "" {
		cfg.Commands.Launcher = testLauncherPath
		// Set MountPath explicitly to match test expectations (otherwise it auto-derives as /run/true)
		cfg.Commands.MountPath = testRuntimeMountPath
	}

	s, err := sandbox.NewWithEnvironment(cfg, env)
	if err != nil {
		t.Fatalf("NewWithEnvironment: %v", err)
	}

	return s
}

func mustNewSandboxWithPathCommands(t *testing.T, workDir, homeDir, binDir string, commands map[string]sandbox.Wrapper) *sandbox.Sandbox {
	t.Helper()

	env := sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": binDir},
	}

	cfg := sandbox.Config{
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
		Commands: sandbox.Commands{
			Wrappers:  commands,
			Launcher:  "/bin/true",
			MountPath: "/run/agent-sandbox",
		},
	}

	return mustNewSandbox(t, &cfg, env)
}

type testEnv struct {
	sandbox *sandbox.Sandbox
	cfg     sandbox.Config
	env     sandbox.Environment
	homeDir string
	workDir string
	binDir  string
	tempDir string
}

type testEnvConfig struct {
	Config      *sandbox.Config
	Block       []string
	Wrappers    map[string]sandbox.Wrapper
	Mounts      []sandbox.Mount
	IncludePath *bool
}

func (env *testEnv) mustSandbox(t *testing.T) *sandbox.Sandbox {
	t.Helper()

	if env.sandbox == nil {
		env.sandbox = mustNewSandbox(t, &env.cfg, env.env)
	}

	return env.sandbox
}

func (env *testEnv) mustCommand(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()

	cmd, cleanup, err := env.mustSandbox(t).Command(t.Context(), args)
	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	return cmd
}

func (env *testEnv) mustWriteBinFile(t *testing.T, name string, data []byte) string {
	t.Helper()

	path := filepath.Join(env.binDir, name)
	mustWriteFile(t, path, data, 0o755)

	return path
}

func (env *testEnv) mustWriteWorkFile(t *testing.T, name string, data []byte, perm os.FileMode) string {
	t.Helper()

	path := filepath.Join(env.workDir, name)
	mustWriteFile(t, path, data, perm)

	return path
}

func newTestEnv(t *testing.T, cfg testEnvConfig) testEnv {
	t.Helper()

	config := sandbox.Config{}
	if cfg.Config != nil {
		config = *cfg.Config
	} else {
		config.Filesystem.Presets = []string{"!@all"}
	}

	if cfg.Block != nil || cfg.Wrappers != nil {
		config.Commands = sandbox.Commands{
			Block:     cfg.Block,
			Wrappers:  cfg.Wrappers,
			Launcher:  "/bin/true",
			MountPath: "/run/agent-sandbox",
		}
	}

	if cfg.Mounts != nil {
		config.Filesystem.Mounts = cfg.Mounts
	}

	includePath := true
	if cfg.IncludePath != nil {
		includePath = *cfg.IncludePath
	}

	homeDir := t.TempDir()
	workDir := t.TempDir()
	tempDir := t.TempDir()
	binDir := filepath.Join(workDir, "bin")
	mustCreateDir(t, binDir)

	env := sandbox.Environment{HomeDir: homeDir, WorkDir: workDir, HostEnv: map[string]string{}}
	if includePath {
		env.HostEnv["PATH"] = binDir
	}

	return testEnv{
		cfg:     config,
		env:     env,
		homeDir: homeDir,
		workDir: workDir,
		binDir:  binDir,
		tempDir: tempDir,
	}
}

func mustCreateDir(t *testing.T, path string) {
	t.Helper()

	err := os.MkdirAll(path, 0o755)
	if err != nil {
		t.Fatalf("mkdir %q: %v", path, err)
	}
}

func mustCreateExecutable(t *testing.T, path string) {
	t.Helper()
	mustCreateDir(t, filepath.Dir(path))
	mustWriteFile(t, path, []byte("#!/bin/sh\necho hello\n"), 0o755)
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()

	err := os.Symlink(target, link)
	if err != nil {
		t.Fatalf("failed to create symlink %s -> %s: %v", link, target, err)
	}
}

func mustWriteFile(t *testing.T, path string, data []byte, perm os.FileMode) {
	t.Helper()

	err := os.WriteFile(path, data, perm)
	if err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func mustContainSubsequence(t *testing.T, haystack []string, needle []string) {
	t.Helper()

	if !containsSubsequence(haystack, needle) {
		t.Fatalf("expected args to contain %v\nargs: %v", needle, haystack)
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func containsSubsequence(haystack []string, needle []string) bool {
	if len(needle) == 0 {
		return true
	}

	if len(haystack) < len(needle) {
		return false
	}

	for i := 0; i <= len(haystack)-len(needle); i++ {
		ok := true

		for j := range needle {
			if haystack[i+j] != needle[j] {
				ok = false

				break
			}
		}

		if ok {
			return true
		}
	}

	return false
}

func indexOfSubsequence(haystack []string, needle []string) int {
	if len(needle) == 0 {
		return 0
	}

	if len(haystack) < len(needle) {
		return -1
	}

	for i := 0; i <= len(haystack)-len(needle); i++ {
		ok := true

		for j := range needle {
			if haystack[i+j] != needle[j] {
				ok = false

				break
			}
		}

		if ok {
			return i
		}
	}

	return -1
}

// ============================================================================
// bwrap_args_test.go ported coverage
// ============================================================================

func bwrapArgsFromCmd(cmd *exec.Cmd) []string {
	args := slices.Clone(cmd.Args)
	if len(args) == 0 {
		return nil
	}

	if filepath.Base(args[0]) == "bwrap" {
		args = args[1:]
	}

	for i, a := range args {
		if a == "--" {
			return args[:i]
		}
	}

	return args
}

func mustCommand(t *testing.T, cfg *sandbox.Config, env sandbox.Environment, command ...string) (*exec.Cmd, int) {
	t.Helper()
	s := mustNewSandbox(t, cfg, env)

	cmd, cleanup, err := s.Command(t.Context(), command)
	if cleanup != nil {
		t.Cleanup(func() { _ = cleanup() })
	}

	if err != nil {
		t.Fatalf("Command: %v", err)
	}

	return cmd, len(cmd.ExtraFiles)
}

func mustCommandError(t *testing.T, cfg *sandbox.Config, env sandbox.Environment, wantSubstring string, command ...string) {
	t.Helper()

	// Mirror mustNewSandbox's test conveniences so callers don't need to set a
	// launcher binary when using command wrappers.
	if (len(cfg.Commands.Block) > 0 || len(cfg.Commands.Wrappers) > 0) && cfg.Commands.Launcher == "" {
		cfg.Commands.Launcher = testLauncherPath
		cfg.Commands.MountPath = testRuntimeMountPath
	}

	s, newErr := sandbox.NewWithEnvironment(cfg, env)
	if newErr != nil {
		if !strings.Contains(newErr.Error(), wantSubstring) {
			t.Fatalf("expected error containing %q, got %v", wantSubstring, newErr)
		}

		return
	}

	cmd, cleanup, err := s.Command(t.Context(), command)
	if cleanup != nil {
		_ = cleanup()
	}

	if err == nil {
		if cmd != nil {
			t.Fatalf("expected error containing %q, got nil (cmd.Args=%v)", wantSubstring, cmd.Args)
		}

		t.Fatalf("expected error containing %q, got nil", wantSubstring)
	}

	if !strings.Contains(err.Error(), wantSubstring) {
		t.Fatalf("expected error containing %q, got %v", wantSubstring, err)
	}
}

func newEnvWithHostEnv(t *testing.T, hostEnv map[string]string) (sandbox.Environment, string) {
	t.Helper()

	homeDir := t.TempDir()
	workDir := t.TempDir()
	binDir := filepath.Join(workDir, "bin")
	mustCreateDir(t, binDir)

	env := sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: workDir,
		HostEnv: map[string]string{"PATH": binDir},
	}
	maps.Copy(env.HostEnv, hostEnv)

	return env, binDir
}

func countOccurrences(args []string, target string) int {
	count := 0

	for _, a := range args {
		if a == target {
			count++
		}
	}

	return count
}

func Test_Sandbox_BaseArgs_IncludeCoreFlags_When_MinimalConfig(t *testing.T) {
	t.Parallel()

	env, binDir := newEnvWithHostEnv(t, nil)
	if env.HostEnv["PATH"] != binDir {
		t.Fatalf("expected PATH to match bin dir, got %q", env.HostEnv["PATH"])
	}

	cfg := sandbox.Config{
		Network:    boolPtr(true),
		Docker:     boolPtr(false),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
	}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	wantPrefix := []string{
		"--die-with-parent",
		"--unshare-all",
		"--share-net",
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/run",
	}

	if len(args) < len(wantPrefix) {
		t.Fatalf("args too short for prefix check: want %v, got %v", wantPrefix, args)
	}

	if !slices.Equal(args[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("prefix mismatch: want %v, got %v", wantPrefix, args[:len(wantPrefix)])
	}

	mustContainSubsequence(t, args, []string{"--chdir", env.WorkDir})
}

func Test_Sandbox_BaseArgs_OmitsShareNet_When_NetworkDisabled(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	cfg := sandbox.Config{
		Network:    boolPtr(false),
		Docker:     boolPtr(false),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
	}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	if slices.Contains(args, "--share-net") {
		t.Fatalf("did not expect --share-net, args: %v", args)
	}
}

func Test_Sandbox_DNSResolverMounts_Are_OnlyApplied_When_NetworkEnabled(t *testing.T) {
	t.Parallel()

	const resolvConf = "/etc/resolv.conf"

	linkTarget, err := os.Readlink(resolvConf)
	if err != nil {
		t.Skip("/etc/resolv.conf is not a symlink")
	}

	resolvedPath := linkTarget
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(filepath.Dir(resolvConf), resolvedPath)
	}

	resolvedPath = filepath.Clean(resolvedPath)
	if resolvedPath == "/run" || !strings.HasPrefix(resolvedPath, "/run/") {
		t.Skipf("/etc/resolv.conf does not resolve under /run (resolved=%q)", resolvedPath)
	}

	parentDir := filepath.Dir(resolvedPath)
	if parentDir == "" || parentDir == "/" || parentDir == "/run" {
		t.Skipf("unexpected resolv.conf target parent dir %q", parentDir)
	}

	info, err := os.Stat(parentDir)
	if err != nil || !info.IsDir() {
		t.Skipf("resolv.conf target parent dir %q not accessible", parentDir)
	}

	env, _ := newEnvWithHostEnv(t, nil)

	cfgEnabled := sandbox.Config{
		Network:    boolPtr(true),
		Docker:     boolPtr(false),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
	}
	cmdEnabled, _ := mustCommand(t, &cfgEnabled, env, "true")
	argsEnabled := bwrapArgsFromCmd(cmdEnabled)
	mustContainSubsequence(t, argsEnabled, []string{"--ro-bind", parentDir, parentDir})

	cfgDisabled := sandbox.Config{
		Network:    boolPtr(false),
		Docker:     boolPtr(false),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
	}
	cmdDisabled, _ := mustCommand(t, &cfgDisabled, env, "true")

	argsDisabled := bwrapArgsFromCmd(cmdDisabled)
	if containsSubsequence(argsDisabled, []string{"--ro-bind", parentDir, parentDir}) {
		t.Fatalf("did not expect /run DNS bind-mount when network disabled; args: %v", argsDisabled)
	}
}

func Test_Sandbox_DockerSocket_Binds_When_Enabled_And_SocketExists(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	mustWriteFile(t, socketPath, []byte("sock"), 0o600)

	env, _ := newEnvWithHostEnv(t, map[string]string{"DOCKER_HOST": "unix://" + socketPath})

	cfg := sandbox.Config{
		Network:    boolPtr(true),
		Docker:     boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
	}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	mustContainSubsequence(t, args, []string{"--bind", socketPath, socketPath})
}

func Test_Sandbox_DockerSocket_Masks_When_Disabled(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "docker.sock")
	mustWriteFile(t, socketPath, []byte("sock"), 0o600)

	env, _ := newEnvWithHostEnv(t, map[string]string{"DOCKER_HOST": "unix://" + socketPath})

	cfg := sandbox.Config{
		Network:    boolPtr(true),
		Docker:     boolPtr(false),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
	}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	mustContainSubsequence(t, args, []string{"--ro-bind", "/dev/null", socketPath})

	if containsSubsequence(args, []string{"--bind", socketPath, socketPath}) {
		t.Fatalf("did not expect docker socket bind when disabled, args: %v", args)
	}
}

func Test_Sandbox_DockerSocket_ReturnsError_When_Enabled_And_SocketMissing(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "missing.sock")
	env, _ := newEnvWithHostEnv(t, map[string]string{"DOCKER_HOST": "unix://" + socketPath})

	cfg := sandbox.Config{
		Network:    boolPtr(true),
		Docker:     boolPtr(true),
		Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}},
	}

	mustCommandError(t, &cfg, env, "docker socket not found", "true")
}

func Test_Sandbox_PolicyMounts_Mounts_Ro_Rw_And_Exclude_When_Configured(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustCreateDir(t, filepath.Join(env.WorkDir, "readonly"))
	mustCreateDir(t, filepath.Join(env.WorkDir, "readwrite"))
	mustCreateDir(t, filepath.Join(env.WorkDir, "excluded"))

	cfg := sandbox.Config{
		Filesystem: sandbox.Filesystem{
			Presets: []string{"!@all"},
			Mounts: []sandbox.Mount{
				sandbox.RO("readonly"),
				sandbox.RW("readwrite"),
				sandbox.Exclude("excluded"),
			},
		},
	}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	ro := filepath.Join(env.WorkDir, "readonly")
	rw := filepath.Join(env.WorkDir, "readwrite")
	ex := filepath.Join(env.WorkDir, "excluded")

	mustContainSubsequence(t, args, []string{"--ro-bind", ro, ro})
	mustContainSubsequence(t, args, []string{"--bind", rw, rw})
	mustContainSubsequence(t, args, []string{"--tmpfs", ex})
}

func Test_Sandbox_PolicyMounts_Expands_Tilde_And_Relative_Patterns_When_Configured(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustCreateDir(t, filepath.Join(env.HomeDir, ".config"))
	mustCreateDir(t, filepath.Join(env.WorkDir, "~bob"))
	mustCreateDir(t, filepath.Join(env.WorkDir, "$HOME"))
	mustCreateDir(t, filepath.Join(env.WorkDir, "target"))
	mustCreateDir(t, filepath.Join(env.WorkDir, "subdir"))

	cfg := sandbox.Config{
		Filesystem: sandbox.Filesystem{
			Presets: []string{"!@all"},
			Mounts: []sandbox.Mount{
				sandbox.RO("~/.config"),
				sandbox.RO("~"),
				sandbox.RO("~bob"),
				sandbox.RO("$HOME"),
				sandbox.RO("foo/../target"),
				sandbox.RW("."),
				sandbox.RO("subdir"),
			},
		},
	}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	mustContainSubsequence(t, args, []string{"--ro-bind", filepath.Join(env.HomeDir, ".config"), filepath.Join(env.HomeDir, ".config")})
	mustContainSubsequence(t, args, []string{"--ro-bind", env.HomeDir, env.HomeDir})
	mustContainSubsequence(t, args, []string{"--ro-bind", filepath.Join(env.WorkDir, "~bob"), filepath.Join(env.WorkDir, "~bob")})
	mustContainSubsequence(t, args, []string{"--ro-bind", filepath.Join(env.WorkDir, "$HOME"), filepath.Join(env.WorkDir, "$HOME")})
	mustContainSubsequence(t, args, []string{"--ro-bind", filepath.Join(env.WorkDir, "target"), filepath.Join(env.WorkDir, "target")})
	mustContainSubsequence(t, args, []string{"--bind", env.WorkDir, env.WorkDir})
	mustContainSubsequence(t, args, []string{"--ro-bind", filepath.Join(env.WorkDir, "subdir"), filepath.Join(env.WorkDir, "subdir")})
}

func Test_Sandbox_PolicyMounts_ResolvesDotDot_When_RelativePath_ContainsDotDot(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustCreateDir(t, filepath.Join(env.WorkDir, "subdir"))

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{sandbox.RO("subdir/..")}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	mustContainSubsequence(t, args, []string{"--ro-bind", env.WorkDir, env.WorkDir})
}

func Test_Sandbox_NewWithEnvironment_ReturnsError_When_PolicyMount_Destination_Empty(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{sandbox.RO("")}}}

	_, err := sandbox.NewWithEnvironment(&cfg, env)
	if err == nil {
		t.Fatal("expected error for empty mount destination")
	}

	if !strings.Contains(err.Error(), "empty destination") {
		t.Fatalf("expected error to mention empty destination, got %v", err)
	}
}

func Test_Sandbox_NewWithEnvironment_ReturnsError_When_PolicyMount_TargetsRun(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{sandbox.RW("/run")}}}

	_, err := sandbox.NewWithEnvironment(&cfg, env)
	if err == nil {
		t.Fatal("expected error for /run policy mount")
	}

	if !strings.Contains(err.Error(), "reserved path") {
		t.Fatalf("expected error to mention reserved path, got %v", err)
	}
}

func Test_Sandbox_PolicyMounts_ExpandGlobs_And_Error_When_NoMatches(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustWriteFile(t, filepath.Join(env.WorkDir, "config1.json"), []byte("{}"), 0o644)
	mustWriteFile(t, filepath.Join(env.WorkDir, "config2.json"), []byte("{}"), 0o644)
	mustWriteFile(t, filepath.Join(env.WorkDir, "config3.json"), []byte("{}"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{sandbox.RO("config*.json")}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	c1 := filepath.Join(env.WorkDir, "config1.json")
	c2 := filepath.Join(env.WorkDir, "config2.json")
	c3 := filepath.Join(env.WorkDir, "config3.json")

	idx1 := indexOfSubsequence(args, []string{"--ro-bind", c1, c1})
	idx2 := indexOfSubsequence(args, []string{"--ro-bind", c2, c2})

	idx3 := indexOfSubsequence(args, []string{"--ro-bind", c3, c3})
	if idx1 == -1 || idx2 == -1 || idx3 == -1 {
		t.Fatalf("expected all glob matches to be mounted; args: %v", args)
	}

	if idx1 >= idx2 || idx2 >= idx3 {
		t.Fatalf("expected glob results to be sorted, got idx1=%d idx2=%d idx3=%d; args: %v", idx1, idx2, idx3, args)
	}

	cfgNone := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{sandbox.RO("*.nonexistent")}}}
	mustCommandError(t, &cfgNone, env, "matched 0 paths", "true")
}

func Test_Sandbox_PolicyMounts_ReturnError_When_GlobPattern_Invalid(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{sandbox.RO("[invalid")}}}

	mustCommandError(t, &cfg, env, "invalid glob pattern", "true")
}

func Test_Sandbox_PolicyMounts_ResolveSymlinks_When_Path_Is_Symlink(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	realDir := filepath.Join(env.WorkDir, "real-dir")
	mustCreateDir(t, realDir)

	linkPath := filepath.Join(env.WorkDir, "link-to-real")

	err := os.Symlink(realDir, linkPath)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{sandbox.RW("link-to-real")}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	mustContainSubsequence(t, args, []string{"--bind", realDir, realDir})

	if containsSubsequence(args, []string{"--bind", linkPath, linkPath}) {
		t.Fatalf("did not expect symlink path to be mounted directly; args: %v", args)
	}
}

func Test_Sandbox_PolicyMounts_ReturnError_When_Symlink_Dangling(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	linkPath := filepath.Join(env.WorkDir, "dangling-link")

	err := os.Symlink("/nonexistent/target", linkPath)
	if err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{sandbox.RO("dangling-link")}}}

	mustCommandError(t, &cfg, env, "resolves to missing path", "true")
}

func Test_Sandbox_PolicyMounts_ReturnError_When_Path_Missing(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{sandbox.RO("does-not-exist")}}}

	mustCommandError(t, &cfg, env, "resolves to missing path", "true")
}

func Test_Sandbox_PolicyMounts_ApplyExactBeatsGlob_When_BothMatchSamePath(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustWriteFile(t, filepath.Join(env.WorkDir, "config.json"), []byte("{}"), 0o644)
	mustWriteFile(t, filepath.Join(env.WorkDir, "other.json"), []byte("{}"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{
		sandbox.RO("*.json"),
		sandbox.RW("config.json"),
	}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	configPath := filepath.Join(env.WorkDir, "config.json")
	otherPath := filepath.Join(env.WorkDir, "other.json")

	mustContainSubsequence(t, args, []string{"--bind", configPath, configPath})
	mustContainSubsequence(t, args, []string{"--ro-bind", otherPath, otherPath})

	if containsSubsequence(args, []string{"--ro-bind", configPath, configPath}) {
		t.Fatalf("did not expect config.json to be mounted read-only; args: %v", args)
	}
}

func Test_Sandbox_PolicyMounts_ApplyLaterWins_When_SameExactPath_ProvidedTwice(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustCreateDir(t, filepath.Join(env.WorkDir, "contested"))
	contested := filepath.Join(env.WorkDir, "contested")

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{
		sandbox.RW("contested"),
		sandbox.RO("contested"),
	}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	mustContainSubsequence(t, args, []string{"--ro-bind", contested, contested})

	if containsSubsequence(args, []string{"--bind", contested, contested}) {
		t.Fatalf("did not expect contested dir to be mounted read-write; args: %v", args)
	}
}

func Test_Sandbox_PolicyMounts_DedupResolvedPaths_When_SameGlobRepeated(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustWriteFile(t, filepath.Join(env.WorkDir, "file.txt"), []byte("x"), 0o644)
	filePath := filepath.Join(env.WorkDir, "file.txt")

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{
		sandbox.RO("*.txt"),
		sandbox.RO("*.txt"),
	}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	// For ro-bind mounts, the path appears twice (src and dst) for a single mount.
	if got := countOccurrences(args, filePath); got != 2 {
		t.Fatalf("expected exactly one mount for %q (2 occurrences), got %d; args: %v", filePath, got, args)
	}
}

func Test_Sandbox_PolicyMounts_SortByDepth_ThenAlphabetically_When_MultiplePaths(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustCreateDir(t, filepath.Join(env.WorkDir, "a"))
	mustCreateDir(t, filepath.Join(env.WorkDir, "a", "b"))
	mustCreateDir(t, filepath.Join(env.WorkDir, "a", "b", "c"))
	mustCreateDir(t, filepath.Join(env.WorkDir, "a", "b", "c", "d", "e"))

	a := filepath.Join(env.WorkDir, "a")
	ab := filepath.Join(env.WorkDir, "a", "b")
	abc := filepath.Join(env.WorkDir, "a", "b", "c")
	abcde := filepath.Join(env.WorkDir, "a", "b", "c", "d", "e")

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{
		sandbox.RO("a/b/c/d/e"),
		sandbox.RO("a"),
		sandbox.RO("a/b/c"),
		sandbox.RO("a/b"),
	}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	idxA := indexOfSubsequence(args, []string{"--ro-bind", a, a})
	idxAB := indexOfSubsequence(args, []string{"--ro-bind", ab, ab})
	idxABC := indexOfSubsequence(args, []string{"--ro-bind", abc, abc})

	idxABCDE := indexOfSubsequence(args, []string{"--ro-bind", abcde, abcde})
	if idxA == -1 || idxAB == -1 || idxABC == -1 || idxABCDE == -1 {
		t.Fatalf("expected all mounts to exist, args: %v", args)
	}

	if idxA >= idxAB || idxAB >= idxABC || idxABC >= idxABCDE {
		t.Fatalf("expected mounts sorted shallow→deep, got idxA=%d idxAB=%d idxABC=%d idxABCDE=%d; args: %v", idxA, idxAB, idxABC, idxABCDE, args)
	}
}

func Test_Sandbox_Presets_EmitExpectedMounts_When_BaseEnabled(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustCreateDir(t, filepath.Join(env.HomeDir, ".ssh"))
	mustCreateDir(t, filepath.Join(env.HomeDir, ".gnupg"))
	mustCreateDir(t, filepath.Join(env.HomeDir, ".aws"))

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all", "@base"}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	mustContainSubsequence(t, args, []string{"--bind", env.WorkDir, env.WorkDir})
	mustContainSubsequence(t, args, []string{"--ro-bind", env.HomeDir, env.HomeDir})

	mustContainSubsequence(t, args, []string{"--tmpfs", filepath.Join(env.HomeDir, ".ssh")})
	mustContainSubsequence(t, args, []string{"--tmpfs", filepath.Join(env.HomeDir, ".gnupg")})
	mustContainSubsequence(t, args, []string{"--tmpfs", filepath.Join(env.HomeDir, ".aws")})
}

func Test_Sandbox_Presets_EmitExpectedMounts_When_CachesEnabled(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	paths := []string{filepath.Join(env.HomeDir, ".cache"), filepath.Join(env.HomeDir, ".bun"), filepath.Join(env.HomeDir, "go"), filepath.Join(env.HomeDir, ".npm"), filepath.Join(env.HomeDir, ".cargo")}
	for _, p := range paths {
		mustCreateDir(t, p)
	}

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all", "@caches"}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	for _, p := range paths {
		mustContainSubsequence(t, args, []string{"--bind-try", p, p})
	}
}

func Test_Sandbox_Presets_ApplyToggle_LastWins_When_Configured(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	cacheDir := filepath.Join(env.HomeDir, ".cache")
	mustCreateDir(t, cacheDir)

	t.Run("Enable", func(t *testing.T) {
		t.Parallel()

		cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all", "!@caches", "@caches"}}}
		cmd, _ := mustCommand(t, &cfg, env, "true")
		args := bwrapArgsFromCmd(cmd)
		mustContainSubsequence(t, args, []string{"--bind-try", cacheDir, cacheDir})
	})

	t.Run("Disable", func(t *testing.T) {
		t.Parallel()

		cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all", "@caches", "!@caches"}}}
		cmd, _ := mustCommand(t, &cfg, env, "true")

		args := bwrapArgsFromCmd(cmd)
		if slices.Contains(args, cacheDir) {
			t.Fatalf("did not expect cache dir mount when disabled, args: %v", args)
		}
	})
}

func Test_Sandbox_Presets_EmitExpectedMounts_When_AgentsEnabled(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustCreateDir(t, filepath.Join(env.HomeDir, ".codex"))
	mustCreateDir(t, filepath.Join(env.HomeDir, ".claude"))
	mustCreateDir(t, filepath.Join(env.HomeDir, ".pi"))
	mustWriteFile(t, filepath.Join(env.HomeDir, ".claude.json"), []byte("{}"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all", "@agents"}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	for _, p := range []string{filepath.Join(env.HomeDir, ".codex"), filepath.Join(env.HomeDir, ".claude"), filepath.Join(env.HomeDir, ".claude.json"), filepath.Join(env.HomeDir, ".pi")} {
		mustContainSubsequence(t, args, []string{"--bind-try", p, p})
	}
}

func Test_Sandbox_Presets_EmitExpectedMounts_When_LintTS_Enabled(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustWriteFile(t, filepath.Join(env.WorkDir, "biome.json"), []byte("{}"), 0o644)
	mustWriteFile(t, filepath.Join(env.WorkDir, ".eslintrc.json"), []byte("{}"), 0o644)
	mustWriteFile(t, filepath.Join(env.WorkDir, "prettier.config.js"), []byte("module.exports = {}\n"), 0o644)
	mustWriteFile(t, filepath.Join(env.WorkDir, "tsconfig.json"), []byte("{}"), 0o644)
	mustWriteFile(t, filepath.Join(env.WorkDir, ".editorconfig"), []byte("root=true\n"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all", "@lint/ts"}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	mustContainSubsequence(t, args, []string{"--ro-bind-try", filepath.Join(env.WorkDir, "biome.json"), filepath.Join(env.WorkDir, "biome.json")})
	mustContainSubsequence(t, args, []string{"--ro-bind-try", filepath.Join(env.WorkDir, ".eslintrc.json"), filepath.Join(env.WorkDir, ".eslintrc.json")})
	mustContainSubsequence(t, args, []string{"--ro-bind-try", filepath.Join(env.WorkDir, "prettier.config.js"), filepath.Join(env.WorkDir, "prettier.config.js")})
	mustContainSubsequence(t, args, []string{"--ro-bind-try", filepath.Join(env.WorkDir, "tsconfig.json"), filepath.Join(env.WorkDir, "tsconfig.json")})
	mustContainSubsequence(t, args, []string{"--ro-bind-try", filepath.Join(env.WorkDir, ".editorconfig"), filepath.Join(env.WorkDir, ".editorconfig")})
}

func Test_Sandbox_Presets_EmitExpectedMounts_When_GitEnabled(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	gitDir := filepath.Join(env.WorkDir, ".git")
	mustCreateDir(t, filepath.Join(gitDir, "hooks"))
	mustWriteFile(t, filepath.Join(gitDir, "config"), []byte("[core]\n"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all", "@git"}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	mustContainSubsequence(t, args, []string{"--ro-bind-try", filepath.Join(gitDir, "hooks"), filepath.Join(gitDir, "hooks")})
	mustContainSubsequence(t, args, []string{"--ro-bind-try", filepath.Join(gitDir, "config"), filepath.Join(gitDir, "config")})
}

func Test_Sandbox_Presets_OmitGitMounts_When_GitDisabled(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	gitDir := filepath.Join(env.WorkDir, ".git")
	hooksPath := filepath.Join(gitDir, "hooks")
	configPath := filepath.Join(gitDir, "config")

	mustCreateDir(t, hooksPath)
	mustWriteFile(t, configPath, []byte("[core]\n"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all", "@base", "@git", "!@git"}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	mustContainSubsequence(t, args, []string{"--bind", env.WorkDir, env.WorkDir})

	if containsSubsequence(args, []string{"--ro-bind-try", hooksPath, hooksPath}) {
		t.Fatalf("did not expect git hooks protection when git preset is disabled; args: %v", args)
	}

	if containsSubsequence(args, []string{"--ro-bind-try", configPath, configPath}) {
		t.Fatalf("did not expect git config protection when git preset is disabled; args: %v", args)
	}
}

func Test_Sandbox_Presets_EmitNoGitMounts_When_NotARepo(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all", "@git"}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	if slices.Contains(args, filepath.Join(env.WorkDir, ".git", "hooks")) {
		t.Fatalf("did not expect git hooks mount when not in a repo, args: %v", args)
	}
}

func Test_Sandbox_Presets_Protects_MainRepo_When_Worktree(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mainRepo := t.TempDir()
	mainGit := filepath.Join(mainRepo, ".git")
	worktreesDir := filepath.Join(mainGit, "worktrees")
	gitDir := filepath.Join(worktreesDir, "wt")
	mustCreateDir(t, filepath.Join(gitDir, "hooks"))
	mustWriteFile(t, filepath.Join(gitDir, "config"), []byte("[core]\n"), 0o644)

	mustCreateDir(t, filepath.Join(mainGit, "hooks"))
	mustWriteFile(t, filepath.Join(mainGit, "config"), []byte("[core]\n"), 0o644)

	mustWriteFile(t, filepath.Join(env.WorkDir, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all", "@git"}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	mustContainSubsequence(t, args, []string{"--ro-bind-try", filepath.Join(gitDir, "hooks"), filepath.Join(gitDir, "hooks")})
	mustContainSubsequence(t, args, []string{"--ro-bind-try", filepath.Join(gitDir, "config"), filepath.Join(gitDir, "config")})
	mustContainSubsequence(t, args, []string{"--ro-bind-try", filepath.Join(mainGit, "hooks"), filepath.Join(mainGit, "hooks")})
	mustContainSubsequence(t, args, []string{"--ro-bind-try", filepath.Join(mainGit, "config"), filepath.Join(mainGit, "config")})
}

func Test_Sandbox_Presets_GitStrict_Protects_Refs_When_Configured(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	gitDir := filepath.Join(env.WorkDir, ".git")
	mustCreateDir(t, filepath.Join(gitDir, "hooks"))
	mustWriteFile(t, filepath.Join(gitDir, "config"), []byte("[core]\n"), 0o644)

	mustWriteFile(t, filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/master\n"), 0o644)

	headsDir := filepath.Join(gitDir, "refs", "heads")
	tagsDir := filepath.Join(gitDir, "refs", "tags")

	mustCreateDir(t, headsDir)
	mustCreateDir(t, tagsDir)

	masterRef := filepath.Join(headsDir, "master")
	featureRef := filepath.Join(headsDir, "feature")

	mustWriteFile(t, masterRef, []byte("deadbeef\n"), 0o644)
	mustWriteFile(t, featureRef, []byte("deadbeef\n"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all", "@git-strict"}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	// Non-current branches should be mounted read-only individually (not the whole directory).
	mustContainSubsequence(t, args, []string{"--ro-bind", featureRef, featureRef})
	mustContainSubsequence(t, args, []string{"--ro-bind", tagsDir, tagsDir})

	// Current branch (master) should NOT appear in args - it's implicitly writable
	// because we don't mount the headsDir as RO (so git can create lock files).
	if containsSubsequence(args, []string{"--ro-bind", masterRef, masterRef}) {
		t.Fatalf("did not expect master ref to be mounted ro; args: %v", args)
	}

	if containsSubsequence(args, []string{"--bind", masterRef, masterRef}) {
		t.Fatalf("did not expect master ref to be explicitly mounted rw; args: %v", args)
	}

	if containsSubsequence(args, []string{"--ro-bind", headsDir, headsDir}) {
		t.Fatalf("did not expect refs/heads directory to be mounted ro (breaks lock files); args: %v", args)
	}
}

func Test_Sandbox_NewWithEnvironment_ReturnsError_When_PresetUnknown(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"@nonexistent"}}}

	_, err := sandbox.NewWithEnvironment(&cfg, env)
	if err == nil {
		t.Fatal("expected error for unknown preset")
	}

	if !strings.Contains(err.Error(), "unknown preset") {
		t.Fatalf("expected error to mention unknown preset, got %v", err)
	}

	if !strings.Contains(err.Error(), "@nonexistent") {
		t.Fatalf("expected error to mention preset name, got %v", err)
	}
}

func Test_Sandbox_Presets_NegatedAll_Produces_MinimalPolicyMounts_When_Configured(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	if containsSubsequence(args, []string{"--bind", env.WorkDir, env.WorkDir}) {
		t.Fatalf("did not expect workdir to be mounted when presets are disabled; args: %v", args)
	}

	if containsSubsequence(args, []string{"--ro-bind", env.HomeDir, env.HomeDir}) {
		t.Fatalf("did not expect homedir to be mounted when presets are disabled; args: %v", args)
	}
}

func Test_Sandbox_Command_Is_Deterministic_Across_NewInstances_When_ConfigSame(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustCreateDir(t, filepath.Join(env.WorkDir, "src"))
	mustCreateDir(t, filepath.Join(env.WorkDir, "bin"))
	mustWriteFile(t, filepath.Join(env.WorkDir, "a.txt"), []byte("a"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{
		sandbox.RO("*.txt"),
		sandbox.RW("src"),
		sandbox.RW("bin"),
	}}}

	var first []string

	for i := range 5 {
		s := mustNewSandbox(t, &cfg, env)

		cmd, cleanup, err := s.Command(t.Context(), []string{"true"})
		if cleanup != nil {
			_ = cleanup()
		}

		if err != nil {
			t.Fatalf("run %d: Command: %v", i, err)
		}

		args := bwrapArgsFromCmd(cmd)
		if i == 0 {
			first = args

			continue
		}

		if !slices.Equal(args, first) {
			t.Fatalf("run %d: expected deterministic args\nfirst=%v\nthis=%v", i, first, args)
		}
	}
}

func Test_Sandbox_ExcludeFile_Uses_ExtraFileFD_When_ExcludeTargetsFile(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	secretPath := filepath.Join(env.WorkDir, "secret.txt")
	mustWriteFile(t, secretPath, []byte("secret\n"), 0o600)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{sandbox.Exclude("secret.txt")}}}

	cmd, extraFiles := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	if extraFiles != 1 {
		t.Fatalf("expected 1 ExtraFile for excluded file, got %d", extraFiles)
	}

	mustContainSubsequence(t, args, []string{"--perms", "0000", "--ro-bind-data", strconv.Itoa(firstExtraFileFD), secretPath})
}

func Test_Sandbox_PolicyMounts_ExpandGlobs_When_PatternUses_QuestionMark(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustWriteFile(t, filepath.Join(env.WorkDir, "a.txt"), []byte("a"), 0o644)
	mustWriteFile(t, filepath.Join(env.WorkDir, "b.txt"), []byte("b"), 0o644)
	mustWriteFile(t, filepath.Join(env.WorkDir, "ab.txt"), []byte("ab"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{sandbox.RO("?.txt")}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	a := filepath.Join(env.WorkDir, "a.txt")
	b := filepath.Join(env.WorkDir, "b.txt")
	ab := filepath.Join(env.WorkDir, "ab.txt")

	mustContainSubsequence(t, args, []string{"--ro-bind", a, a})
	mustContainSubsequence(t, args, []string{"--ro-bind", b, b})

	if slices.Contains(args, ab) {
		t.Fatalf("did not expect ab.txt to match ? wildcard; args: %v", args)
	}
}

func Test_Sandbox_PolicyMounts_ExpandGlobs_When_PatternUses_CharClass(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustWriteFile(t, filepath.Join(env.WorkDir, "file1.txt"), []byte("1"), 0o644)
	mustWriteFile(t, filepath.Join(env.WorkDir, "file2.txt"), []byte("2"), 0o644)
	mustWriteFile(t, filepath.Join(env.WorkDir, "file3.txt"), []byte("3"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{sandbox.RO("file[12].txt")}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	f1 := filepath.Join(env.WorkDir, "file1.txt")
	f2 := filepath.Join(env.WorkDir, "file2.txt")
	f3 := filepath.Join(env.WorkDir, "file3.txt")

	mustContainSubsequence(t, args, []string{"--ro-bind", f1, f1})
	mustContainSubsequence(t, args, []string{"--ro-bind", f2, f2})

	if slices.Contains(args, f3) {
		t.Fatalf("did not expect file3.txt to match [12]; args: %v", args)
	}
}

func Test_Sandbox_PolicyMounts_ExpandGlobs_When_PatternHas_NestedWildcard(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustCreateDir(t, filepath.Join(env.WorkDir, "packages", "client"))
	mustCreateDir(t, filepath.Join(env.WorkDir, "packages", "server"))
	mustCreateDir(t, filepath.Join(env.WorkDir, "packages", "shared"))

	mustWriteFile(t, filepath.Join(env.WorkDir, "packages", "client", "config.json"), []byte("{}"), 0o644)
	mustWriteFile(t, filepath.Join(env.WorkDir, "packages", "server", "config.json"), []byte("{}"), 0o644)
	mustWriteFile(t, filepath.Join(env.WorkDir, "packages", "shared", "other.json"), []byte("{}"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all"}, Mounts: []sandbox.Mount{sandbox.RO("packages/*/config.json")}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	client := filepath.Join(env.WorkDir, "packages", "client", "config.json")
	server := filepath.Join(env.WorkDir, "packages", "server", "config.json")
	other := filepath.Join(env.WorkDir, "packages", "shared", "other.json")

	mustContainSubsequence(t, args, []string{"--ro-bind", client, client})
	mustContainSubsequence(t, args, []string{"--ro-bind", server, server})

	if slices.Contains(args, other) {
		t.Fatalf("did not expect other.json to match config.json pattern; args: %v", args)
	}
}

func Test_Sandbox_Presets_EmitExpectedMounts_When_LintGo_And_LintPython_Enabled(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustWriteFile(t, filepath.Join(env.WorkDir, ".golangci.yml"), []byte("linters: {}\n"), 0o644)
	mustWriteFile(t, filepath.Join(env.WorkDir, "pyproject.toml"), []byte("[tool]\n"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all", "@lint/go", "@lint/python"}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	mustContainSubsequence(t, args, []string{"--ro-bind-try", filepath.Join(env.WorkDir, ".golangci.yml"), filepath.Join(env.WorkDir, ".golangci.yml")})
	mustContainSubsequence(t, args, []string{"--ro-bind-try", filepath.Join(env.WorkDir, "pyproject.toml"), filepath.Join(env.WorkDir, "pyproject.toml")})
}

func Test_Sandbox_Presets_LastWins_When_LintAll_Disabled_Then_PythonEnabled(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustWriteFile(t, filepath.Join(env.WorkDir, ".golangci.yml"), []byte("linters: {}\n"), 0o644)
	mustWriteFile(t, filepath.Join(env.WorkDir, "pyproject.toml"), []byte("[tool]\n"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all", "!@lint/all", "@lint/python"}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	mustContainSubsequence(t, args, []string{"--ro-bind-try", filepath.Join(env.WorkDir, "pyproject.toml"), filepath.Join(env.WorkDir, "pyproject.toml")})

	if slices.Contains(args, filepath.Join(env.WorkDir, ".golangci.yml")) {
		t.Fatalf("did not expect go lint mounts when lint/all is disabled; args: %v", args)
	}
}

func Test_Sandbox_Presets_OmitAgentsMounts_When_AgentsDisabled(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	mustCreateDir(t, filepath.Join(env.HomeDir, ".claude"))

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all", "@agents", "!@agents"}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	if slices.Contains(args, filepath.Join(env.HomeDir, ".claude")) {
		t.Fatalf("did not expect agents mounts when agents preset is disabled; args: %v", args)
	}
}

func Test_Sandbox_Presets_GitStrict_OmitsCurrentRefWrite_When_DetachedHead(t *testing.T) {
	t.Parallel()

	env, _ := newEnvWithHostEnv(t, nil)

	gitDir := filepath.Join(env.WorkDir, ".git")
	mustCreateDir(t, filepath.Join(gitDir, "hooks"))
	mustWriteFile(t, filepath.Join(gitDir, "config"), []byte("[core]\n"), 0o644)

	// Detached HEAD is represented by a non-ref line.
	mustWriteFile(t, filepath.Join(gitDir, "HEAD"), []byte("deadbeef\n"), 0o644)

	headsDir := filepath.Join(gitDir, "refs", "heads")
	tagsDir := filepath.Join(gitDir, "refs", "tags")

	mustCreateDir(t, headsDir)
	mustCreateDir(t, tagsDir)

	masterRef := filepath.Join(headsDir, "master")
	mustWriteFile(t, masterRef, []byte("deadbeef\n"), 0o644)

	cfg := sandbox.Config{Filesystem: sandbox.Filesystem{Presets: []string{"!@all", "@git-strict"}}}

	cmd, _ := mustCommand(t, &cfg, env, "true")
	args := bwrapArgsFromCmd(cmd)

	// In detached HEAD, ALL branches are protected (mounted RO individually).
	mustContainSubsequence(t, args, []string{"--ro-bind", masterRef, masterRef})
	mustContainSubsequence(t, args, []string{"--ro-bind", tagsDir, tagsDir})

	// Should NOT mount headsDir as RO (we mount individual refs instead).
	if containsSubsequence(args, []string{"--ro-bind", headsDir, headsDir}) {
		t.Fatalf("did not expect refs/heads directory to be mounted ro; args: %v", args)
	}
	// Should NOT have any RW bind for refs in detached HEAD.
	if containsSubsequence(args, []string{"--bind", masterRef, masterRef}) {
		t.Fatalf("did not expect branch ref to be writable in detached HEAD; args: %v", args)
	}
}
