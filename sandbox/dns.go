//go:build linux

package sandbox

import (
	"os"
	"path/filepath"
	"strings"
)

// dnsResolverArgs returns bwrap args to preserve DNS resolution when /etc/resolv.conf
// is a symlink into /run (common with systemd-resolved).
//
// The sandbox mounts /run as a fresh tmpfs, which would otherwise break such
// symlinks. We fix this by bind-mounting the symlink target's parent directory
// from the host into /run inside the sandbox.
func dnsResolverArgs(debugf Debugf) []string {
	const resolvConf = "/etc/resolv.conf"

	linkTarget, err := os.Readlink(resolvConf)
	if err != nil {
		return nil
	}

	resolvedPath := linkTarget
	if !filepath.IsAbs(resolvedPath) {
		resolvedPath = filepath.Join(filepath.Dir(resolvConf), resolvedPath)
	}

	resolvedPath = filepath.Clean(resolvedPath)
	if resolvedPath == "/run" || !strings.HasPrefix(resolvedPath, "/run/") {
		return nil
	}

	parentDir := filepath.Dir(resolvedPath)
	if parentDir == "" || parentDir == "/" {
		return nil
	}

	// Avoid mounting the entire host /run into the sandbox.
	if parentDir == "/run" {
		return nil
	}

	info, err := os.Stat(parentDir)
	if err != nil || !info.IsDir() {
		return nil
	}

	if debugf != nil {
		debugf("dns: resolv.conf is symlink to %q (resolved=%q); bind-mounting %q", linkTarget, resolvedPath, parentDir)
	}

	return []string{
		"--dir", parentDir,
		"--ro-bind", parentDir, parentDir,
	}
}
