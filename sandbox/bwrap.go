//go:build linux

package sandbox

// This file contains the core sandbox planner.
//
// The planner turns Config + Environment into a deterministic plan consisting of:
//   - a deterministic list of bwrap arguments (plan.bwrapArgs)
//
// The planner is responsible for all filesystem-dependent work (glob expansion,
// preset discovery, symlink resolution) and runs during Sandbox construction.
// Per-command resources (temporary files and wrapper FDs) are allocated by
// Sandbox.Command.
import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// plan is the deterministic view derived from Config+Environment.
//
// It is intentionally small:
//   - bwrapArgs drive command construction (Command)
type plan struct {
	// bwrapArgs are the deterministic arguments passed to `bwrap` for this
	// sandbox (everything before the "-- <argv...>" separator).
	bwrapArgs []string

	// needsEmptyFile indicates that at least one exclusion masks a file by
	// mounting an unreadable empty file over it.
	//
	// The planner emits placeholder `--ro-bind-data` arguments for these masks and
	// Command() supplies an always-empty inherited FD (currently /dev/null) to
	// materialize them.
	needsEmptyFile bool

	// wrapperMounts are per-command `--ro-bind-data` mounts for command wrappers.
	// They require exec.Cmd.ExtraFiles and are materialized by Command() at
	// runtime.
	wrapperMounts []roBindDataMount

	// chmods are bwrap --chmod operations applied after wrapper mounts.
	chmods []chmodMount
}

type chmodMount struct {
	path  string
	perms os.FileMode
}

// mountSpec is a single low-level mount operation plus the metadata needed to
// deterministically order mounts.
type mountSpec struct {
	mount     Mount
	pathDepth int
}

// mountPlan is the intermediate product of filesystem planning.
//
// - specs become bwrap arguments
// - needsEmptyFile requests a per-command empty file mask for excluded files.
type mountPlan struct {
	specs []mountSpec

	// needsEmptyFile indicates we emitted an exclusion that masks a file by
	// mounting an unreadable empty file over it.
	needsEmptyFile bool
}

// pathResolver converts caller-provided patterns into absolute host paths.
// It also provides helpers for path ordering.
type pathResolver struct {
	homeDir string
	workDir string
}

func newPathResolver(env Environment) pathResolver {
	return pathResolver{homeDir: env.HomeDir, workDir: env.WorkDir}
}

// Resolve converts a caller-supplied path/pattern into an absolute, cleaned host path.
//
// - "~" and "~/..." are expanded using Environment.HomeDir
// - relative paths are interpreted relative to Environment.WorkDir.
func (p pathResolver) Resolve(path string) string {
	if path == "" {
		return ""
	}

	switch {
	case path == "~":
		path = p.homeDir
	case strings.HasPrefix(path, "~/"):
		path = filepath.Join(p.homeDir, path[2:])
	case !filepath.IsAbs(path):
		path = filepath.Join(p.workDir, path)
	}

	return filepath.Clean(path)
}

func (pathResolver) Depth(path string) int {
	cleaned := filepath.Clean(path)
	if cleaned == "/" {
		return 0
	}

	return strings.Count(cleaned, "/")
}

// planner constructs a deterministic plan from Config+Environment.
//
// The planner performs filesystem-dependent work (glob expansion, symlink
// resolution, preset discovery) during Sandbox construction.
type planner struct {
	cfg   Config
	env   Environment
	paths pathResolver

	args []string
	plan plan
}

func (p *planner) debugf(format string, args ...any) {
	if p.cfg.Debugf == nil {
		return
	}

	p.cfg.Debugf("sandbox(planning): "+format, args...)
}

func buildPlan(v *validated) (*plan, error) {
	p := planner{cfg: v.cfg, env: v.env, paths: newPathResolver(v.env)}

	return p.build()
}

