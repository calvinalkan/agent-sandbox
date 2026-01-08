# agent-sandbox - Filesystem Sandbox for Coding Agents

## Specification

### Overview

`agent-sandbox` is a CLI tool that runs commands inside a filesystem sandbox using [bwrap](https://github.com/containers/bubblewrap) (bubblewrap). It protects system files and sensitive data from modification while allowing agents to work within designated areas.

**Platform:** Linux only (requires bwrap and Linux namespaces)

---

### Command Structure

```
agent-sandbox [global-flags] [exec-flags] <command> [command-args]
agent-sandbox check [-q]
```

Global flags must appear before exec flags. Exec flags must appear before the command. The command receives all remaining arguments, including any flags (e.g., `npm install --save-dev`).

Flag parsing stops at the first non-flag argument (the command).

---

### Global Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--cwd PATH` | `-C` | Run as if invoked from PATH |
| `--config PATH` | `-c` | Use config file at PATH instead of project config |
| `--help` | `-h` | Show help (context-sensitive) |
| `--version` | `-v` | Show version and exit |

The `-h` / `--help` flag may appear anywhere in the command line. When present, help is displayed and no action is taken.

---

### Exec Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--network` | on | Network access (use `--network=false` to disable) |
| `--docker` | off | Docker socket access |
| `--dry-run` | off | Print bwrap command without executing |
| `--debug` | off | Print sandbox startup details to stderr |
| `--ro PATH` | | Add read-only path (repeatable) |
| `--rw PATH` | | Add read-write path (repeatable) |
| `--exclude PATH` | | Add excluded/hidden path (repeatable) |
| `--cmd KEY=VALUE` | | Command wrapper override (repeatable) |

Boolean flags use `--flag` for true, `--flag=false` or `--flag=0` for false.

The `--cmd` flag can be repeated: `--cmd git=true --cmd rm=false` or comma-separated: `--cmd git=true,rm=false`.

The `--ro`, `--rw`, and `--exclude` flags can be repeated: `--ro ~/secrets --rw ./output --exclude .env`

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

### Check Command

```
agent-sandbox check [-q]
```

Checks if the current process is running inside an agent-sandbox.

| Flag | Description |
|------|-------------|
| `-q` | Quiet mode, no output |

**Output (without -q):**
- Inside sandbox: prints `inside sandbox`
- Outside sandbox: prints `outside sandbox`

**Exit code:**
- `0` = inside sandbox
- `1` = outside sandbox

The detection mechanism is tamperproof — it cannot be faked or disabled from inside the sandbox.

---

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Error (check stderr for details) |
| 130 | Interrupted (SIGINT/SIGTERM) |

For `check` command: 0 = inside sandbox, 1 = outside sandbox.

---

### Signal Handling

When `agent-sandbox` receives an interrupt signal (SIGINT/SIGTERM):

1. Signal is forwarded to the sandboxed process
2. Waits up to 10 seconds for graceful termination
3. A second interrupt forces immediate exit
4. Exit code is 130

---

### Configuration

**Loading order (each step overwrites previous):**

1. Built-in defaults
2. Global config (always loaded if exists, even with `-c`)
3. Project config OR `-c` path (one or the other, not both):
   - Default: loads `.agent-sandbox` from current directory (if exists)
   - With `-c PATH`: loads PATH instead, project config is ignored
4. CLI flags (highest priority)

**Config file locations:**

| Location | Path | Format |
|----------|------|--------|
| Global | `$XDG_CONFIG_HOME/agent-sandbox/config.json` or `.jsonc` (defaults to `~/.config/agent-sandbox/`) | JSON or JSONC |
| Project | `.agent-sandbox.json` or `.agent-sandbox.jsonc` in current directory | JSON or JSONC |

Each location supports either `.json` or `.jsonc` extension, but not both. If both exist, it's an error.

**Format:** `.json` files must be valid JSON. `.jsonc` files support `//` and `/* */` comments.

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
| `exclude` | Hidden (cannot see or access) |

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

**Default:** `@all` preset is always applied. User config adds paths or removes presets with `!@preset`.

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
- Globs expand at sandbox startup to existing paths only

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
3. **Same specificity, access level:** `exclude` > `ro` > `rw` (most restrictive wins)
4. **Same specificity, different config layer:** Later layer wins (CLI > project > global > preset)

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

**Default:** `@all` is always applied. Use `!@preset` to exclude.

**Built-in presets:**

| Preset | Description |
|--------|-------------|
| `@base` | Core sandbox: working directory writable, home protected (new files allowed, existing protected), temp writable, agent configs writable, secrets excluded (~/.ssh, ~/.gnupg, ~/.aws), sandbox config protected |
| `@caches` | Build tool caches writable (~/.cache, ~/.bun, ~/go, ~/.npm, ~/.cargo) |
| `@git` | Git hooks and config protected (.git/hooks, .git/config), with automatic worktree support |
| `@lint/ts` | TypeScript/JavaScript lint configs protected (biome, eslint, prettier, tsconfig) |
| `@lint/go` | Go lint configs protected (golangci) |
| `@lint/python` | Python lint configs protected (ruff, flake8, mypy, pylint, pyproject.toml) |
| `@lint/all` | All lint presets combined |
| `@all` | Everything: @base, @caches, @git, @lint/all |

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

**Wrapper mechanism:** All paths to the binary are discovered (e.g., `/usr/bin/git`, `/bin/git`, `/usr/local/bin/git`) and symlinks resolved. For blocking (`false`), a blocker is mounted over all locations. For wrapping (`@preset` or custom), the real binary is mounted to a randomized hidden path, and the wrapper is overlaid at all locations. This prevents trivial bypasses via alternate paths.

**Custom wrapper scripts:** When you need custom logic, write a wrapper script. The script receives the original arguments and should `exec` the real command (available at `$AGENT_SANDBOX_<CMD>`) when allowed.

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
exec "$AGENT_SANDBOX_NPM" "$@"
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

### Hardcoded Behavior

These cannot be changed by configuration:

| Item | Behavior |
|------|----------|
| `/dev` | Virtual device mount (required for programs) |
| `/proc` | Virtual proc mount (required for programs) |
| Sandbox detection | Tamperproof mechanism for `agent-sandbox check` (cannot be faked or disabled from inside) |
| Symlink resolution | Paths are resolved before mounting |
| Docker socket resolution | Symlinks auto-resolved when `--docker` enabled |
| Nested sandboxes | Running `agent-sandbox` inside a sandbox works without special handling |

---

### Environment Variables

All environment variables from the parent process are passed through to the sandboxed process unchanged.

---

### Security Guarantees

| Guarantee | Description |
|-----------|-------------|
| `ro` paths | Cannot be modified, deleted, or overwritten |
| `exclude` paths | Cannot be read, listed, or detected |
| Config files | `.agent-sandbox.json`/`.jsonc` and global config are read-only inside sandbox |
| Sandbox detection | `agent-sandbox check` result cannot be faked from inside |
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
| Path doesn't exist (non-glob) | Skip silently (may exist in other projects) |
| Glob matches nothing | Skip silently (pattern may be valid for other projects) |
| Running as root | Error, refuse to run |
| Home directory not found (with @base) | Error, exit |
| $PWD inside excluded path | Error, exit |
| Not on Linux | Error, exit |
| bwrap not installed | Error, exit |

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
if agent-sandbox check -q; then
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
