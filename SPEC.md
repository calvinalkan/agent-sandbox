# agent-sandbox - Filesystem Sandbox for Coding Agents

## Specification

### Overview

`agent-sandbox` is a CLI tool that runs commands inside a filesystem sandbox using [bwrap](https://github.com/containers/bubblewrap) (bubblewrap). It protects system files and sensitive data from modification while allowing agents to work within designated areas.

**Platform:** Linux only (requires bwrap and Linux namespaces)

---

### Command Structure

```
agent-sandbox [flags] <command> [command-args]
```

Flags must appear before the command. The command receives all remaining arguments, including any flags (e.g., `npm install --save-dev`).

Flag parsing stops at the first non-flag argument (the command).

---

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--help` | `-h` | | Show help |
| `--version` | `-v` | | Show version and exit |
| `--check` | | | Check if running inside sandbox and exit |
| `--cwd PATH` | `-C` | | Run as if invoked from PATH |
| `--config PATH` | `-c` | | Use config file at PATH instead of project config |
| `--network` | | on | Network access (use `--network=false` to disable) |
| `--docker` | | off | Docker socket access |
| `--dry-run` | | off | Print bwrap command without executing |
| `--debug` | | off | Print sandbox startup details to stderr |
| `--ro PATH` | | | Add read-only path (repeatable) |
| `--rw PATH` | | | Add read-write path (repeatable) |
| `--exclude PATH` | | | Add excluded/hidden path (repeatable) |
| `--cmd KEY=VALUE` | | | Command wrapper override (repeatable) |

Boolean flags use `--flag` for true, `--flag=false` or `--flag=0` for false.

The `--cmd` flag can be repeated: `--cmd git=true --cmd rm=false` or comma-separated: `--cmd git=true,rm=false`.

The `--ro`, `--rw`, and `--exclude` flags can be repeated: `--ro ~/secrets --rw ./output --exclude .env`

The `-h` / `--help` flag is only handled by agent-sandbox before the sandboxed command name (while parsing flags). If `-h`/`--help` appears after the command name, it is passed through to the sandboxed command unchanged (it may be a valid flag for that tool).

---

### --check Flag

The `--check` flag checks if the current process is running inside an agent-sandbox.

**Output:**
- Inside sandbox: prints `inside sandbox`
- Outside sandbox: prints `outside sandbox`

**Exit code:**
- `0` = inside sandbox
- `1` = outside sandbox

The detection mechanism is tamperproof — it cannot be faked or disabled from inside the sandbox.

---

### Docker Socket Access

`agent-sandbox` does not "unshare Docker" as a namespace. The `docker` setting controls whether the sandboxed process can reach the Docker daemon endpoint.

`agent-sandbox` builds the sandbox filesystem by bind-mounting the host root as read-only (`--ro-bind / /`) and then overlaying specific mounts (including a fresh `/run` tmpfs via `--tmpfs /run`). This affects how Docker is exposed:

- **Docker disabled (`--docker=false`, default):**
  - The sandbox makes the Docker daemon unreachable.
  - `agent-sandbox` masks the resolved Docker socket path by bind-mounting `/dev/null` over it (deterministic behavior, even if `/run` is a fresh tmpfs).

- **Docker enabled (`--docker=true`):**
  - The sandbox bind-mounts the Docker Unix socket into the sandbox so Docker clients can connect.
  - Symlinks are resolved so paths like `/var/run/docker.sock` correctly map to the real socket location (often `/run/docker.sock`).

**Socket path selection:**
- If `DOCKER_HOST` is set to a Unix socket URL (e.g. `unix:///var/run/docker.sock`), that path is used.
- Otherwise the default socket path is `/var/run/docker.sock`.

Note: if Docker is configured to use a TCP endpoint (`DOCKER_HOST=tcp://...`), Docker access also depends on network namespace settings (`--network`).

---

### Debug Output

When `--debug` is enabled, prints to stderr during sandbox startup:

- Config files found and loaded (with paths)
- Config merge steps
- Path and glob resolution results
- Final resolved paths with access levels
- Command wrapper setup
- Generated bwrap arguments

This does not affect output from the sandboxed process itself.

---

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Sandbox setup error (check stderr for details) |
| 130 | Interrupted (SIGINT/SIGTERM) |
| other | Propagated exit code from the sandboxed command |

For `--check` flag: 0 = inside sandbox, 1 = outside sandbox.

---

### Signal Handling

When `agent-sandbox` receives an interrupt signal (SIGINT/SIGTERM):

1. Cancels sandbox execution and sends SIGTERM to the `bwrap` process (the sandboxed command may or may not receive the signal depending on process-group delivery)
2. Waits up to 10 seconds for graceful termination
3. A second interrupt or timeout sends SIGKILL to the `bwrap` process and forces exit
4. Exit code is 130

---

### Configuration

**Loading order (each step overwrites previous):**

1. Built-in defaults
2. Global config (always loaded if exists, even with `-c`)
3. Project config OR `-c` path (one or the other, not both):
   - Default: loads `.agent-sandbox.json` or `.agent-sandbox.jsonc` from current directory (if exists)
   - With `-c PATH`: loads PATH instead, project config is ignored
4. CLI flags (highest priority)

**Config file locations:**

| Location | Path | Format |
|----------|------|--------|
| Global | `$XDG_CONFIG_HOME/agent-sandbox/config.json` or `.jsonc` (defaults to `~/.config/agent-sandbox/`) | JSON or JSONC |
| Project | `.agent-sandbox.json` or `.agent-sandbox.jsonc` in current directory | JSON or JSONC |

Each location supports either `.json` or `.jsonc` extension, but not both. If both exist, it's an error.

**Format:** Both `.json` and `.jsonc` files accept JSONC (`//` and `/* */` comments, trailing commas).

**Example:**
```jsonc
{
  "filesystem": {
    // Modify default presets (@all is default)
    "presets": ["!@lint/python"],
    // File access
    "ro": ["~/code/other-project"],
    "rw": [".generated/"],
    "exclude": ["~/.aws"]
  },
  
  "commands": {
    "git": "@git",
    "rm": false
  },
  
  "network": true,
  "docker": false
}
```

---

### File Access Model

Three access levels for paths:

| Level | Meaning |
|-------|---------|
| `ro` | Read-only (can read, cannot write) |
| `rw` | Read-write (can read and write) |
| `exclude` | Hidden contents (masked by empty file/dir; path may remain detectable) |

**Config structure:**
```jsonc
{
  "filesystem": {
    "presets": ["!@lint/python"],  // modify default @all
    "ro": ["path1", "path2"],
    "rw": ["path3"],
    "exclude": ["path4"]
  }
}
```

**Default:** `@all` is applied when presets are not specified. Use `!@all` to disable defaults (then add desired presets); use `!@preset` to remove individual defaults.

**Missing path behavior:**
- `filesystem.ro`/`rw`/`exclude` and `--ro`/`--rw`/`--exclude` ignore missing paths and globs that match nothing (best-effort).
- Invalid glob patterns are errors.
- The Go API provides strict policy mounts (`RO`, `RW`, `Exclude`) plus `ROTry`, `RWTry`, and `ExcludeTry`.
- `ExcludeFile` and `ExcludeDir` force a specific file/dir mask even when missing (no glob patterns).

---

### Path Patterns

| Pattern | Meaning | Example |
|---------|---------|---------|
| `path` | Exact path (and everything under it) | `~/.ssh`, `src/auth` |
| `*/` | Single directory level wildcard | `packages/*/biome.json` |

**Path resolution:**
- `~` at start expands to home directory
- Absolute paths (`/`, `~`) resolve as-is
- Relative paths resolve relative to effective pwd (respects `--cwd`)
- Globs expand at sandbox startup; patterns that match nothing are ignored for `filesystem.ro`/`rw`/`exclude` and `--ro`/`--rw`/`--exclude`

**Glob support:**
- `*` matches any characters within a single path segment (native Go `filepath.Glob`)
- Multiple wildcards allowed: `src/*/*/config.json`
- No `**` recursive glob support

**Global config note:** Relative paths in global config resolve to the effective pwd, not the config file location. Use absolute paths (`~` or `/`) to reference paths relative to the global config itself.

**bwrap inheritance:** When a path is mounted, everything under it inherits the same access level. `~/.config` as read-only means all of `~/.config/...` is read-only. More specific paths override less specific ones.

**Environment variables:** Not expanded. `$HOME`, `$PWD` etc. are treated as literal strings.

---

### Specificity Rules

When multiple rules match a path, the most specific wins:

1. **Exact path beats glob:** `~/.config/biome.json` beats `~/.config/*`
2. **Longer path beats shorter:** `~/.config/nvim` beats `~/.config`
3. **Same specificity, different config layer:** Later layer wins (CLI > project > global > preset)
4. **Same specificity, within the same layer:** `exclude` > `ro` > `rw` (most restrictive wins)

---

### Filesystem Presets

Presets are built-in named configurations for filesystem access. Users cannot define custom presets.

**Referencing presets:**
```jsonc
{
  "filesystem": {
    "presets": ["!@lint/python"]  // @all is default, just exclude what you don't want
  }
}
```

**Default:** `@all` is applied when presets are not specified. Use `!@all` to disable defaults; use `!@preset` to exclude individual presets.

**Built-in presets:**

| Preset | Description |
|--------|-------------|
| `@base` | Core sandbox: working directory writable, home directory read-only, temp writable, secrets excluded (~/.ssh, ~/.gnupg, ~/.aws), sandbox config protected |
| `@caches` | Build tool caches writable (~/.cache, ~/.bun, ~/go, ~/.npm, ~/.cargo) |
| `@agents` | AI coding agent configs writable (~/.codex, ~/.claude, ~/.claude.json, ~/.pi) |
| `@git` | Git hooks and config protected (.git/hooks, .git/config), with automatic worktree support |
| `@git-strict` | Git metadata protected more aggressively: tags and non-current branch refs are read-only (current branch remains writable); supports worktrees |
| `@lint/ts` | TypeScript/JavaScript lint configs protected (biome, eslint, prettier, tsconfig) |
| `@lint/go` | Go lint configs protected (golangci) |
| `@lint/python` | Python lint configs protected (ruff, flake8, mypy, pylint, pyproject.toml) |
| `@lint/all` | All lint presets combined |
| `@all` | Everything: @base, @caches, @agents, @git, @lint/all |

---

### Command Wrappers

Command wrappers intercept specific binaries to enforce safety rules.

**Config:**
```jsonc
{
  "commands": {
    "git": "@git",                  // use built-in wrapper
    "rm": false,                    // block entirely
    "npm": true,                    // raw command (remove wrapper)
    "curl": "~/bin/curl-wrapper"    // custom wrapper script
  }
}
```

**Values:**

| Value | Meaning |
|-------|---------|
| `"@preset"` | Use built-in smart wrapper (only available for specific tools) |
| `false` | Block command entirely (exits with error) |
| `true` | Raw command (no wrapper, removes default if any) |
| `"path"` | Custom wrapper script (user provides the logic) |

**Why no custom rules?** Every CLI has different conventions for flags, subcommands, and arguments. Reliably parsing `git -C /path push --force` vs `npm run build` vs `docker run ubuntu rm -rf /` requires tool-specific knowledge. Rather than provide a fragile pattern-matching DSL, we offer:
- Built-in presets with proper parsing for supported tools
- Custom wrapper scripts for everything else

**CLI override:**
```bash
agent-sandbox --cmd git=true --cmd rm=false npm install
```

**Default:**
```jsonc
{
  "commands": {
    "git": "@git"
  }
}
```

**Built-in command presets:**

Currently only `@git` is available. More may be added in the future.

#### @git

Smart wrapper that understands git's argument structure and blocks dangerous operations:

| Blocked | Reason | Alternative |
|---------|--------|-------------|
| `git checkout` | Can discard uncommitted changes | `git switch` for branches, `git restore` is also blocked |
| `git restore` | Discards uncommitted changes | Commit or stash first |
| `git reset --hard` | Discards commits and changes | `git reset --soft` or `git revert` |
| `git clean -f` | Deletes untracked files | Manual review |
| `git commit --no-verify` | Bypasses safety hooks | Fix the hook issues |
| `git stash drop` | Permanently deletes stash | Keep stashes or use `git stash clear` with caution |
| `git stash clear` | Deletes all stashes | Export important stashes first |
| `git stash pop` | Can cause merge conflicts that lose stash | `git stash apply` (keeps stash intact) |
| `git branch -D` | Force deletes unmerged branch | `git branch -d` (safe delete) |
| `git push --force` | Rewrites remote history | `git push --force-with-lease` |

The wrapper properly parses git's global flags (e.g., `-C`, `--no-pager`) to correctly identify subcommands regardless of flag position.

When blocked, prints a guidance message to stderr and exits with error.

**Temp directory exception:** If the current working directory is inside the system temp directory (for example `/tmp`), the git wrapper does not block operations. This is intended for tests and throwaway repos.

**Wrapper mechanism:** All paths to the binary are discovered (e.g., `/usr/bin/git`, `/bin/git`, `/usr/local/bin/git`) and symlinks resolved. For blocking (`false`), a blocker is mounted over all locations. For presets and custom wrappers, the real binary is mounted at `/run/agent-sandbox/bin/<cmd>` and wrapper logic is driven by sandbox-internal files (`/run/agent-sandbox/policies/<cmd>` for scripts, `/run/agent-sandbox/presets/<cmd>` for presets). Discovered target paths are then replaced with a launcher that dispatches to the right policy/preset.

**Binary/command bypass (obfuscation only):**
- A process inside the sandbox can often discover wrapper mounts by inspecting `/proc/self/mountinfo`.
- The "real" tool binary is mounted at `/run/agent-sandbox/bin/<cmd>` (not on PATH). If a process knows that path, it can execute the real tool directly and bypass wrapper/preset logic.
- To reduce trivial discovery (but not eliminate it), agent-sandbox:
  - uses an ELF launcher at wrapped target paths (so `cat $(command -v git)` doesn't trivially reveal a shell shim), and
  - makes `/run/agent-sandbox/{bin,policies,presets}` search-only (mode `0111`) so directory listing like `ls /run/agent-sandbox/bin` fails.

These measures are deterrence only. Filesystem rules (`ro`, `rw`, `exclude`) are the enforcement boundary.

**Why a launcher binary instead of mounting scripts directly?**

If wrapper scripts were mounted directly over command paths (e.g., a bash script at `/usr/bin/git`), a user could simply `cat /usr/bin/git` and read the script source, which would reveal the real binary location (`/run/agent-sandbox/bin/git`). They could then call the real binary directly, bypassing the wrapper entirely.

By using a compiled launcher binary:
1. `cat /usr/bin/git` shows binary data, not readable script with paths
2. The launcher contains dispatch logic in compiled form - not human-readable
3. Combined with `0111` directory permissions, users cannot easily discover what commands are wrapped or where real binaries are mounted
4. Additionally, using a compiled launcher allows wrapper logic (like the `@git` preset) to be implemented in Go rather than bash, which is faster and allows for more sophisticated argument parsing

A determined user can still discover wrapped binaries via `/proc/self/mountinfo` or by guessing paths. The launcher approach raises the barrier but does not eliminate bypass entirely. The ultimate enforcement is always the filesystem permissions - even if a wrapper is bypassed, `ro`/`rw`/`exclude` rules cannot be circumvented.

**Custom wrapper scripts:** When you need custom logic, write a wrapper script. The script receives the original tool arguments unchanged. The real binary is available via `$AGENT_SANDBOX_REAL` and the wrapped command name via `$AGENT_SANDBOX_CMD`.

Example - blocking npm publish commands:

`~/.config/agent-sandbox/npm-wrapper.sh`:
```bash
#!/bin/bash
case "$1" in
  publish|unpublish|deprecate)
    echo "npm $1 blocked by sandbox" >&2
    exit 1
    ;;
esac

exec "$AGENT_SANDBOX_REAL" "$@"
```

`~/.config/agent-sandbox/config.json`:
```jsonc
{
  "commands": {
    "npm": "~/.config/agent-sandbox/npm-wrapper.sh"
  }
}
```

**Note:** Command wrappers are a deterrent, not absolute security. Filesystem rules (`ro`, `rw`, `exclude`) are the ultimate enforcement — even if a wrapper is bypassed, protected paths remain protected.

---

### Config Merging

**Layer order (later overrides earlier):**
1. Built-in defaults
2. Filesystem presets (expanded)
3. Global config (always, if exists)
4. Project config OR `--config` path (if exists)
5. CLI flags

**Filesystem arrays (`presets`, `ro`, `rw`, `exclude`):** Merged (concatenated), then specificity rules applied.

**Object fields (`commands`):** Merged, later values override earlier for same key.

**Boolean fields (`network`, `docker`):** Later value wins.

---

### Nested Sandboxes

Running `agent-sandbox` inside an existing sandbox (nested sandbox) works, but with specific constraints:

**Filesystem permissions can only be made MORE restrictive:**
- Outer: `rw` → Inner: `ro` ✅ (locks down further)
- Outer: `ro` → Inner: `rw` ❌ (inner cannot escalate, remains `ro`)
- Outer: `exclude` → Inner: anything ❌ (path is already hidden)

This is enforced by the kernel — the inner sandbox cannot escape the outer's restrictions.

**Network can only be disabled, not re-enabled:**
- Outer: enabled → Inner: disabled ✅
- Outer: disabled → Inner: enabled ❌ (inner cannot escape network namespace)

**Command wrappers can only be made MORE restrictive:**

- Outer: raw → Inner: `false` / `@git` / script ✅ (locks down further)
- Outer: wrapped/preset/blocked → Inner: raw ❌ (inner cannot override outer wrappers)
- `--cmd` CLI flag: allowed inside a sandbox, but only applies to commands that are not already wrapped by the outer sandbox

Note: the agent-sandbox binary may be renamed on the host. Inside the sandbox, multicall dispatch is triggered by policy/preset marker files rather than the executable name, so renamed binaries still behave like the normal CLI.

When running inside a sandbox, command rules from config files and `--cmd` are applied only for commands that are not already wrapped by the outer sandbox. This lets nested sandboxes lock down additional tools without weakening the outer sandbox policy.

Implementation detail: inner sandboxes mount the outer runtime at `/run/agent-sandbox/outer`, and the multicall launcher searches:
1. `/run/agent-sandbox/...` (inner)
2. `/run/agent-sandbox/outer/...` (outer)

**Workaround for dynamic behavior:**

If you need inner sandbox behavior to vary based on outer sandbox state, use a custom wrapper script that checks filesystem state:

```bash
#!/bin/bash
# Check if a marker file is read-only (outer sandbox locked it down)
if ! touch /tmp/.sandbox-marker 2>/dev/null; then
  echo "Running in restricted mode" >&2
  # Apply stricter rules
fi

exec "$AGENT_SANDBOX_REAL" "$@"
```

---

### Hardcoded Behavior

These cannot be changed by configuration:

| Item | Behavior |
|------|----------|
| `/dev` | Virtual device mount (required for programs) |
| `/proc` | Virtual proc mount (required for programs) |
| Sandbox detection | Tamperproof mechanism for `--check` (cannot be faked or disabled from inside) |
| Symlink resolution | Paths are resolved before mounting |
| Docker socket resolution | Symlinks auto-resolved when `--docker` enabled |
| Nested sandboxes | Running `agent-sandbox` inside a sandbox works (see Nested Sandboxes section) |

---

### Environment Variables

All environment variables from the parent process are passed through to the sandboxed process unchanged.

---

### Security Guarantees

| Guarantee | Description |
|-----------|-------------|
| `ro` paths | Cannot be modified, deleted, or overwritten |
| `exclude` paths | Contents cannot be read (paths may be detectable as empty placeholders) |
| Config files | `.agent-sandbox.json`/`.jsonc` and global config are read-only when present; missing config files can be created and affect future runs |
| Sandbox detection | `--check` result cannot be faked from inside |
| Blocked commands | Cannot execute when wrapper set to `false` or operation forbidden |
| Network (disabled) | No network access when `--network=false` |
| Root filesystem | Read-only by default |

---

### Error Conditions

| Condition | Behavior |
|-----------|----------|
| Invalid JSON/JSONC config | Error, exit |
| Both .json and .jsonc exist at same location | Error, exit |
| Unknown preset referenced | Error, exit |
| Invalid glob pattern | Error, exit |
| Path doesn't exist (`filesystem.ro/rw/exclude`, `--ro/--rw/--exclude`) | Skip silently |
| Glob matches nothing (`filesystem.ro/rw/exclude`, `--ro/--rw/--exclude`) | Skip silently |
| Running as root | Error, refuse to run |
| Home directory not found (with @base) | Error, exit |
| $PWD inside excluded path | Error, exit |
| Not on Linux | Error, exit |
| bwrap not installed | Error, exit |
| `--cmd` flag used inside sandbox | Allowed, but only applies to commands not already wrapped by outer sandbox |

---

### Examples

**Run command in sandbox with defaults:**
```bash
agent-sandbox npm install
```

**Disable network:**
```bash
agent-sandbox --network=false npm install
```

**Enable docker:**
```bash
agent-sandbox --docker npm install
```

**Override command wrapper:**
```bash
agent-sandbox --cmd git=true git checkout main
```

**Dry run (show bwrap command):**
```bash
agent-sandbox --dry-run npm install
```

**Check if sandboxed:**
```bash
if agent-sandbox --check; then
  echo "inside sandbox"
fi
```

**Run from different directory:**
```bash
agent-sandbox -C ~/other-project npm install
```

**Add read-only protection:**
```bash
agent-sandbox --ro src/auth/ node build.js
```

**Add writable path:**
```bash
agent-sandbox --rw ./dist npm run build
```

**Hide sensitive files:**
```bash
agent-sandbox --exclude .env --exclude secrets/ npm test
```

**Project config (`.agent-sandbox.json` or `.agent-sandbox.jsonc`):**
```jsonc
{
  "filesystem": {
    // Don't protect Python lint configs in this project
    "presets": ["!@lint/python"],
    // Extra protection
    "ro": ["src/auth/", "config/*/secrets.json"],
    // Allow writing to generated files
    "rw": [".generated/"]
  },
  
  "commands": {
    "rm": false
  }
}
```

**Global config (`~/.config/agent-sandbox/config.json` or `config.jsonc`):**
```jsonc
{
  "docker": true,
  
  "filesystem": {
    // Extra paths I want writable on all machines
    "rw": [
      "~/code/experiments",
      "~/code/worktrees"
    ],
    // Exclude additional secrets
    "exclude": [
      "~/.config/gh",
      "~/.kube"
    ]
  }
}
```