func (p *planner) build() (*plan, error) {
	// This function does not create per-command resources (such as temp files).
	// If the resulting args require per-invocation resources, the plan includes
	// placeholders and Command() is responsible for substituting them and
	// returning a cleanup function.
	p.plan = plan{}
	p.args = make([]string, 0, 64)

	p.appendArgs("--die-with-parent", "--unshare-all")

	networkEnabled := p.cfg.Network == nil || *p.cfg.Network
	if networkEnabled {
		p.appendArgs("--share-net")
	}

	dockerEnabled := p.cfg.Docker != nil && *p.cfg.Docker

	rootMode := p.cfg.BaseFS
	if rootMode == "" {
		rootMode = BaseFSHost
	}

	p.debugf("start workDir=%q homeDir=%q rootMode=%q network=%t docker=%t", p.env.WorkDir, p.env.HomeDir, rootMode, networkEnabled, dockerEnabled)

	switch rootMode {
	case BaseFSHost:
		p.appendMount("--ro-bind", "/", "/")
	case BaseFSEmpty:
		p.appendTmpfs("/")
	default:
		// BaseFS is validated at construction time.
		return nil, internalErrorf("planner.build", "unknown BaseFS %q", rootMode)
	}

	p.appendArgs("--dev", "/dev")
	p.appendArgs("--proc", "/proc")
	p.appendTmpfs("/run")

	// DNS (systemd-resolved) compatibility: on many systems /etc/resolv.conf is a
	// symlink into /run. Since we mount /run as a fresh tmpfs, we need to bind-mount
	// the symlink target's parent directory into /run so DNS keeps working.
	//
	// Only do this when network is enabled.
	if networkEnabled {
		if dnsArgs := dnsResolverArgs(p.debugf); len(dnsArgs) > 0 {
			p.appendArgs(dnsArgs...)
		}
	}

	// Temp directory normalization: bind-mount the host temp dir to /tmp inside
	// the sandbox and set TMPDIR=/tmp. This ensures consistent temp directory
	// behavior regardless of the host's TMPDIR setting.
	//
	// Added early so user/preset mounts can override (e.g., exclude subdirs of /tmp).
	if p.cfg.TempDir != "" {
		p.debugf("tempDir=%q -> /tmp", p.cfg.TempDir)
		p.appendMount("--bind", p.cfg.TempDir, "/tmp")
		p.appendArgs("--setenv", "TMPDIR", "/tmp")
	}

	presetMounts, err := expandPresets(p.cfg.Filesystem.Presets, p.env)
	if err != nil {
		return nil, err
	}

	presetsLabel := p.cfg.Filesystem.Presets
	if presetsLabel == nil {
		presetsLabel = []string{"@all"}
	}

	p.debugf("presets=%v => mounts=%d", presetsLabel, len(presetMounts))

	allMounts := append(slices.Clone(presetMounts), p.cfg.Filesystem.Mounts...)

	policyMounts, extraMounts := splitFilesystemMounts(allMounts)
	p.debugf("mounts total=%d filesystem=%d direct=%d", len(allMounts), len(policyMounts), len(extraMounts))

	resolvedRules, err := resolveAndDedupRules(policyMounts, p.paths, p.debugf)
	if err != nil {
		return nil, err
	}

	p.debugf("resolved filesystem rules=%d", len(resolvedRules))

	fsPlan, err := mountPlanFromResolved(resolvedRules)
	if err != nil {
		return nil, err
	}

	p.debugf("mount plan specs=%d needsEmptyFile=%t", len(fsPlan.specs), fsPlan.needsEmptyFile)

	err = p.appendMountPlan(fsPlan)
	if err != nil {
		return nil, err
	}

	if len(extraMounts) > 0 {
		var extraPlan mountPlan

		extraPlan, err = mountPlanFromExtra(extraMounts, p.paths)
		if err != nil {
			return nil, err
		}

		p.debugf("extra mount plan specs=%d", len(extraPlan.specs))

		err = p.appendMountPlan(extraPlan)
		if err != nil {
			return nil, err
		}
	}

	wrapperPlan, err := buildCommandWrapperPlan(p.cfg.Commands, p.env, p.paths, p.debugf)
	if err != nil {
		return nil, err
	}

	if wrapperPlan.isEmpty() {
		p.debugf("command wrappers disabled")
	} else {
		p.debugf("command wrapper plan dirs=%d realBinaryMounts=%d launcherMounts=%d dataMounts=%d", len(wrapperPlan.dirs), len(wrapperPlan.realBinaryMounts), len(wrapperPlan.launcherMounts), len(wrapperPlan.dataMounts))
	}

	if !wrapperPlan.isEmpty() {
		for _, dirMount := range wrapperPlan.dirs {
			if dirMount.Kind == MountDir && dirMount.Perms != 0 {
				p.plan.chmods = append(p.plan.chmods, chmodMount{path: dirMount.Dst, perms: dirMount.Perms})
			}

			var args []string

			args, err = mountToArgs(dirMount)
			if err != nil {
				return nil, fmt.Errorf("mountToArgs for %s src=%q dst=%q fd=%d perms=%#o: %w", mountKindName(dirMount.Kind), dirMount.Src, dirMount.Dst, dirMount.FD, uint32(dirMount.Perms.Perm()), err)
			}

			p.args = append(p.args, args...)
		}

		for _, m := range wrapperPlan.realBinaryMounts {
			p.appendMount("--ro-bind", m.Src, m.Dst)
		}

		for _, m := range wrapperPlan.launcherMounts {
			p.appendMount("--ro-bind", m.Src, m.Dst)
		}

		p.plan.wrapperMounts = append(p.plan.wrapperMounts, wrapperPlan.dataMounts...)
	}

	// This is appended last so that caller-provided mounts cannot accidentally
	// re-expose the docker socket.
	dockerPlan, err := dockerSocketMountPlan(dockerEnabled, p.env.HostEnv, p.paths, p.debugf)
	if err != nil {
		return nil, err
	}

	err = p.appendMountPlan(dockerPlan)
	if err != nil {
		return nil, err
	}

	p.appendChdir(p.env.WorkDir)

	p.plan.bwrapArgs = p.args

	return &p.plan, nil
}

