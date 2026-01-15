# agent-sandbox - Technical Steering

High-level technical direction for building agent-sandbox.

---

## Core Premise

This is a **wrapper around bwrap** (bubblewrap). We don't implement sandboxing - bwrap does the hard work. Our job is to:

1. Provide a user-friendly configuration layer
2. Resolve configs, presets, and paths into bwrap arguments
3. Execute bwrap with the right arguments

---

## Language & Distribution

**Go.** Single static binary, no runtime dependencies.

**External dependency:** `bwrap` must be installed and available in PATH.

**Minimal Go dependencies:**
- `github.com/spf13/pflag` - POSIX flag parsing
- `github.com/tailscale/hujson` - JSONC parsing

Nothing else.

---

## Dataflow

```
┌─────────────────────────────────────────────────────────────┐
│                        INPUT                                │
│  CLI args, env, config files (global + project)             │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    CONFIG LOADING                           │
│  Layer by layer, each overwrites previous:                  │
│  defaults → global config → project config → CLI flags      │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                   PRESET EXPANSION                          │
│  @all applied by default                                    │
│  !@preset syntax removes presets                            │
│  Presets expand to ro/rw/exclude path lists                 │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                   PATH RESOLUTION                           │
│  ~ → home directory                                         │
│  relative → effective cwd                                   │
│  globs → expand to existing paths only                      │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                 SPECIFICITY RESOLUTION                      │
│  When paths conflict, most specific wins:                   │
│  • longer path > shorter path                               │
│  • exact path > glob pattern                                │
│  • same specificity: exclude > ro > rw                      │
│  • same specificity + level: later config layer wins        │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                 COMMAND WRAPPER SETUP                       │
│  Find all binary paths for wrapped commands                 │
│  Generate blocker/wrapper scripts                           │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                  BWRAP ARG GENERATION                       │
│  Resolved paths → --ro-bind, --bind, --tmpfs                │
│  Wrappers → overlay mounts                                  │
│  + /dev, /proc, network config                              │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                        OUTPUT                               │
│              exec bwrap <args> -- <command>                 │
└─────────────────────────────────────────────────────────────┘
```

---

## Code Structure

All Go code lives in `cmd/agent-sandbox/`. Single `package main`, flat structure.

**One command per file:**
```
cmd_exec.go      # exec command
cmd_exec_test.go # its tests
cmd_check.go     # check command
cmd_check_test.go
```

**Supporting files** for complex logic (should be rare):
```
config.go        # config loading, merging
config_test.go
```

**Entry point:**
```
main.go          # just calls Run()
run.go           # Run() with full abstraction of env, io, args
```

The `Run()` function takes all dependencies as arguments (stdin, stdout, stderr, args, env, signal channel). This enables testing without mocking.

---

## Error Handling

**Handle every error.** No ignored errors, ever.

**Always wrap with context:**
```go
return fmt.Errorf("loading config from %s: %w", path, err)
```

**Actionable hints when possible:**
```go
return fmt.Errorf("bwrap not found in PATH: %w (install with: apt install bubblewrap)", err)
```

**Multiple errors in a codepath:** Use `errors.Join`, never ignore an error just because another one happened.

**Commands return errors, never print them.** Single top-level error handler in `run.go` or `command.go` formats and prints errors.

---

## Testing

**Test against the real CLI.** The primary test interface is `Run()` with real arguments.

**E2E tests with real bwrap.** Actually run commands in the sandbox and verify behavior - what's accessible, what's blocked, what's read-only.

**Simple test programs.** Use bash, `cat`, `touch`, `ls` - whatever is easiest to verify a behavior. Not Go binaries (they need compilation).

**--dry-run for complex argument verification.** When testing intricate bwrap argument generation, use `--dry-run` and assert on the output.

**Test helpers** in `testing_test.go` provide a `CLI` struct that wraps `Run()` for convenient test setup.

**Table tests.** Prefer table-driven tests for cases with multiple inputs/expectations.

**Test naming:** `Thing_Does_Y_When_Z`
```go
func Test_Config_Returns_Error_When_Both_Json_And_Jsonc_Exist(t *testing.T)
func Test_Sandbox_Blocks_Write_When_Path_Is_Readonly(t *testing.T)
func Test_Preset_Expands_Correctly_When_Negated(t *testing.T)
```

**DO NOT USE MOCKING EVER** Mocks are useless. Do not use them ever,
do not ever add code just for the purpose of easy unit tests. ever.

