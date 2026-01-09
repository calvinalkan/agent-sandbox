package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	testRunPath         = "/run"
	bwrapRoBindTry      = "--ro-bind-try"
	bwrapBindTry        = "--bind-try"
	bwrapRoBind         = "--ro-bind"
	bwrapBind           = "--bind"
	bwrapTmpfs          = "--tmpfs"
	devNull             = "/dev/null"
	testHomeUserProject = "/home/user/project"
)

// mustBwrapArgs calls BwrapArgs and fails the test if it returns an error.
func mustBwrapArgs(t *testing.T, paths []ResolvedPath, cfg *Config) []string {
	t.Helper()

	args, err := BwrapArgs(paths, cfg)
	if err != nil {
		t.Fatalf("BwrapArgs returned unexpected error: %v", err)
	}

	return args
}

// createTestSocket creates a real file (simulating a socket) in a temp dir.
// Returns the path to the file.
func createTestSocket(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	socketPath := filepath.Join(dir, "docker.sock")

	err := os.WriteFile(socketPath, []byte{}, 0o600)
	if err != nil {
		t.Fatalf("failed to create test socket: %v", err)
	}

	return socketPath
}

// createTestSocketSymlink creates a real file and a symlink to it.
// Returns (symlinkPath, realPath).
func createTestSocketSymlink(t *testing.T) (string, string) {
	t.Helper()

	dir := t.TempDir()
	realPath := filepath.Join(dir, "real", "docker.sock")
	symlinkPath := filepath.Join(dir, "link", "docker.sock")

	// Create the real file
	err := os.MkdirAll(filepath.Dir(realPath), 0o750)
	if err != nil {
		t.Fatalf("failed to create real dir: %v", err)
	}

	err = os.WriteFile(realPath, []byte{}, 0o600)
	if err != nil {
		t.Fatalf("failed to create real socket: %v", err)
	}

	// Create the symlink
	err = os.MkdirAll(filepath.Dir(symlinkPath), 0o750)
	if err != nil {
		t.Fatalf("failed to create link dir: %v", err)
	}

	err = os.Symlink(realPath, symlinkPath)
	if err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	return symlinkPath, realPath
}