func (p *planner) appendArgs(parts ...string) {
	p.args = append(p.args, parts...)
}

func (p *planner) appendMount(flag, src, dst string) {
	p.args = append(p.args, flag, src, dst)
}

func (p *planner) appendTmpfs(dst string) {
	p.args = append(p.args, "--tmpfs", dst)
}

func (p *planner) appendChdir(dir string) {
	p.args = append(p.args, "--chdir", dir)
}

func (p *planner) appendMountPlan(plan mountPlan) error {
	for _, spec := range plan.specs {
		if spec.mount.Kind == MountDir && spec.mount.Perms != 0 {
			p.plan.chmods = append(p.plan.chmods, chmodMount{path: spec.mount.Dst, perms: spec.mount.Perms})
		}

		args, err := mountToArgs(spec.mount)
		if err != nil {
			return fmt.Errorf("mountToArgs for %s src=%q dst=%q fd=%d perms=%#o: %w", mountKindName(spec.mount.Kind), spec.mount.Src, spec.mount.Dst, spec.mount.FD, uint32(spec.mount.Perms.Perm()), err)
		}

		p.args = append(p.args, args...)
	}

	if plan.needsEmptyFile {
		p.plan.needsEmptyFile = true
	}

	return nil
}

// emptyDataFD is a sentinel used for file exclusions that are materialized at
// Command() time.
//
// During planning we emit a placeholder FD string in the bwrap argv. Command()
// then opens an always-empty reader (currently /dev/null) as an inherited
// ExtraFile and replaces the placeholder with its child FD number.
const (
	emptyDataFD            = -1
	emptyDataFDPlaceholder = "\x00AGENT_SANDBOX_EMPTYDATAFD\x00"
)

