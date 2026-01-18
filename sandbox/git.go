//go:build linux

package sandbox

// This file contains git-specific preset helpers.
//
// The @git and @git-strict presets protect repository metadata from modification.
// The strict variant additionally makes non-current branch refs read-only while
// allowing updates to the current branch (so typical git workflows still work).
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// gitPresetRules returns policy mounts that protect git metadata.
//
// When strict is false, it protects hooks and config.
// When strict is true, it additionally protects refs/heads (except the current
// branch ref, which remains writable when not in detached HEAD).
func gitPresetRules(workDir string, strict bool) ([]Mount, error) {
	gitDir, mainRepo, err := discoverGitDirs(workDir)
	if err != nil {
		return nil, err
	}

	if gitDir == "" {
		return nil, nil
	}

	mounts := []Mount{
		ROTry(filepath.Join(gitDir, "hooks")),
		ROTry(filepath.Join(gitDir, "config")),
	}

	if mainRepo != "" {
		// When in a worktree, the worktree's git directory (gitDir) lives inside
		// the main repo at .git/worktrees/<name>. Git needs write access to this
		// directory for lock files (index.lock, etc.). Since @base may make the
		// main repo's parent directory read-only, we must explicitly grant RW
		// access to the worktree's git directory.
		mounts = append(mounts,
			RW(gitDir),
			ROTry(filepath.Join(mainRepo, ".git", "hooks")),
			ROTry(filepath.Join(mainRepo, ".git", "config")),
		)
	}

	if !strict {
		return mounts, nil
	}

	headRef, detached, err := gitHeadState(gitDir)
	if err != nil {
		return nil, err
	}

	commonGitDir := gitDir
	if mainRepo != "" {
		commonGitDir = filepath.Join(mainRepo, ".git")
	}

	headsDir := filepath.Join(commonGitDir, "refs", "heads")

	headInfo, err := os.Stat(headsDir)
	if err != nil {
		return nil, fmt.Errorf("git heads dir %q: %w", headsDir, err)
	}

	if !headInfo.IsDir() {
		return nil, fmt.Errorf("git heads dir %q is not a directory", headsDir)
	}

	// Git commit needs to create lock files (e.g. refs/heads/master.lock) in the
	// refs/heads directory. If we mount the directory as RO, new files cannot be
	// created even if existing files are remounted RW.
	//
	// Instead, we iterate over all branch refs and mount each non-current branch
	// as RO, leaving the directory itself writable so the current branch can be
	// updated via the lock-file mechanism.
	branchRefs, err := collectBranchRefs(headsDir)
	if err != nil {
		return nil, err
	}

	currentRefPath := ""
	if !detached {
		currentRefPath = filepath.Join(headsDir, filepath.FromSlash(headRef))
	}

	for _, refPath := range branchRefs {
		if refPath == currentRefPath {
			continue
		}

		mounts = append(mounts, RO(refPath))
	}

	mounts = append(mounts, RO(filepath.Join(commonGitDir, "refs", "tags")))

	packedRefsPath := filepath.Join(commonGitDir, "packed-refs")

	packedInfo, err := os.Stat(packedRefsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat packed refs %q: %w", packedRefsPath, err)
		}
	} else {
		if packedInfo.IsDir() {
			return nil, fmt.Errorf("packed refs %q is a directory", packedRefsPath)
		}

		mounts = append(mounts, RO(packedRefsPath))
	}

	return mounts, nil
}

// discoverGitDirs discovers the effective git directory for workDir.
//
// It supports both normal repositories (a .git directory) and worktrees (a .git
// file containing "gitdir: <path>").
//
// When workDir is a worktree, mainRepo may be returned to allow protecting the
// main repository's hooks/config as well.
func discoverGitDirs(workDir string) (string, string, error) {
	gitPath := filepath.Join(workDir, ".git")

	info, err := os.Lstat(gitPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil
		}

		return "", "", fmt.Errorf("stat git path %q: %w", gitPath, err)
	}

	if info.IsDir() {
		return gitPath, "", nil
	}

	// Worktrees commonly use a .git file containing "gitdir: <path>".
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return "", "", fmt.Errorf("read git file %q: %w", gitPath, err)
	}

	line := strings.TrimSpace(string(data))
	if line == "" {
		return "", "", fmt.Errorf("git file %q is empty", gitPath)
	}

	const prefix = "gitdir:"

	if !strings.HasPrefix(strings.ToLower(line), prefix) {
		return "", "", fmt.Errorf("git file %q does not start with %q", gitPath, prefix)
	}

	gitDirPath := strings.TrimSpace(line[len(prefix):])

	if gitDirPath == "" {
		return "", "", fmt.Errorf("git file %q has empty gitdir path", gitPath)
	}

	if !filepath.IsAbs(gitDirPath) {
		gitDirPath = filepath.Join(workDir, gitDirPath)
	}

	gitDirPath = filepath.Clean(gitDirPath)
	if !filepath.IsAbs(gitDirPath) {
		return "", "", fmt.Errorf("gitdir path %q from %q is not absolute", gitDirPath, gitPath)
	}

	info, err = os.Stat(gitDirPath)
	if err != nil {
		return "", "", fmt.Errorf("gitdir %q not found: %w", gitDirPath, err)
	}

	if !info.IsDir() {
		return "", "", fmt.Errorf("gitdir %q is not a directory", gitDirPath)
	}

	const worktreesMarker = "/.git/worktrees/"

	var mainRepo string
	if idx := strings.Index(gitDirPath, worktreesMarker); idx > 0 {
		// Derive main repo path from ".../<main>/.git/worktrees/<name>".
		mainRepo = gitDirPath[:idx]
	}

	return gitDirPath, mainRepo, nil
}

// gitHeadState reads .git/HEAD and determines whether the repo is detached.
//
// For non-detached HEAD, it returns the branch name under refs/heads/.
func gitHeadState(gitDir string) (string, bool, error) {
	headPath := filepath.Join(gitDir, "HEAD")

	head, err := os.ReadFile(headPath)
	if err != nil {
		return "", false, fmt.Errorf("read git HEAD %q: %w", headPath, err)
	}

	line := strings.TrimSpace(string(head))
	if line == "" {
		return "", false, fmt.Errorf("git HEAD %q is empty", headPath)
	}

	const refPrefix = "ref: "
	if strings.HasPrefix(line, refPrefix) {
		ref := strings.TrimSpace(line[len(refPrefix):])
		if ref == "" {
			return "", false, fmt.Errorf("git HEAD %q has empty ref", headPath)
		}

		const headsPrefix = "refs/heads/"
		if after, ok := strings.CutPrefix(ref, headsPrefix); ok {
			if after == "" {
				return "", false, fmt.Errorf("git HEAD %q points to an empty branch", headPath)
			}

			return after, false, nil
		}

		return "", false, fmt.Errorf("git HEAD %q references unsupported ref %q", headPath, ref)
	}

	return "", true, nil
}

// collectBranchRefs walks the refs/heads directory and returns all ref file paths.
// Git branch refs can be nested (e.g., refs/heads/feature/foo), so we walk recursively.
func collectBranchRefs(headsDir string) ([]string, error) {
	var refs []string

	err := filepath.WalkDir(headsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		refs = append(refs, path)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk refs/heads %q: %w", headsDir, err)
	}

	return refs, nil
}
