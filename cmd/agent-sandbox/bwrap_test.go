package main

import (
	"slices"
	"strings"
	"testing"
)

const (
	testRunPath         = "/run"
	bwrapRoBindTry      = "--ro-bind-try"
	bwrapBindTry        = "--bind-try"
	testHomeUserProject = "/home/user/project"
)

func Test_BwrapArgs_Includes_Die_With_Parent(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := BwrapArgs(nil, cfg)

	if !slices.Contains(args, "--die-with-parent") {
		t.Errorf("expected --die-with-parent in args, got: %v", args)
	}
}

func Test_BwrapArgs_Includes_Unshare_All(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := BwrapArgs(nil, cfg)

	if !slices.Contains(args, "--unshare-all") {
		t.Errorf("expected --unshare-all in args, got: %v", args)
	}
}

func Test_BwrapArgs_Includes_Share_Net_When_Network_Enabled(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := BwrapArgs(nil, cfg)

	if !slices.Contains(args, "--share-net") {
		t.Errorf("expected --share-net in args when network enabled, got: %v", args)
	}
}

func Test_BwrapArgs_Omits_Share_Net_When_Network_Disabled(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Network:      boolPtr(false),
		EffectiveCwd: testHomeUserProject,
	}
	args := BwrapArgs(nil, cfg)

	if slices.Contains(args, "--share-net") {
		t.Errorf("expected no --share-net in args when network disabled, got: %v", args)
	}
}

func Test_BwrapArgs_Mounts_Dev_Virtual(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := BwrapArgs(nil, cfg)

	// Find the --dev flag and check its argument
	idx := slices.Index(args, "--dev")
	if idx == -1 {
		t.Fatalf("expected --dev in args, got: %v", args)
	}

	if idx+1 >= len(args) || args[idx+1] != "/dev" {
		t.Errorf("expected --dev /dev, got --dev %s", args[idx+1])
	}
}

func Test_BwrapArgs_Mounts_Proc_Virtual(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := BwrapArgs(nil, cfg)

	// Find the --proc flag and check its argument
	idx := slices.Index(args, "--proc")
	if idx == -1 {
		t.Fatalf("expected --proc in args, got: %v", args)
	}

	if idx+1 >= len(args) || args[idx+1] != "/proc" {
		t.Errorf("expected --proc /proc, got --proc %s", args[idx+1])
	}
}