// resolvedRule is a policy rule after all filesystem-dependent expansion.
//
// These are produced from policy mounts (RO/RW/Exclude) by:
//   - resolving ~ and relative paths
//   - expanding globs
//   - resolving symlinks
//   - stat'ing to distinguish files vs directories
//
// resolvedRule retains enough metadata to implement precedence (exact beats glob,
// later beats earlier) deterministically.
type resolvedRule struct {
	// resolved is the absolute host path after all expansions.
	resolved string
	// index is the original mount index for tie-breaking (later wins).
	index     int
	pathDepth int
	// kind is the policy mount kind to apply at resolved.
	kind MountKind
	// useTry indicates that RO/RW mounts should use the corresponding *-try bwrap
	// flags and that missing paths are tolerated at planning time.
	useTry bool
	// isExact reports whether the original policy mount was an exact path.
	isExact bool
	// isDir reports whether resolved is a directory.
	isDir bool
}

// resolveAndDedupRules expands policy mounts into concrete, resolved host paths.
//
// It applies the policy precedence rules:
//   - exact path mounts beat glob mounts
//   - for equal specificity, later mounts win
//
// Missing paths and dangling symlinks:
//   - for *Try policy mounts, they are skipped silently
//   - for strict policy mounts, they are returned as errors
func resolveAndDedupRules(mounts []Mount, paths pathResolver, debugf Debugf) ([]resolvedRule, error) {
	winners := make(map[string]resolvedRule)

	skippedMissingTotal := 0
	skippedEmptyTotal := 0
	globNoMatchTotal := 0
	missingExamples := make([]string, 0, 5)

	for i, mount := range mounts {
		pat := strings.TrimSpace(mount.Dst)
		if pat == "" {
			return nil, internalErrorf("resolveAndDedupRules", "policy mount %d has empty destination (kind=%s)", i, mountKindName(mount.Kind))
		}

		// Policy mounts (RO/RW/Exclude) must not carry low-level mount fields.
		if mount.Src != "" || mount.FD != 0 || mount.Perms != 0 {
			return nil, internalErrorf("resolveAndDedupRules", "policy mount %d has low-level fields set (kind=%s dst=%q src=%q fd=%d perms=%#o)", i, mountKindName(mount.Kind), mount.Dst, mount.Src, mount.FD, uint32(mount.Perms.Perm()))
		}

		allowMissing := false
		useTry := false
		forceType := false
		forceIsDir := false

		switch mount.Kind {
		case MountReadOnlyTry, MountReadWriteTry:
			allowMissing = true
			useTry = true
		case MountExcludeTry:
			allowMissing = true
		case MountExcludeFile:
			forceType = true
			forceIsDir = false
		case MountExcludeDir:
			forceType = true
			forceIsDir = true
		default:
			// Other mount kinds use default values (allowMissing=false, useTry=false, forceType=false)
		}

		expanded := paths.Resolve(pat)
		if expanded == "" {
			return nil, fmt.Errorf("resolved empty path for mount %d (%q)", i, pat)
		}

		if !filepath.IsAbs(expanded) {
			return nil, fmt.Errorf("resolved path %q for mount %d (%q) is not absolute", expanded, i, pat)
		}

		if forceType {
			resolved := filepath.Clean(expanded)

			depth := paths.Depth(resolved)
			if depth > 32767 {
				return nil, fmt.Errorf("resolved path %q (mount %d) is too deeply nested (%d)", resolved, i, depth)
			}

			cand := resolvedRule{
				resolved:  resolved,
				index:     i,
				pathDepth: depth,
				kind:      mount.Kind,
				useTry:    false,
				isExact:   true,
				isDir:     forceIsDir,
			}

			if prev, ok := winners[resolved]; !ok || beatsRule(cand, prev) {
				winners[resolved] = cand
			}

			continue
		}

		isGlob := hasGlobMeta(expanded)

		var matches []string

		if isGlob {
			ms, err := filepath.Glob(expanded)
			if err != nil {
				return nil, fmt.Errorf("invalid glob pattern %q at index %d: %w", expanded, i, err)
			}

			if len(ms) == 0 {
				if allowMissing {
					globNoMatchTotal++

					if debugf != nil {
						debugf("filesystem mounts: glob matched 0 paths (ignored) dst=%q expanded=%q", mount.Dst, expanded)
					}

					continue
				}

				return nil, fmt.Errorf("policy mount %d (%s) %q matched 0 paths", i, mountKindName(mount.Kind), mount.Dst)
			}

			matches = ms
		} else {
			matches = []string{expanded}
		}

		for _, match := range matches {
			if match == "" {
				continue
			}

			resolved, err := filepath.EvalSymlinks(match)
			if err != nil {
				if os.IsNotExist(err) {
					skippedMissingTotal++

					if allowMissing {
						if len(missingExamples) < cap(missingExamples) {
							missingExamples = append(missingExamples, match)
						}

						continue
					}

					return nil, fmt.Errorf("policy mount %d (%s) %q resolves to missing path %q", i, mountKindName(mount.Kind), mount.Dst, match)
				}

				return nil, fmt.Errorf("resolve path %q (mount %d): %w", match, i, err)
			}

			resolved = filepath.Clean(resolved)

			info, err := os.Stat(resolved)
			if err != nil {
				if os.IsNotExist(err) {
					skippedMissingTotal++

					if allowMissing {
						if len(missingExamples) < cap(missingExamples) {
							missingExamples = append(missingExamples, resolved)
						}

						continue
					}

					return nil, fmt.Errorf("policy mount %d (%s) %q resolved to missing path %q", i, mountKindName(mount.Kind), mount.Dst, resolved)
				}

				return nil, fmt.Errorf("stat resolved path %q (mount %d): %w", resolved, i, err)
			}

			depth := paths.Depth(resolved)
			if depth > 32767 {
				return nil, fmt.Errorf("resolved path %q (mount %d) is too deeply nested (%d)", resolved, i, depth)
			}

			cand := resolvedRule{
				resolved:  resolved,
				index:     i,
				pathDepth: depth,
				kind:      mount.Kind,
				useTry:    useTry,
				isExact:   !isGlob,
				isDir:     info.IsDir(),
			}

			if prev, ok := winners[resolved]; !ok || beatsRule(cand, prev) {
				winners[resolved] = cand
			}
		}
	}

	if debugf != nil {
		debugf("filesystem mounts: summary total=%d resolved=%d skippedMissing=%d skippedEmpty=%d globNoMatch=%d", len(mounts), len(winners), skippedMissingTotal, skippedEmptyTotal, globNoMatchTotal)

		if len(missingExamples) > 0 {
			debugf("filesystem mounts: skipped missing examples=%q", missingExamples)
		}
	}

	all := make([]resolvedRule, 0, len(winners))
	for _, r := range winners {
		all = append(all, r)
	}

	return all, nil
}

