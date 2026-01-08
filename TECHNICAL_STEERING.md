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

Mount agent-sandbox itself into the sandbox:
1. Use `os.Executable()` to find our own binary
2. Mount it into the sandbox (e.g., `/usr/bin/agent-sandbox`) with `--ro-bind`

Tamperproof detection via marker file:
1. Mount a read-only marker file at a known path (e.g., `/.agent-sandbox`)
2. `check` command simply tests if the marker exists

Since agent-sandbox is a compiled Go binary, it can be mounted read-only (or execute-only if bwrap supports it). The marker file cannot be created or removed from inside the sandbox.

---

## Command Wrapper Implementation

**Directory structure** inside the sandbox:
```
/usr/bin/agent-sandbox              # agent-sandbox binary (for check, recursive calls)

/run/<random>/agent-sandbox/
├── binaries/
│   ├── wrap-binary                 # agent-sandbox binary
│   └── real/
│       ├── git                     # real git binary
│       └── npm                     # real npm binary
└── wrappers/
    ├── git                         # bash: exec .../wrap-binary git "$@"
    ├── npm                         # bash: exec .../wrap-binary npm "$@"
    └── rm                          # bash: echo "rm blocked"; exit 1

/usr/bin/git                        # bind mount of .../wrappers/git
/usr/bin/npm                        # bind mount of .../wrappers/npm
/usr/bin/rm                         # bind mount of .../wrappers/rm
```

**How it works:**
1. Find all paths to each wrapped binary (`/usr/bin/git`, `/bin/git`, symlinks resolved)
2. Mount real binary to `/run/<random>/agent-sandbox/binaries/real/<name>`
3. Generate wrapper scripts in `/run/<random>/agent-sandbox/wrappers/`
4. Bind mount wrapper scripts over ALL original binary locations

**The `wrap-binary` command:**
- Not shown in `--help`
- Errors if not inside sandbox (marker file missing)
- Uses `os.Executable()` to find its own path, then locates real binary at `../real/<name>`
- No config file needed at runtime - just path convention

**One hidden command** (not in `--help`, only works inside sandbox):

| Command | Purpose |
|---------|---------|
| `wrap-binary <cmd> [args...]` | Presets: apply rules, exec real binary. Custom: set env var, exec user's script |

**Wrapper scripts:**
```bash
# deny-binary - single static script, uses $0 for command name:
#!/bin/bash
echo "command '$(basename $0)' is blocked in this sandbox" >&2
exit 1

# For preset (@git) or custom script:
exec agent-sandbox wrap-binary git "$@"
```

For blocked commands, mount `deny-binary` directly at all binary paths (e.g., `/usr/bin/rm`). One script, reused everywhere.

**Script generation:**
1. Create temp dir on host (`/tmp/<random>/`)
2. Write `deny-binary` script (static)
3. Write `wrap-<cmd>` scripts (one per preset/custom command)
4. Start bwrap with mounts
5. Wait for bwrap to exit
6. Delete temp dir

**Custom wrapper scripts:**
- `wrap-binary` sets `AGENT_SANDBOX_<NAME>` env var pointing to real binary
- Example: `AGENT_SANDBOX_NPM=/run/<random>/.../real/npm`
- Then execs user's script via Go's exec (with env var set)
- User's script uses the env var to call real binary:
```bash
#!/bin/bash
case "$1" in
  publish|unpublish) echo "blocked" >&2; exit 1 ;;
esac
exec "$AGENT_SANDBOX_NPM" "$@"
```

**Presets:**
- Built-in presets are Go implementations that understand tool-specific argument grammar
- For now, only `@git` is implemented - more presets can be added later
- No user-defined argument matching - too error-prone across different CLI conventions

---

## Platform

**Linux only.** bwrap requires Linux namespaces. No OS detection or graceful fallback - just error if not Linux.