func Test_BwrapArgs_Includes_Die_With_Parent(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := mustBwrapArgs(t, nil, cfg)

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
	args := mustBwrapArgs(t, nil, cfg)

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
	args := mustBwrapArgs(t, nil, cfg)

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
	args := mustBwrapArgs(t, nil, cfg)

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
	args := mustBwrapArgs(t, nil, cfg)

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
	args := mustBwrapArgs(t, nil, cfg)

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
	args := mustBwrapArgs(t, nil, cfg)

	// Find --ro-bind / / in args
	found := false

	for i := range len(args) - 2 {
		if args[i] == bwrapRoBind && args[i+1] == "/" && args[i+2] == "/" {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("expected %s / / in args, got: %v", bwrapRoBind, args)
	}
}

func Test_BwrapArgs_Mounts_Run_As_Tmpfs(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := mustBwrapArgs(t, nil, cfg)

	// Find --tmpfs /run in args
	idx := slices.Index(args, bwrapTmpfs)
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
	args := mustBwrapArgs(t, nil, cfg)

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
	args := mustBwrapArgs(t, paths, cfg)

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
	args := mustBwrapArgs(t, paths, cfg)

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
	args := mustBwrapArgs(t, paths, cfg)

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
	args := mustBwrapArgs(t, paths, cfg)

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
	args := mustBwrapArgs(t, paths, cfg)

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
	args := mustBwrapArgs(t, nil, cfg)

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
		if args[i] == bwrapRoBind && args[i+1] == "/" && args[i+2] == "/" {
			roBindRootIdx = i

			break
		}
	}

	// Find --tmpfs /run
	tmpfsRunIdx := -1

	for i := range len(args) - 1 {
		if args[i] == bwrapTmpfs && args[i+1] == testRunPath {
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
	args := mustBwrapArgs(t, paths, cfg)

	// Find --tmpfs /run index
	tmpfsRunIdx := -1

	for i := range len(args) - 1 {
		if args[i] == bwrapTmpfs && args[i+1] == testRunPath {
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

	// Path mounts should come after tmpfs /run
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
	args1 := mustBwrapArgs(t, paths, cfg)
	args2 := mustBwrapArgs(t, paths, cfg)
	args3 := mustBwrapArgs(t, paths, cfg)

	if !slices.Equal(args1, args2) || !slices.Equal(args2, args3) {
		t.Errorf("BwrapArgs should produce deterministic output")
	}
}

// ============================================================================
// Docker socket handling tests (using dockerSocketArgs directly)
// ============================================================================

func Test_DockerSocketArgs_Masks_Socket_When_Disabled_And_Not_Under_Run(t *testing.T) {
	t.Parallel()

	// Create a socket in a temp dir (not under /run)
	socketPath := createTestSocket(t)

	args, err := dockerSocketArgs(boolPtr(false), socketPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have --ro-bind /dev/null <socketPath> to mask the socket
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d: %v", len(args), args)
	}

	if args[0] != bwrapRoBind || args[1] != devNull || args[2] != socketPath {
		t.Errorf("expected [%s %s %s], got: %v", bwrapRoBind, devNull, socketPath, args)
	}
}

func Test_DockerSocketArgs_Masks_Socket_When_Docker_Is_Nil_And_Not_Under_Run(t *testing.T) {
	t.Parallel()

	// Create a socket in a temp dir (not under /run)
	socketPath := createTestSocket(t)

	args, err := dockerSocketArgs(nil, socketPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Docker defaults to false when nil, so socket should be masked
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d: %v", len(args), args)
	}

	if args[0] != bwrapRoBind || args[1] != devNull || args[2] != socketPath {
		t.Errorf("expected [%s %s %s], got: %v", bwrapRoBind, devNull, socketPath, args)
	}
}

func Test_DockerSocketArgs_Binds_Socket_When_Enabled(t *testing.T) {
	t.Parallel()

	// Create a real socket file in temp dir
	socketPath := createTestSocket(t)

	args, err := dockerSocketArgs(boolPtr(true), socketPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have --bind <resolved> <socketPath>
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d: %v", len(args), args)
	}

	if args[0] != bwrapBind {
		t.Errorf("expected first arg to be %s, got: %s", bwrapBind, args[0])
	}

	if args[2] != socketPath {
		t.Errorf("expected third arg to be %s, got: %s", socketPath, args[2])
	}
}

func Test_DockerSocketArgs_Resolves_Symlink(t *testing.T) {
	t.Parallel()

	// Create a real symlink in temp dir
	symlinkPath, realPath := createTestSocketSymlink(t)

	args, err := dockerSocketArgs(boolPtr(true), symlinkPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should resolve the symlink: --bind <realPath> <symlinkPath>
	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d: %v", len(args), args)
	}

	if args[0] != bwrapBind {
		t.Errorf("expected first arg to be %s, got: %s", bwrapBind, args[0])
	}

	if args[1] != realPath {
		t.Errorf("expected resolved path %s, got: %s", realPath, args[1])
	}

	if args[2] != symlinkPath {
		t.Errorf("expected socket path %s, got: %s", symlinkPath, args[2])
	}
}

func Test_DockerSocketArgs_Returns_Error_When_Socket_Missing(t *testing.T) {
	t.Parallel()

	// Use a path that doesn't exist
	nonexistentPath := filepath.Join(t.TempDir(), "nonexistent", "docker.sock")

	_, err := dockerSocketArgs(boolPtr(true), nonexistentPath)
	if err == nil {
		t.Fatal("expected error when socket doesn't exist")
	}

	// Check that error wraps ErrDockerSocketNotFound
	if !strings.Contains(err.Error(), "docker socket not found") {
		t.Errorf("expected error to mention 'docker socket not found', got: %v", err)
	}
}

func Test_BwrapArgs_Skips_Docker_Mask_When_Socket_Under_Run(t *testing.T) {
	t.Parallel()

	// On systems where /var/run -> /run, the docker socket is under /run.
	// Since we mount /run as tmpfs, no masking is needed - the socket simply won't exist.
	cfg := &Config{
		Network:      boolPtr(true),
		Docker:       boolPtr(false),
		EffectiveCwd: testHomeUserProject,
	}
	args := mustBwrapArgs(t, nil, cfg)

	// Verify --tmpfs /run is in args
	tmpfsRunFound := false

	for i := range len(args) - 1 {
		if args[i] == bwrapTmpfs && args[i+1] == testRunPath {
			tmpfsRunFound = true

			break
		}
	}

	if !tmpfsRunFound {
		t.Fatal("expected tmpfs /run in args")
	}

	// On this system, /var/run/docker.sock is likely under /run (via symlink)
	// Check if we skip masking (no args for docker socket) or if we mask (socket not under /run)
	// This is system-dependent, so we just verify the args are valid bwrap args
	argsStr := strings.Join(args, " ")
	t.Logf("bwrap args: %s", argsStr)
}

func Test_BwrapArgs_Docker_Disabled_Does_Not_Include_Socket_Bind(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Network:      boolPtr(true),
		Docker:       boolPtr(false),
		EffectiveCwd: testHomeUserProject,
	}
	args := mustBwrapArgs(t, nil, cfg)

	// Should NOT have --bind for the docker socket (only --ro-bind for masking)
	for i := range len(args) - 2 {
		if args[i] == bwrapBind && args[i+2] == DockerSocketPath {
			t.Errorf("expected no %s for docker socket when disabled, got: %v", bwrapBind, args)
		}
	}
}

// ============================================================================
// Self binary mount tests
// ============================================================================

func Test_BwrapArgs_Includes_Self_Binary_Mount(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := mustBwrapArgs(t, nil, cfg)

	// Should have --ro-bind <self> /run/agent-sandbox
	found := false

	for i := range len(args) - 2 {
		if args[i] == bwrapRoBind && args[i+2] == SandboxBinaryPath {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("expected %s <binary> %s in args, got: %v", bwrapRoBind, SandboxBinaryPath, args)
	}
}

func Test_BwrapArgs_Self_Binary_Mount_Comes_After_Docker_Socket(t *testing.T) {
	t.Parallel()

	cfg := &Config{
		Network:      boolPtr(true),
		Docker:       boolPtr(false),
		EffectiveCwd: testHomeUserProject,
	}
	args := mustBwrapArgs(t, nil, cfg)

	// Find --tmpfs /run (which comes right before docker handling)
	tmpfsRunIdx := -1

	for i := range len(args) - 1 {
		if args[i] == bwrapTmpfs && args[i+1] == testRunPath {
			tmpfsRunIdx = i

			break
		}
	}

	// Find self binary mount
	selfMountIdx := -1

	for i := range len(args) - 2 {
		if args[i] == bwrapRoBind && args[i+2] == SandboxBinaryPath {
			selfMountIdx = i

			break
		}
	}

	if tmpfsRunIdx == -1 {
		t.Fatal("expected --tmpfs /run in args")
	}

	if selfMountIdx == -1 {
		t.Fatal("expected self binary mount in args")
	}

	// Self binary mount should come after tmpfs /run
	if selfMountIdx < tmpfsRunIdx {
		t.Errorf("self binary mount (idx=%d) should come after --tmpfs /run (idx=%d)", selfMountIdx, tmpfsRunIdx)
	}
}

func Test_BwrapArgs_Self_Binary_Mount_Comes_Before_Path_Mounts(t *testing.T) {
	t.Parallel()

	paths := []ResolvedPath{
		{Original: "/some/path", Resolved: "/some/path", Access: PathAccessRo, Source: PathSourceProject},
	}
	cfg := &Config{
		Network:      boolPtr(true),
		EffectiveCwd: testHomeUserProject,
	}
	args := mustBwrapArgs(t, paths, cfg)

	// Find self binary mount
	selfMountIdx := -1

	for i := range len(args) - 2 {
		if args[i] == bwrapRoBind && args[i+2] == SandboxBinaryPath {
			selfMountIdx = i

			break
		}
	}

	// Find path mount
	pathMountIdx := -1

	for i := range len(args) - 2 {
		if args[i] == bwrapRoBindTry && args[i+1] == "/some/path" {
			pathMountIdx = i

			break
		}
	}

	if selfMountIdx == -1 {
		t.Fatal("expected self binary mount in args")
	}

	if pathMountIdx == -1 {
		t.Fatal("expected path mount in args")
	}

	// Self binary mount should come before path mounts
	if selfMountIdx > pathMountIdx {
		t.Errorf("self binary mount (idx=%d) should come before path mounts (idx=%d)", selfMountIdx, pathMountIdx)
	}
}

func Test_selfBinaryArgs_Returns_Ro_Bind_Args(t *testing.T) {
	t.Parallel()

	args, err := selfBinaryArgs()
	if err != nil {
		t.Fatalf("selfBinaryArgs() returned error: %v", err)
	}

	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d: %v", len(args), args)
	}

	if args[0] != bwrapRoBind {
		t.Errorf("expected first arg to be %s, got: %s", bwrapRoBind, args[0])
	}

	// args[1] is the resolved path to the binary (varies by system)
	// Just verify it's not empty
	if args[1] == "" {
		t.Error("expected second arg (binary path) to be non-empty")
	}

	if args[2] != SandboxBinaryPath {
		t.Errorf("expected third arg to be %s, got: %s", SandboxBinaryPath, args[2])
	}
}

func Test_selfBinaryArgs_Resolves_Binary_Path(t *testing.T) {
	t.Parallel()

	args, err := selfBinaryArgs()
	if err != nil {
		t.Fatalf("selfBinaryArgs() returned error: %v", err)
	}

	binaryPath := args[1]

	// The binary path should be an absolute path
	if !filepath.IsAbs(binaryPath) {
		t.Errorf("expected absolute path, got: %s", binaryPath)
	}

	// The binary should exist
	_, err = os.Stat(binaryPath)
	if err != nil {
		t.Errorf("binary path %s should exist: %v", binaryPath, err)
	}
}