func beatsRule(ruleA, ruleB resolvedRule) bool {
	// Exact beats glob regardless of rule order.
	if ruleA.isExact && !ruleB.isExact {
		return true
	}

	if !ruleA.isExact && ruleB.isExact {
		return false
	}

	// Otherwise, later wins.
	return ruleA.index > ruleB.index
}

// splitFilesystemMounts partitions mounts into policy mounts and direct mounts.
//
// Policy mounts are RO/RW/Exclude patterns that are resolved against the host
// filesystem and translated into low-level mount operations.
//
// Direct mounts (RoBind, Bind, Tmpfs, Dir, RoBindData, ...) are appended after
// policy mounts in a deterministic order.
func splitFilesystemMounts(mounts []Mount) ([]Mount, []Mount) {
	policy := make([]Mount, 0, len(mounts))
	extra := make([]Mount, 0)

	for _, m := range mounts {
		switch m.Kind {
		case MountReadOnly, MountReadOnlyTry, MountReadWrite, MountReadWriteTry, MountExclude, MountExcludeTry, MountExcludeFile, MountExcludeDir:
			policy = append(policy, m)
		default:
			extra = append(extra, m)
		}
	}

	return policy, extra
}

// mountPlanFromResolved translates resolved policy rules into concrete mounts.
//
// Excluded directories are implemented as tmpfs mounts. Excluded files are
// implemented by mounting an unreadable empty file over the target.
//
// The planner emits placeholder `--ro-bind-data` arguments, and Command()
// supplies an always-empty inherited FD (currently /dev/null) to materialize
// them.
func mountPlanFromResolved(resolved []resolvedRule) (mountPlan, error) {
	specs := make([]mountSpec, 0, len(resolved))
	needsEmptyFile := false

	for _, rule := range resolved {
		spec := mountSpec{pathDepth: rule.pathDepth}
		switch rule.kind {
		case MountReadOnly, MountReadOnlyTry:
			kind := MountRoBind
			if rule.useTry {
				kind = MountRoBindTry
			}

			spec.mount = Mount{Kind: kind, Src: rule.resolved, Dst: rule.resolved}
		case MountReadWrite, MountReadWriteTry:
			kind := MountBind
			if rule.useTry {
				kind = MountBindTry
			}

			spec.mount = Mount{Kind: kind, Src: rule.resolved, Dst: rule.resolved}
		case MountExclude, MountExcludeTry, MountExcludeFile, MountExcludeDir:
			if rule.isDir {
				spec.mount = Mount{Kind: MountTmpfs, Dst: rule.resolved}

				break
			}

			needsEmptyFile = true

			// Ensure parent directories exist *before* the ro-bind-data mount.
			//
			// bwrap auto-creates missing parents, but when a --perms option is in
			// effect and sets group/other perms to zero (as we do for excluded files),
			// those perms can leak into newly-created parent directories.
			parent := filepath.Dir(rule.resolved)
			if parent != "" && parent != "/" && parent != rule.resolved {
				parentDepth := rule.pathDepth
				if parentDepth > 0 {
					parentDepth--
				}

				specs = append(specs, mountSpec{mount: Mount{Kind: MountDir, Dst: parent}, pathDepth: parentDepth})
			}

			spec.mount = Mount{Kind: MountRoBindData, FD: emptyDataFD, Perms: 0o000, Dst: rule.resolved}
		default:
			return mountPlan{}, internalErrorf("mountPlanFromResolved", "invalid resolved kind %s for %q", mountKindName(rule.kind), rule.resolved)
		}

		specs = append(specs, spec)
	}

	// Sort from shallowest destination to deepest so that parent mounts are applied
	// before child mounts. This is crucial for correctness: later mounts can
	// re-expose paths inside excluded directories.
	sort.Slice(specs, func(i, j int) bool {
		if specs[i].pathDepth != specs[j].pathDepth {
			return specs[i].pathDepth < specs[j].pathDepth
		}

		return specs[i].mount.Dst < specs[j].mount.Dst
	})

	return mountPlan{specs: specs, needsEmptyFile: needsEmptyFile}, nil
}

