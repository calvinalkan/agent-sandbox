//go:build linux

package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"sync"

	"golang.org/x/sys/unix"
)

const firstExtraFD = 3

// Command constructs an unstarted [exec.Cmd] that would run argv inside the
// sandbox. The returned cleanup function must be called to release resources
// (e.g. wrapper FDs, temporary files). Cleanup is safe to call multiple times;
// cleanup routines are expected to be idempotent and ignore missing files.
//
// The returned *[exec.Cmd] is NOT started. Callers may set Stdin/Stdout/Stderr and
// then call Run/Start/Wait.
func (s *Sandbox) Command(ctx context.Context, argv []string) (*exec.Cmd, func() error, error) {
	if s == nil || s.v == nil {
		return nil, func() error { return nil }, errors.New("sandbox: uninitialized sandbox (use New or NewWithEnvironment)")
	}

	if len(argv) == 0 {
		return nil, func() error { return nil }, errors.New("sandbox: no command provided")
	}

	plan := s.plan
	if plan == nil {
		return nil, func() error { return nil }, errors.New("sandbox: uninitialized sandbox plan (use New or NewWithEnvironment)")
	}

	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, func() error { return nil }, fmt.Errorf("sandbox: bwrap not found in PATH: %w", err)
	}

	debugf := s.v.cfg.Debugf

	var cleanupFuncs []func() error

	cleanupAll := func() error {
		var errs []error

		for i := len(cleanupFuncs) - 1; i >= 0; i-- {
			err := cleanupFuncs[i]()
			if err != nil {
				errs = append(errs, err)
			}
		}

		return errors.Join(errs...)
	}

	bwrapArgs := slices.Clone(plan.bwrapArgs)

	var extraFiles []*os.File

	if plan.needsEmptyFile {
		// Excluded files are masked by mounting an unreadable empty file over them.
		// The planner emits a placeholder FD in the bwrap argv, and we substitute it
		// here with an inherited FD that always reads as empty.
		devNullFile, err := os.Open(os.DevNull)
		if err != nil {
			return nil, func() error { return nil }, fmt.Errorf("open %s for empty exclusion source: %w", os.DevNull, err)
		}

		extraFiles = append(extraFiles, devNullFile)
		cleanupFuncs = append(cleanupFuncs, closeFilesOnce([]*os.File{devNullFile}))

		childFD := firstExtraFD + (len(extraFiles) - 1)
		replaceArg(bwrapArgs, emptyDataFDPlaceholder, strconv.Itoa(childFD))

		if slices.Contains(bwrapArgs, emptyDataFDPlaceholder) {
			cleanupErr := cleanupAll()

			return nil, func() error { return nil }, errors.Join(internalErrorf("Command", "empty-data FD placeholder not replaced"), cleanupErr)
		}
	}

	if len(plan.wrapperMounts) > 0 {
		wrapperArgs, files, err := roBindDataArgs(plan.wrapperMounts, firstExtraFD+len(extraFiles))
		if err != nil {
			cleanupErr := cleanupAll()

			return nil, func() error { return nil }, errors.Join(err, cleanupErr)
		}

		extraFiles = append(extraFiles, files...)
		bwrapArgs = append(bwrapArgs, wrapperArgs...)
		cleanupFuncs = append(cleanupFuncs, closeFilesOnce(files))
	}

	if len(plan.chmods) > 0 {
		for _, chmod := range plan.chmods {
			permString := fmt.Sprintf("%04o", chmod.perms.Perm())
			bwrapArgs = append(bwrapArgs, "--chmod", permString, chmod.path)
		}
	}

	args := make([]string, 0, len(bwrapArgs)+1+len(argv))
	args = append(args, bwrapArgs...)
	args = append(args, "--")
	args = append(args, argv...)

	cmd := exec.CommandContext(ctx, bwrapPath, args...)
	cmd.Dir = s.v.env.WorkDir

	cmd.Env = slices.Clone(s.v.envSlice)
	if len(extraFiles) > 0 {
		cmd.ExtraFiles = extraFiles
	}

	if debugf != nil {
		debugf("sandbox(command): argv0=%q bwrap=%q bwrapArgs=%d extraFiles=%d wrapperMounts=%d chmods=%d", argv[0], bwrapPath, len(bwrapArgs), len(extraFiles), len(plan.wrapperMounts), len(plan.chmods))
	}

	return cmd, cleanupAll, nil
}

