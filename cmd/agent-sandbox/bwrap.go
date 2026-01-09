package main

// BwrapArgs generates bwrap arguments from resolved paths and configuration.
//
// The argument order is important - bwrap processes arguments in order, so:
//  1. Namespace and process setup (--die-with-parent, --unshare-all, --share-net)
//  2. Virtual mounts for /dev and /proc
//  3. Base root filesystem mount (--ro-bind / /)
//  4. Isolated runtime tmpfs for /run
//  5. Individual path mounts, sorted by depth (shallower first)
//  6. Working directory (--chdir)
//
// Exclude paths are currently ignored (handled by d5g3tgg).
func BwrapArgs(paths []ResolvedPath, cfg *Config) []string {
	// Process cleanup and namespace setup first
	args := []string{
		"--die-with-parent", // Auto-cleanup when parent dies
		"--unshare-all",     // Create new namespaces
	}

	// Network sharing (default is on, so we share unless explicitly disabled)
	if cfg.Network != nil && *cfg.Network {
		args = append(args, "--share-net")
	}

	// Always include virtual mounts (per SPEC hardcoded behavior)
	// Root filesystem read-only (per SPEC security guarantees)
	// Isolated runtime tmpfs for /run
	args = append(args,
		"--dev", "/dev",
		"--proc", "/proc",
		"--ro-bind", "/", "/",
		"--tmpfs", "/run",
	)

	// Process paths in order - ResolveAndSort ensures correct depth ordering
	// More specific paths come AFTER less specific, so they overlay correctly
	for _, resolvedPath := range paths {
		switch resolvedPath.Access {
		case PathAccessRo:
			// Use --ro-bind-try for paths that may not exist
			// (e.g., lint configs that only exist in some projects)
			args = append(args, "--ro-bind-try", resolvedPath.Resolved, resolvedPath.Resolved)
		case PathAccessRw:
			// Use --bind-try for optional writable paths
			args = append(args, "--bind-try", resolvedPath.Resolved, resolvedPath.Resolved)
		case PathAccessExclude:
			// Exclude mounts are implemented in d5g3tgg (directories vs files differ).
			// For now, skip exclude paths - they'll be handled separately.
			continue
		}
	}

	// Working directory
	args = append(args, "--chdir", cfg.EffectiveCwd)

	return args
}