// mountPlanFromExtra converts direct mounts into a mountPlan.
//
// Direct mounts are sorted to ensure deterministic output and to avoid accidental
// shadowing (parents are mounted before children).
func mountPlanFromExtra(mounts []Mount, paths pathResolver) (mountPlan, error) {
	extra := slices.Clone(mounts)
	sort.Slice(extra, func(left, right int) bool {
		di, dj := paths.Depth(extra[left].Dst), paths.Depth(extra[right].Dst)
		if di != dj {
			return di < dj
		}

		if extra[left].Dst != extra[right].Dst {
			return extra[left].Dst < extra[right].Dst
		}

		if extra[left].Kind != extra[right].Kind {
			return extra[left].Kind < extra[right].Kind
		}

		return extra[left].Src < extra[right].Src
	})

	specs := make([]mountSpec, 0, len(extra))
	for _, mount := range extra {
		spec, err := mountSpecFromExtra(mount, paths)
		if err != nil {
			return mountPlan{}, fmt.Errorf("direct mount %s src=%q dst=%q fd=%d perms=%#o: %w", mountKindName(mount.Kind), mount.Src, mount.Dst, mount.FD, uint32(mount.Perms.Perm()), err)
		}

		switch mount.Kind {
		case MountRoBind, MountRoBindTry, MountBind, MountBindTry:
			_, statErr := os.Stat(mount.Src)
			if statErr != nil {
				if os.IsNotExist(statErr) {
					if mount.Kind == MountRoBindTry || mount.Kind == MountBindTry {
						continue
					}

					return mountPlan{}, fmt.Errorf("direct mount %s src=%q dst=%q fd=%d perms=%#o: source does not exist", mountKindName(mount.Kind), mount.Src, mount.Dst, mount.FD, uint32(mount.Perms.Perm()))
				}

				return mountPlan{}, fmt.Errorf("direct mount %s src=%q dst=%q fd=%d perms=%#o: stat source %q: %w", mountKindName(mount.Kind), mount.Src, mount.Dst, mount.FD, uint32(mount.Perms.Perm()), mount.Src, statErr)
			}
		default:
			// Other mount kinds don't require source stat validation
		}

		specs = append(specs, spec)
	}

	return mountPlan{specs: specs}, nil
}