---

## Debug Output

`--debug` prints to stderr at key decision points:

- Config files found and loaded
- Config merge steps
- Path resolution results
- Final access levels per path
- Command wrapper setup
- Generated bwrap arguments

**Deliberate, not overwhelming.** Debug output should help diagnose "why did it do X" without flooding the terminal.

---

## Check Command Implementation

Sandbox detection is based on a deterministic, read-only mount:
1. The sandbox always ensures `/run/agent-sandbox` exists inside the sandbox.
2. The `agent-sandbox` binary is mounted read-only at `/run/agent-sandbox/agent-sandbox`.
3. The `check` command returns "inside sandbox" iff that path exists.

This is tamper-resistant: processes inside the sandbox cannot create or remove that mount.

---

## Command Wrapper Implementation

Wrappers are implemented by overlaying resolved executable target paths (found by
searching PATH and resolving symlinks). Depending on configuration, each target
is replaced with either:
- a small `--ro-bind-data` shell shim (fallback), or
- an ELF multicall launcher binary via `--ro-bind` (preferred when
  `CommandLauncherBinary` is set).

Wrapper and preset metadata is materialized under `/run/agent-sandbox` using
`--ro-bind-data` (script/marker content is provided via an inherited FD, typically
backed by memfd).

**Directory structure** inside the sandbox:
```
/run/agent-sandbox/
├── agent-sandbox                  # mounted agent-sandbox binary (for check + launcher)
├── bin/
│   └── <cmd>                      # real binary (only mounted when needed)
├── wrappers/
│   └── <cmd>                      # deny/custom wrapper scripts
└── presets/
    └── <cmd>                      # marker files for built-in presets

/usr/bin/git (and other resolved paths)  # replaced by per-path shim or launcher
```

Note: `/run/agent-sandbox` and its subdirectories (`bin`, `wrappers`, `presets`) are set to mode `0111` (search-only) so directory listing like `ls /run/agent-sandbox/bin` fails. This is a deterrence measure only; mounts can still be inspected via `/proc/self/mountinfo`.

**How it works:**
1. Discover all executable targets for each configured command by searching PATH
   (symlinks resolved and deduplicated by resolved target).
2. For `false` (deny): create a deny wrapper at `/run/agent-sandbox/wrappers/<cmd>`
   and replace every discovered target with the launcher/shim.
3. For script wrappers:
   - Mount the wrapper script at `/run/agent-sandbox/wrappers/<cmd>`.
   - If the wrapper needs the real binary, also mount the real binary at
     `/run/agent-sandbox/bin/<cmd>`.
   - Replace every discovered target with the launcher/shim.
4. For presets:
   - Create a marker at `/run/agent-sandbox/presets/<cmd>` and mount the real
     binary at `/run/agent-sandbox/bin/<cmd>`.
   - Replace every discovered target with the launcher/shim.

**The `multicall` dispatcher:**
- Hidden (not shown in `--help`) and only works inside the sandbox.
- Invoked either explicitly as `agent-sandbox multicall <cmd> ...` (shim mode) or
  implicitly when the launcher is executed via a wrapped path like `/usr/bin/git`
  (argv0-based dispatch).
- Dispatch order:
  1. If `/run/agent-sandbox/wrappers/<cmd>` exists, execute it and set:
     - `AGENT_SANDBOX_CMD=<cmd>`
     - `AGENT_SANDBOX_REAL=/run/agent-sandbox/bin/<cmd>` (if present, else empty)
  2. Otherwise, if `/run/agent-sandbox/presets/<cmd>` exists, run the built-in
     preset (currently only `git`).

**Custom wrapper scripts:**
- Receive the original tool arguments unchanged.
- Use `$AGENT_SANDBOX_REAL` to invoke the real tool.

Example (block npm publish):
```bash
#!/bin/sh
case "$1" in
  publish|unpublish|deprecate)
    echo "blocked" >&2
    exit 1
    ;;
esac
exec "$AGENT_SANDBOX_REAL" "$@"
```

**Presets:**
- Built-in presets are implemented in the `multicall` dispatcher (Go code) and enabled via marker files under `/run/agent-sandbox/presets`.
- Currently only `@git` is implemented.
- No user-defined argument matching DSL (too fragile across CLIs).

---

## Platform

**Linux only.** bwrap requires Linux namespaces. No OS detection or graceful fallback - just error if not Linux.