// envMapToSliceSorted converts a map env to a sorted KEY=VALUE slice.
//
// Sorting improves determinism in tests and makes debug output stable.
func envMapToSliceSorted(env map[string]string) []string {
	if len(env) == 0 {
		return []string{}
	}

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}

	return out
}

func replaceArg(args []string, placeholder, value string) {
	for i, arg := range args {
		if arg == placeholder {
			args[i] = value
		}
	}
}

func closeFilesOnce(files []*os.File) func() error {
	var (
		once   sync.Once
		outErr error
	)

	return func() error {
		once.Do(func() {
			outErr = closeFiles(files...)
		})

		return outErr
	}
}

func closeFiles(files ...*os.File) error {
	var errs []error

	for _, f := range files {
		if f == nil {
			continue
		}

		err := f.Close()
		if err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// roBindDataArgs materializes a list of roBindDataMounts into bwrap args and ExtraFiles.
//
// It allocates one backing file per mount, writes the mount data into it,
// rewinds it, and returns it as an inherited file.
func roBindDataArgs(mounts []roBindDataMount, firstChildFD int) ([]string, []*os.File, error) {
	args := make([]string, 0, len(mounts)*5)
	files := make([]*os.File, 0, len(mounts))

	closeOnError := func(cause error) error {
		closeErr := closeFiles(files...)

		return errors.Join(cause, closeErr)
	}

	for i, mount := range mounts {
		backingFile, err := newRoBindDataBackingFile()
		if err != nil {
			return nil, nil, closeOnError(fmt.Errorf("create ro-bind-data backing file for %q (mount %d): %w", mount.dst, i, err))
		}

		files = append(files, backingFile)

		_, err = backingFile.WriteString(mount.data)
		if err != nil {
			return nil, nil, closeOnError(fmt.Errorf("write ro-bind-data for %q (mount %d): %w", mount.dst, i, err))
		}

		_, err = backingFile.Seek(0, 0)
		if err != nil {
			return nil, nil, closeOnError(fmt.Errorf("rewind ro-bind-data for %q (mount %d): %w", mount.dst, i, err))
		}

		childFD := firstChildFD + i

		mountArgs, err := mountToArgs(Mount{Kind: MountRoBindData, Dst: mount.dst, FD: childFD, Perms: mount.perms})
		if err != nil {
			return nil, nil, closeOnError(fmt.Errorf("build ro-bind-data args for %q (mount %d): %w", mount.dst, i, err))
		}

		args = append(args, mountArgs...)
	}

	return args, files, nil
}

func newRoBindDataBackingFile() (*os.File, error) {
	// Prefer an anonymous in-memory file when possible to avoid filesystem I/O.
	fd, err := unix.MemfdCreate("sandbox-ro-bind-data", unix.MFD_CLOEXEC)
	if err == nil {
		memFile := os.NewFile(uintptr(fd), "sandbox-ro-bind-data")
		if memFile == nil {
			closeErr := unix.Close(fd)

			return nil, errors.Join(
				internalErrorf("newRoBindDataBackingFile", "os.NewFile returned nil"),
				closeErr,
			)
		}

		return memFile, nil
	}

	// Fall back to an unlinked temp file. bwrap reads the content via the
	// inherited FD, not by path.
	tempFile, tmpErr := os.CreateTemp("", "sandbox-ro-bind-data-*")
	if tmpErr != nil {
		return nil, errors.Join(
			fmt.Errorf("memfd_create: %w", err),
			fmt.Errorf("create temp file: %w", tmpErr),
		)
	}

	// Best-effort unlink; ignore error as the file is still usable via FD.
	_ = os.Remove(tempFile.Name())

	return tempFile, nil
}