// mountSpecFromExtra validates and annotates a single direct mount.
func mountSpecFromExtra(mnt Mount, paths pathResolver) (mountSpec, error) {
	if strings.TrimSpace(mnt.Dst) == "" {
		return mountSpec{}, internalErrorf("mountSpecFromExtra", "dst is empty (kind=%s src=%q fd=%d perms=%#o)", mountKindName(mnt.Kind), mnt.Src, mnt.FD, uint32(mnt.Perms.Perm()))
	}

	if !filepath.IsAbs(mnt.Dst) {
		return mountSpec{}, internalErrorf("mountSpecFromExtra", "dst %q is not absolute (kind=%s src=%q)", mnt.Dst, mountKindName(mnt.Kind), mnt.Src)
	}

	switch mnt.Kind {
	case MountReadOnly, MountReadOnlyTry, MountReadWrite, MountReadWriteTry, MountExclude, MountExcludeTry, MountExcludeFile, MountExcludeDir:
		return mountSpec{}, internalErrorf("mountSpecFromExtra", "called on policy mount kind=%s dst=%q", mountKindName(mnt.Kind), mnt.Dst)
	case MountRoBind, MountRoBindTry:
		if strings.TrimSpace(mnt.Src) == "" || !filepath.IsAbs(mnt.Src) {
			return mountSpec{}, internalErrorf("mountSpecFromExtra", "ro-bind source %q is invalid (dst=%q kind=%s)", mnt.Src, mnt.Dst, mountKindName(mnt.Kind))
		}

	case MountBind, MountBindTry:
		if strings.TrimSpace(mnt.Src) == "" || !filepath.IsAbs(mnt.Src) {
			return mountSpec{}, internalErrorf("mountSpecFromExtra", "bind source %q is invalid (dst=%q kind=%s)", mnt.Src, mnt.Dst, mountKindName(mnt.Kind))
		}

	case MountTmpfs:
		if mnt.Src != "" {
			return mountSpec{}, internalErrorf("mountSpecFromExtra", "tmpfs mount has src %q (dst=%q)", mnt.Src, mnt.Dst)
		}

	case MountDir:
		if mnt.Src != "" {
			return mountSpec{}, internalErrorf("mountSpecFromExtra", "dir mount has src %q (dst=%q)", mnt.Src, mnt.Dst)
		}

	case MountRoBindData:
		if mnt.Src != "" || mnt.FD <= 0 {
			return mountSpec{}, internalErrorf("mountSpecFromExtra", "ro-bind-data mount invalid (dst=%q src=%q fd=%d perms=%#o)", mnt.Dst, mnt.Src, mnt.FD, uint32(mnt.Perms.Perm()))
		}

	default:
		return mountSpec{}, internalErrorf("mountSpecFromExtra", "unknown mount kind %d (src=%q dst=%q fd=%d perms=%#o)", mnt.Kind, mnt.Src, mnt.Dst, mnt.FD, uint32(mnt.Perms.Perm()))
	}

	depth := paths.Depth(mnt.Dst)
	if depth > 32767 {
		return mountSpec{}, internalErrorf("mountSpecFromExtra", "dst %q is too deeply nested (%d)", mnt.Dst, depth)
	}

	return mountSpec{
		mount:     mnt,
		pathDepth: depth,
	}, nil
}

