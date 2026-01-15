//go:build linux

package sandbox

import "os"

// Mount describes a mount operation or policy mount.
//
// For policy kinds (MountReadOnly, MountReadOnlyTry, MountReadWrite,
// MountReadWriteTry, MountExclude, MountExcludeTry, MountExcludeFile,
// MountExcludeDir), Dst is a host path or pattern. It may be absolute, relative
// to [Environment.WorkDir], "~"-prefixed, or a glob. During planning, the
// pattern is expanded and resolved to absolute host paths, and each resolved
// host path is mounted at the same absolute destination inside the sandbox.
// Src/FD/Perms are ignored.
//
// For low-level mounts, Src is the host path and Dst is the absolute path inside
// the sandbox. For mounts that only need a destination (e.g. tmpfs), Src is
// ignored.
//
// MountRoBindData uses FD and Perms to mount file content provided through
// exec.Cmd.ExtraFiles.
type Mount struct {
	// Kind selects the mount operation or policy mount type.
	Kind MountKind

	// Src is the host source path for bind mounts.
	//
	// For policy mounts (RO/RW/Exclude), Src is ignored and must be empty.
	Src string

	// Dst is the destination path inside the sandbox.
	//
	// For policy mounts, Dst is a pattern that will be resolved to absolute paths
	// on the host.
	Dst string

	// Perms controls the permissions applied by mounts that create filesystem
	// entries inside the sandbox.
	//
	// - MountRoBindData: sets the mode of the injected file.
	// - MountDir: if non-zero, the directory is chmod'd after mounts.
	//
	// For other mount kinds it may be ignored.
	Perms os.FileMode

	// FD is used for MountRoBindData and refers to the child FD number inside the
	// bwrap process (e.g. 3 for the first ExtraFile).
	//
	// For other mount kinds it must be zero.
	FD int
}