func Test_BwrapArgs_Mounts_Root_Readonly_First(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := BwrapArgs(nil, cfg)

	// Find --ro-bind / / in args
	found := false

	for i := range len(args) - 2 {
		if args[i] == "--ro-bind" && args[i+1] == "/" && args[i+2] == "/" {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("expected --ro-bind / / in args, got: %v", args)
	}
}

func Test_BwrapArgs_Mounts_Run_As_Tmpfs(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := BwrapArgs(nil, cfg)

	// Find --tmpfs /run in args
	idx := slices.Index(args, "--tmpfs")
	if idx == -1 {
		t.Fatalf("expected --tmpfs in args, got: %v", args)
	}

	if idx+1 >= len(args) || args[idx+1] != testRunPath {
		t.Errorf("expected --tmpfs /run, got --tmpfs %s", args[idx+1])
	}
}

func Test_BwrapArgs_Sets_Chdir(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := BwrapArgs(nil, cfg)

	idx := slices.Index(args, "--chdir")
	if idx == -1 {
		t.Fatalf("expected --chdir in args, got: %v", args)
	}

	if idx+1 >= len(args) || args[idx+1] != testHomeUserProject {
		t.Errorf("expected --chdir /home/user/project, got --chdir %s", args[idx+1])
	}
}

func Test_BwrapArgs_Generates_Ro_Bind_Try_For_Ro_Paths(t *testing.T) {
	t.Parallel()

	paths := []ResolvedPath{
		{Original: "~/code", Resolved: "/home/user/code", Access: PathAccessRo, Source: PathSourceProject},
	}
	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := BwrapArgs(paths, cfg)

	// Find --ro-bind-try /home/user/code /home/user/code
	found := false

	for i := range len(args) - 2 {
		if args[i] == bwrapRoBindTry && args[i+1] == "/home/user/code" && args[i+2] == "/home/user/code" {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("expected --ro-bind-try /home/user/code /home/user/code in args, got: %v", args)
	}
}

func Test_BwrapArgs_Generates_Bind_Try_For_Rw_Paths(t *testing.T) {
	t.Parallel()

	paths := []ResolvedPath{
		{Original: ".generated", Resolved: "/home/user/project/.generated", Access: PathAccessRw, Source: PathSourceProject},
	}
	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := BwrapArgs(paths, cfg)

	// Find --bind-try /home/user/project/.generated /home/user/project/.generated
	found := false

	for i := range len(args) - 2 {
		if args[i] == bwrapBindTry && args[i+1] == "/home/user/project/.generated" && args[i+2] == "/home/user/project/.generated" {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("expected --bind-try for rw path in args, got: %v", args)
	}
}

func Test_BwrapArgs_Skips_Exclude_Paths(t *testing.T) {
	t.Parallel()

	paths := []ResolvedPath{
		{Original: "~/.aws", Resolved: "/home/user/.aws", Access: PathAccessExclude, Source: PathSourcePreset},
	}
	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := BwrapArgs(paths, cfg)

	// Exclude paths should NOT appear in args (handled by d5g3tgg)
	argsStr := strings.Join(args, " ")
	if strings.Contains(argsStr, ".aws") {
		t.Errorf("exclude path should not appear in args (deferred to d5g3tgg), got: %v", args)
	}
}

func Test_BwrapArgs_Maintains_Path_Order_For_Mount_Overlay(t *testing.T) {
	t.Parallel()

	// Paths should be mounted in order (shallower first) so deeper paths overlay
	// The caller is responsible for sorting via ResolveAndSort, but we verify
	// that BwrapArgs preserves the input order
	paths := []ResolvedPath{
		{Original: "/home", Resolved: "/home", Access: PathAccessRo, Source: PathSourcePreset},
		{Original: "/home/user", Resolved: "/home/user", Access: PathAccessRw, Source: PathSourceProject},
		{Original: "/home/user/project", Resolved: testHomeUserProject, Access: PathAccessRw, Source: PathSourceProject},
	}
	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := BwrapArgs(paths, cfg)

	// Find indices of each path mount
	homeIdx := -1
	userIdx := -1
	projectIdx := -1

	for i := range len(args) - 2 {
		if (args[i] == bwrapRoBindTry || args[i] == bwrapBindTry) && args[i+1] == "/home" {
			homeIdx = i
		}

		if (args[i] == bwrapRoBindTry || args[i] == bwrapBindTry) && args[i+1] == "/home/user" {
			userIdx = i
		}

		if (args[i] == bwrapRoBindTry || args[i] == bwrapBindTry) && args[i+1] == testHomeUserProject {
			projectIdx = i
		}
	}

	if homeIdx == -1 || userIdx == -1 || projectIdx == -1 {
		t.Fatalf("expected all paths to be mounted, homeIdx=%d userIdx=%d projectIdx=%d", homeIdx, userIdx, projectIdx)
	}

	// Verify order: /home before /home/user before /home/user/project
	if homeIdx >= userIdx || userIdx >= projectIdx {
		t.Errorf("paths should be mounted in depth order (shallow first): homeIdx=%d userIdx=%d projectIdx=%d", homeIdx, userIdx, projectIdx)
	}
}

func Test_BwrapArgs_Handles_Multiple_Path_Types(t *testing.T) {
	t.Parallel()

	paths := []ResolvedPath{
		{Original: "~/ro-dir", Resolved: "/home/user/ro-dir", Access: PathAccessRo, Source: PathSourceProject},
		{Original: "~/rw-dir", Resolved: "/home/user/rw-dir", Access: PathAccessRw, Source: PathSourceProject},
		{Original: "~/exclude-dir", Resolved: "/home/user/exclude-dir", Access: PathAccessExclude, Source: PathSourcePreset},
	}
	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := BwrapArgs(paths, cfg)

	// Check ro path is --ro-bind-try
	foundRo := false

	for i := range len(args) - 2 {
		if args[i] == bwrapRoBindTry && args[i+1] == "/home/user/ro-dir" {
			foundRo = true

			break
		}
	}

	if !foundRo {
		t.Errorf("expected --ro-bind-try for ro path")
	}

	// Check rw path is --bind-try
	foundRw := false

	for i := range len(args) - 2 {
		if args[i] == bwrapBindTry && args[i+1] == "/home/user/rw-dir" {
			foundRw = true

			break
		}
	}

	if !foundRw {
		t.Errorf("expected --bind-try for rw path")
	}

	// Check exclude path is NOT in args
	argsStr := strings.Join(args, " ")
	if strings.Contains(argsStr, "exclude-dir") {
		t.Errorf("exclude path should not appear in args")
	}
}

func Test_BwrapArgs_Base_Order_Is_Correct(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := BwrapArgs(nil, cfg)

	// Verify the base order: die-with-parent, unshare-all, share-net, dev, proc, ro-bind /, tmpfs /run, chdir
	// Find indices of key arguments
	dieIdx := slices.Index(args, "--die-with-parent")
	unshareIdx := slices.Index(args, "--unshare-all")
	shareNetIdx := slices.Index(args, "--share-net")
	devIdx := slices.Index(args, "--dev")
	procIdx := slices.Index(args, "--proc")
	chdirIdx := slices.Index(args, "--chdir")

	// Find --ro-bind / /
	roBindRootIdx := -1

	for i := range len(args) - 2 {
		if args[i] == "--ro-bind" && args[i+1] == "/" && args[i+2] == "/" {
			roBindRootIdx = i

			break
		}
	}

	// Find --tmpfs /run
	tmpfsRunIdx := -1

	for i := range len(args) - 1 {
		if args[i] == "--tmpfs" && args[i+1] == testRunPath {
			tmpfsRunIdx = i

			break
		}
	}

	// Verify order
	if dieIdx > unshareIdx {
		t.Errorf("--die-with-parent should come before --unshare-all")
	}

	if unshareIdx > shareNetIdx {
		t.Errorf("--unshare-all should come before --share-net")
	}

	if shareNetIdx > devIdx {
		t.Errorf("--share-net should come before --dev")
	}

	if devIdx > procIdx {
		t.Errorf("--dev should come before --proc")
	}

	if procIdx > roBindRootIdx {
		t.Errorf("--proc should come before --ro-bind / /")
	}

	if roBindRootIdx > tmpfsRunIdx {
		t.Errorf("--ro-bind / / should come before --tmpfs /run")
	}

	if tmpfsRunIdx > chdirIdx {
		t.Errorf("--tmpfs /run should come before --chdir")
	}
}

func Test_BwrapArgs_Path_Mounts_Come_After_Base_Mounts(t *testing.T) {
	t.Parallel()

	paths := []ResolvedPath{
		{Original: "/some/path", Resolved: "/some/path", Access: PathAccessRo, Source: PathSourceProject},
	}
	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := BwrapArgs(paths, cfg)

	// Find --tmpfs /run index
	tmpfsRunIdx := -1

	for i := range len(args) - 1 {
		if args[i] == "--tmpfs" && args[i+1] == testRunPath {
			tmpfsRunIdx = i

			break
		}
	}

	// Find path mount index
	pathMountIdx := -1

	for i := range len(args) - 2 {
		if args[i] == bwrapRoBindTry && args[i+1] == "/some/path" {
			pathMountIdx = i

			break
		}
	}

	// Path mounts should come after --tmpfs /run
	if pathMountIdx < tmpfsRunIdx {
		t.Errorf("path mounts should come after base mounts (tmpfs /run)")
	}

	// And before --chdir
	chdirIdx := slices.Index(args, "--chdir")
	if pathMountIdx > chdirIdx {
		t.Errorf("path mounts should come before --chdir")
	}
}

func Test_BwrapArgs_Deterministic_Output(t *testing.T) {
	t.Parallel()

	paths := []ResolvedPath{
		{Original: "/a", Resolved: "/a", Access: PathAccessRo, Source: PathSourcePreset},
		{Original: "/b", Resolved: "/b", Access: PathAccessRw, Source: PathSourceProject},
		{Original: "/c", Resolved: "/c", Access: PathAccessRo, Source: PathSourceCLI},
	}
	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}

	// Run multiple times and verify same output
	args1 := BwrapArgs(paths, cfg)
	args2 := BwrapArgs(paths, cfg)
	args3 := BwrapArgs(paths, cfg)

	if !slices.Equal(args1, args2) || !slices.Equal(args2, args3) {
		t.Errorf("BwrapArgs should produce deterministic output")
	}
}