// mountKindName returns a stable, human-readable name for a MountKind.
func mountKindName(kind MountKind) string {
	switch kind {
	case MountReadOnly:
		return "read-only"
	case MountReadOnlyTry:
		return "read-only-try"
	case MountReadWrite:
		return "read-write"
	case MountReadWriteTry:
		return "read-write-try"
	case MountExclude:
		return "exclude"
	case MountExcludeTry:
		return "exclude-try"
	case MountExcludeFile:
		return "exclude-file"
	case MountExcludeDir:
		return "exclude-dir"
	case MountRoBind:
		return "ro-bind"
	case MountRoBindTry:
		return "ro-bind-try"
	case MountBind:
		return "bind"
	case MountBindTry:
		return "bind-try"
	case MountTmpfs:
		return "tmpfs"
	case MountDir:
		return "dir"
	case MountRoBindData:
		return "ro-bind-data"
	default:
		return fmt.Sprintf("unknown(%d)", kind)
	}
}

// mountToArgs converts a low-level Mount into the corresponding bwrap CLI arguments.
//
// Policy mounts (RO/RW/Exclude) are rejected here; they must be resolved to
// concrete mounts first.
func mountToArgs(mnt Mount) ([]string, error) {
	switch mnt.Kind {
	case MountReadOnly, MountReadOnlyTry, MountReadWrite, MountReadWriteTry, MountExclude, MountExcludeTry, MountExcludeFile, MountExcludeDir:
		return nil, internalErrorf("mountToArgs", "called on policy mount kind=%s dst=%q", mountKindName(mnt.Kind), mnt.Dst)
	case MountRoBind:
		return []string{"--ro-bind", mnt.Src, mnt.Dst}, nil
	case MountRoBindTry:
		return []string{"--ro-bind-try", mnt.Src, mnt.Dst}, nil
	case MountBind:
		return []string{"--bind", mnt.Src, mnt.Dst}, nil
	case MountBindTry:
		return []string{"--bind-try", mnt.Src, mnt.Dst}, nil
	case MountTmpfs:
		return []string{"--tmpfs", mnt.Dst}, nil
	case MountDir:
		return []string{"--dir", mnt.Dst}, nil
	case MountRoBindData:
		var fdString string

		switch {
		case mnt.FD == emptyDataFD:
			fdString = emptyDataFDPlaceholder
		case mnt.FD <= 0:
			return nil, internalErrorf("mountToArgs", "ro-bind-data mount has invalid FD %d (dst=%q)", mnt.FD, mnt.Dst)
		default:
			fdString = strconv.Itoa(mnt.FD)
		}

		perms := mnt.Perms

		// Note: bwrap expects an octal string (e.g. 0555) for --perms.
		permString := fmt.Sprintf("%04o", perms.Perm())

		return []string{"--perms", permString, "--ro-bind-data", fdString, mnt.Dst}, nil
	default:
		return nil, internalErrorf("mountToArgs", "unknown mount kind %d (src=%q dst=%q fd=%d perms=%#o)", mnt.Kind, mnt.Src, mnt.Dst, mnt.FD, uint32(mnt.Perms.Perm()))
	}
}

func hasGlobMeta(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}
