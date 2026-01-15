# Agent-Sandbox Spec Review (Deduplicated)

This document consolidates discrepancies between SPEC.md and the behavior of the `agent-sandbox` binary. It merges two independent spec review reports and removes duplicate entries while retaining all unique details and evidence.

---

## Consolidated mismatches

### `-h/--help` placement is limited to before `<command>`

**Spec says:**
`-h/--help` may appear anywhere in the command line and when present no action is taken.

**Actual behavior:**
`-h/--help` is only treated as help when it appears before the first non-flag argument (`<command>`). When placed after `<command>`, it is passed through to the sandboxed command.

**Proposed spec update:**
`-h/--help` may appear anywhere **before** `<command>` (interleaved with global/exec flags), but once the first non-flag argument is seen, the remaining args are opaque and passed through unchanged.

**Evidence:**
```bash
agent-sandbox echo hi -h   # prints: "hi -h" (runs command)
agent-sandbox -h echo hi   # prints help (does not run command)
```

---

### Undocumented preset `@git-strict` (filesystem + command presets)

**Spec says:**
Only `@git` is available for built-in command presets, and the filesystem preset table does not mention `@git-strict`.

**Actual behavior:**
The implementation recognizes a filesystem preset named `@git-strict`, and also a command preset named `@git-strict`.

**Expected:**
Either document `@git-strict` in the spec or remove it from the implementation.

**Evidence:**
```bash
agent-sandbox -C t/project-unknown-preset echo hi
# -> error includes: available: ... @git, @git-strict, ...

agent-sandbox --cmd git-strict=@git-strict true
# -> succeeds

agent-sandbox --cmd git=@git-strict true
# -> errors: @git-strict preset can only be used for 'git-strict' command
```

---

### `exclude` paths are detectable and may appear as empty overlays

**Spec says:**
Excluded paths are hidden and cannot be read, listed, or detected.

**Actual behavior:**
Excluded paths remain visible in listings and can be detected via `stat`/`test -e`. Excluded directories appear as empty placeholders (often writable tmpfs overlays), and excluded files show up with no permissions and size 0.

**Evidence (file exclusion):**
```bash
agent-sandbox --exclude t/fs-basic/secret.txt bash -lc 'ls -la t/fs-basic; test -e t/fs-basic/secret.txt; echo $?'
# -> `ls` shows `secret.txt` (mode `----------`, size 0)
# -> `test -e` returns 0 (detectable)
```

**Evidence (directory exclusion):**
```bash
agent-sandbox --exclude t/fs-basic/secretdir bash -lc 'ls -la t/fs-basic; ls -la t/fs-basic/secretdir'
# -> `ls` shows `secretdir` exists
# -> listing inside shows only `.` and `..`
```

**Evidence (excluded path detection via stat):**
```bash
agent-sandbox sh -c 'stat ~/.ssh'
agent-sandbox --exclude secret.txt cat secret.txt
agent-sandbox --exclude secret.txt stat secret.txt
```

**Evidence (empty writable overlay inside sandbox):**
```bash
agent-sandbox --exclude t/fs-basic/secretdir bash -lc 'echo INSIDE > t/fs-basic/secretdir/newinside.txt; ls -la t/fs-basic/secretdir'
# -> shows newinside.txt inside sandbox

ls -la t/fs-basic/secretdir
# -> outside still shows only the original inside.txt; newinside.txt not present
```

**Note:**
Contents are hidden, but path existence is still detectable via `stat`/`ls -la`. This may be intentional (avoiding "No such file" errors for programs), but differs from the spec wording.

---

### `@base` home behavior is read-only and writable subpaths must exist

**Spec says:**
`@base` allows new files in `$HOME` while protecting existing files.

**Actual behavior:**
`$HOME` is mounted read-only, so new files cannot be created in `~`. Only specific subpaths (e.g., `~/.cache`, `~/.codex`) may be mounted writable by other presets, and those subpaths must already exist outside the sandbox.

**Expected:**
New files should be creatable in the home directory while existing files remain protected.

**Evidence (home read-only):**
```bash
agent-sandbox sh -c 'echo "test" > ~/new-file.txt'
# -> Read-only file system

agent-sandbox sh -c 'touch ~/new-file.txt'
# -> Read-only file system
```

**Evidence (missing preset subpaths cannot be created):**
```bash
HOME="$PWD/t/fakehome4" XDG_CONFIG_HOME="$PWD/t/xdg-empty" agent-sandbox bash -lc 'mkdir -p "$HOME/.cache"'
# -> fails with "Read-only file system"
```

**Note:**
This suggests the preset uses bind-try semantics for writable subpaths.

---

### SIGINT/SIGTERM are not forwarded when only `agent-sandbox` is signaled

**Spec says:**
SIGINT/SIGTERM are forwarded to the sandboxed process.

**Actual behavior:**
If only the `agent-sandbox` PID is signaled (not the whole process group), the sandboxed process does not receive the signal.

**Note:**
If you signal the whole process group (as Ctrl+C does), the trap runs; current behavior relies on process-group delivery rather than explicit forwarding.

**Evidence:**
```bash
rm -f t/trap_marker2.txt
setsid agent-sandbox bash -lc 'trap "echo trapped > t/trap_marker2.txt; sleep 5; exit 0" INT TERM; while true; do sleep 1; done' &
pid=$!
sleep 0.5
kill -INT "$pid"
wait "$pid"
# Result: t/trap_marker2.txt is NOT created
```

---

### `.agent-sandbox` project config file is not recognized

**Spec says:**
The default project config file is `.agent-sandbox`.

**Actual behavior:**
Only `.agent-sandbox.json` and `.agent-sandbox.jsonc` are recognized.

**Evidence:**
```bash
# t/project-dotfile/.agent-sandbox contains {"commands":{"rm":false}}
agent-sandbox -C t/project-dotfile rm --version
# -> rm runs normally (config not loaded)
```

---

### Specificity rule: later layers can override `exclude` and `ro`

**Spec says:**
For equal specificity, the most restrictive access wins (`exclude` > `ro` > `rw`).

**Actual behavior:**
Later layers (project config or CLI) can override earlier `exclude` or `ro` rules and make the same path writable.

**Evidence (exclude overridden by project config):**
```bash
XDG_CONFIG_HOME="$PWD/t/xdg-layer" agent-sandbox -C t/layer-test bash -lc 'cat secret.txt; echo X >> secret.txt'
# -> succeeds (project overrides global exclude)
```

**Evidence (ro overridden by project config):**
```bash
XDG_CONFIG_HOME="$PWD/t/xdg-layer-ro" agent-sandbox -C t/layer-test bash -lc 'echo X >> secret.txt'
# -> succeeds (project overrides global ro)
```

**Evidence (exclude overridden by CLI --rw):**
```bash
XDG_CONFIG_HOME="$PWD/t/xdg-layer-cli" agent-sandbox -C t/layer-cli --rw secret.txt bash -lc 'cat secret.txt; echo X >> secret.txt'
# -> succeeds
```

---

### Exit codes: sandboxed command exit code is propagated

**Spec says:**
Only `0`, `1`, and `130` are used for exit codes.

**Actual behavior:**
The sandbox propagates the sandboxed command exit code.

**Evidence:**
```bash
agent-sandbox bash -lc 'exit 42'
# -> agent-sandbox exits 42
```

---

### `.json` configs accept JSONC comments and trailing commas

**Spec says:**
Only `.jsonc` supports comments, and `.json` must be valid JSON.

**Actual behavior:**
`.agent-sandbox.json` accepts `//` comments and trailing commas.

**Evidence (comments):**
```bash
# t/project-json-comments/.agent-sandbox.json contains a `//` comment
agent-sandbox -C t/project-json-comments rm --version
# -> rm is blocked (config parsed and applied)
```

**Evidence (trailing commas):**
```bash
# t/project-trailing-comma/.agent-sandbox.json contains trailing commas
agent-sandbox -C t/project-trailing-comma rm --version
# -> rm is blocked (config parsed and applied)
```

---

### Config files are not always read-only inside the sandbox

**Spec says:**
Project and global config files are read-only inside the sandbox.

**Actual behavior:**
Config files can be created when missing, and can be made writable through CLI or config rules.

**Evidence (create when missing):**
```bash
agent-sandbox sh -c 'echo "{}" > .agent-sandbox.json'
# -> File is created (Exit: 0)

agent-sandbox sh -c 'echo "x" >> .agent-sandbox.json'
# -> Read-only file system (subsequent run)
```

**Evidence (CLI --rw grants write):**
```bash
agent-sandbox -C t/project-config-protect --rw .agent-sandbox.json bash -lc 'echo "//hack" >> .agent-sandbox.json'
# -> succeeds and persists outside the sandbox
```

**Evidence (project config grants itself write):**
```bash
agent-sandbox -C t/self-modify bash -lc 'echo "//self" >> .agent-sandbox.json'
# -> succeeds and persists outside
```

**Evidence (global config grants itself write):**
```bash
XDG_CONFIG_HOME="$PWD/t/xdg-self-mod" agent-sandbox bash -lc 'echo "//global-self" >> "$XDG_CONFIG_HOME/agent-sandbox/config.json"'
# -> succeeds and persists outside
```

**Impact:**
A sandboxed process can create a config file when it is missing, which can affect subsequent runs even though it does not affect the current session.

**Possible fix:**
Pre-create `.agent-sandbox*` paths as read-only even when missing, or refuse writes to those paths in the working directory.

---

### Environment variables are not passed through unchanged (`PWD` rewritten)

**Spec says:**
All environment variables are passed through unchanged.

**Actual behavior:**
`PWD` is rewritten to the effective working directory.

**Evidence:**
```bash
echo "$PWD"  # /home/calvin/code/experiments/2026-01-09-spec-review
agent-sandbox -C t/layer-test env | rg '^PWD='
# -> PWD=/home/calvin/code/experiments/2026-01-09-spec-review/t/layer-test

PWD=/fakepwd agent-sandbox env | rg '^PWD='
# -> PWD=/home/calvin/code/experiments/2026-01-09-spec-review
```

---

### Preset `ro` can be overridden by CLI `--rw`

**Spec says:**
For the same path, `ro` should beat `rw`.

**Actual behavior:**
A CLI `--rw` rule overrides preset `ro` for the same path.

**Evidence:**
```bash
abs_home="$PWD/t/fakehome3"
HOME="$abs_home" XDG_CONFIG_HOME="$PWD/t/xdg-empty" agent-sandbox --rw "$abs_home" bash -lc 'touch "$HOME/new.txt"'
# -> succeeds
```

---

### `--rw /run` breaks sandbox startup

**Spec says:**
No mention of `/run` as an internal reserved path.

**Actual behavior:**
Granting `--rw /run` breaks startup because agent-sandbox uses `/run/<id>/...` internally.

**Evidence:**
```bash
agent-sandbox --rw /run echo hi
# -> bwrap: Can't mkdir parents for /run/<id>/agent-sandbox/binaries/wrap-binary: Permission denied
```

---

### Default `@all` preset can be disabled via `!@all`

**Spec says:**
`@all` is always applied.

**Actual behavior:**
A config can specify `filesystem.presets: ["!@all"]` to remove all default preset mounts. This makes the working directory read-only (no `@base`), leaves `/tmp` not writable, and removes the default secret exclusions.

**Evidence:**
```bash
HOME="$PWD/t/noall-home" XDG_CONFIG_HOME="$PWD/t/xdg-noall" agent-sandbox bash -lc 'cat "$HOME/.ssh/secretkey"'
# -> prints TOPSECRET (not excluded)

HOME="$PWD/t/noall-home" XDG_CONFIG_HOME="$PWD/t/xdg-noall" agent-sandbox bash -lc 'echo INSIDE > t/noall-write-outside.txt'
# -> fails: Read-only file system
```

---

### Nested sandbox: inherited `@git` wrapper breaks

**Spec says:**
Command wrappers are inherited in nested sandboxes.

**Actual behavior:**
Nested sandboxes inherit the wrapper script but not its `/run/.../wrap-binary` helper, causing failures.

**Note:**
Likely due to the inner sandbox mounting a fresh `/run` tmpfs, so the wrapper helper path is missing.

**Evidence:**
```bash
agent-sandbox bash -lc 'agent-sandbox git status 2>&1 | sed -n "1p"; echo EXIT:$?'
# -> /usr/bin/git: line 2: /run/<id>/agent-sandbox/binaries/wrap-binary: No such file or directory
# -> EXIT:127
```

---

### Hardcoded `/dev` and `/proc` mounts are overrideable

**Spec says:**
`/dev` and `/proc` mounts are hardcoded and cannot be changed.

**Actual behavior:**
They can be replaced via `--exclude` or `--rw`, which changes `/dev` contents dramatically; `--rw /dev` can make `/dev/null` unusable and break shell startup scripts.

**Evidence (`--exclude`):**
```bash
agent-sandbox --exclude /proc bash -lc 'ls -ld /proc; ls /proc/self'
# -> /proc exists but /proc/self is missing

agent-sandbox --exclude /dev bash -lc 'ls -ld /dev; ls -l /dev/null'
# -> /dev exists but /dev/null is a regular file
```

**Evidence (`--rw /dev`):**
```bash
agent-sandbox --rw /dev bash -lc 'ls /dev | wc -l; echo hi > /dev/null; echo WRITE_EXIT:$?'
# -> /dev/null write fails: Permission denied
```

---

### Sandbox detection is not tamperproof

**Spec says:**
`agent-sandbox check` is tamperproof and detection is hardcoded (cannot be faked or disabled from inside the sandbox).

**Actual behavior:**
Excluding `/run` makes `check` report "outside". Creating `/run/.sandbox-marker` can fake "inside".

**Evidence (exclude /run):**
```bash
agent-sandbox --exclude /run bash -lc 'agent-sandbox check; echo CHECK_EXIT:$?'
# -> outside sandbox
# -> CHECK_EXIT:1
```

**Evidence (fake marker):**
```bash
agent-sandbox --exclude /run bash -lc '\
  agent-sandbox check -q; echo BEFORE:$?; \
  rm -f /run/.sandbox-marker; \
  agent-sandbox check -q; echo AFTER_RM:$?; \
  touch /run/.sandbox-marker; \
  agent-sandbox check -q; echo AFTER_TOUCH:$? \
'
# -> BEFORE:1, AFTER_RM:1, AFTER_TOUCH:0
```

---

### `@git` wrapper is bypassable via `/run/.../real/git`

**Spec says:**
Wrapper paths prevent trivial bypasses via alternate paths, and "operation forbidden" is a security guarantee; the real binary is mounted at a randomized hidden path.

**Actual behavior:**
The real git binary is directly executable at `/run/<random>/agent-sandbox/binaries/real/git`.

**Evidence:**
```bash
agent-sandbox -C t/gitrepo bash -lc '\
  real=$(ls -1 /run/*/agent-sandbox/binaries/real/git | head -n1); \
  "$real" checkout -b bypassed-branch \
'
```

---

### `@git` wrapper misses combined short flags (`-fd`, `-ff`, `-fu`)

**Spec says:**
Blocked operations like `git clean -f` and `git push --force` are prevented.

**Actual behavior:**
Combined short flags bypass the block.

**Evidence (clean -fd):**
```bash
agent-sandbox -C t/gitrepo bash -lc '\
  echo junk > junk3.txt; mkdir -p junkdir && echo junk > junkdir/file.txt; \
  git clean -fd; \
  ls -la junk3.txt junkdir/file.txt 2>/dev/null || echo MISSING \
'
```

**Evidence (push -fu):**
```bash
agent-sandbox -C t/gitrepo git push -fu origin bypass1
# -> git: '/.../remote.git' is not a git command. See 'git --help'.
```

---

### `@git` wrapper breaks `git-receive-pack`/`git-upload-pack`

**Spec says:**
Wrapper correctly handles git plumbing commands.

**Actual behavior:**
Symlinked git plumbing commands resolve to `git` and fail under the wrapper.

**Evidence:**
```bash
agent-sandbox -C t/gitrepo bash -lc '\
  git-receive-pack "$PWD/../remote.git" </dev/null 2>&1 | sed -n "1,2p"; echo EXIT:${PIPESTATUS[0]} \
'
# -> git: '/.../remote.git' is not a git command. See 'git --help'.
# -> EXIT:1
```

---

### SPEC.md doesn’t mention the `exec` subcommand

**Spec says:**
Only `agent-sandbox [global-flags] [exec-flags] <command>` and `check` are documented.

**Actual behavior:**
The implementation supports `agent-sandbox exec ...` and advertises it in `-h`.

**Evidence:**
```bash
agent-sandbox -h | sed -n '1,40p'
agent-sandbox exec echo hi
```

---

### `@git` wrapper doesn’t block abbreviated long flags

**Spec says:**
Wrapper blocks dangerous operations like `git reset --hard`, `git commit --no-verify`, and `git clean -f`.

**Actual behavior:**
Abbreviated long flags bypass the block.

**Evidence:**
```bash
agent-sandbox -C t/gitrepo git reset --har
agent-sandbox -C t/gitrepo git -c commit.gpgsign=false commit --no-veri --allow-empty -m 'abbrev noverify'
agent-sandbox -C t/gitrepo bash -lc 'echo junk > junkabbrev.txt; git clean --for; ls -la junkabbrev.txt 2>/dev/null || echo MISSING'
```

---

### `@git` wrapper doesn’t block forced branch deletion via `--delete --force` / `-d -f`

**Spec says:**
`git branch -D` is blocked.

**Actual behavior:**
Equivalent forced deletion forms are allowed.

**Evidence:**
```bash
agent-sandbox -C t/gitrepo git branch --delete --force force-delete-test
# (also works: agent-sandbox -C t/gitrepo git branch -d -f force-delete-test)
```

---

### Command config treats string "true"/"false" as paths

**Spec says:**
`true` allows raw command, `false` blocks command.

**Actual behavior:**
Strings "true"/"false" are treated as wrapper paths (`/bin/true`, `/bin/false`).

**Correct usage:**
Use JSON booleans (`true`/`false`, unquoted) to allow or block commands.

**Impact:**
Users who write `"true"`/`"false"` get unexpected behavior.

**Possible fix:**
Error on string values `"true"`/`"false"` or treat them as boolean equivalents.

**Evidence:**
```json
{
  "commands": {
    "git": "true",
    "npm": "false"
  }
}
```

```bash
agent-sandbox git --version
# -> runs /bin/true, not git

agent-sandbox npm --version
# -> runs /bin/false, not blocked
```

---

### SECURITY: `git -p` bypasses the wrapper

**Spec says:**
The wrapper properly parses git's global flags (e.g., `-C`, `--no-pager`) to identify subcommands regardless of flag position, and blocks dangerous operations.

**Actual behavior:**
`-p` (short for `--paginate`) bypasses the wrapper entirely.

**Impact:**
Attackers can bypass any git command restriction by prefixing with `-p` (e.g., `checkout`, `reset --hard`, `push --force`).

**Severity:**
HIGH.

**Root cause:**
The wrapper recognizes `--paginate` but not its short form `-p`.

**Evidence:**
```bash
agent-sandbox git checkout
# -> error: git checkout blocked ... (Exit: 1)

agent-sandbox git --paginate checkout
# -> error: git checkout blocked ... (Exit: 1)

agent-sandbox git -p checkout
# -> checkout runs successfully (Exit: 0)
```

---

### CRITICAL SECURITY: symlinks bypass excluded path protection

**Spec says:**
Excluded paths cannot be read/listed/detected; symlinks are resolved before mounting.

**Actual behavior:**
A symlink pointing to an excluded path can be added to `rw`/`ro` and reveals the excluded contents.

**Impact:**
Excluded secrets can be accessed via the symlink (e.g., `~/.ssh`, `~/.gnupg`, `~/.aws`, or any custom excluded path).

**Severity:**
CRITICAL.

**Root cause:**
A user-created symlink to an excluded path is resolved and mounted, bypassing the exclude check.

**Evidence:**
```bash
ln -s ~/.ssh ./ssh-link

cat .agent-sandbox.json
# {
#   "filesystem": {
#     "rw": ["ssh-link"]
#   }
# }

agent-sandbox cat ssh-link/config
# -> shows real ~/.ssh contents
```

---

### Preset secret exclusions can be overridden via later rules

**Spec says:**
Secrets like `~/.ssh` are excluded by default presets.

**Actual behavior:**
Later rules (e.g. `--rw`) can override the preset exclusion and expose real contents.

**Evidence:**
```bash
HOME="$PWD/t/fakehome" agent-sandbox --rw "$PWD/t/fakehome/.ssh" bash -lc 'cat "$HOME/.ssh/secretkey"'
# -> succeeds
```

---

## Features tested and reported as working correctly

The second report also claimed these behaviors were correct during testing:

- Command structure (`agent-sandbox [flags] <command>` and `agent-sandbox exec [flags] <command>`)
- Global flags (`--cwd`/`-C`, `--config`/`-c`, `--help`/`-h`, `--version`/`-v`)
- Exec flags (`--network`, `--docker`, `--dry-run`, `--debug`, `--ro`, `--rw`, `--exclude`, `--cmd`)
- Boolean flag syntax (`--flag`, `--flag=false`, `--flag=0`)
- Check command (`agent-sandbox check`, `agent-sandbox check -q`)
- Exit codes (0 for success, 1 for error, 130 for interrupted, check: 0=inside/1=outside)
- Signal handling (forwarding, 10s timeout, exit code 130)
- Configuration loading order (defaults → global → project/explicit → CLI)
- JSON and JSONC config file support (comments work)
- Duplicate config file detection (both .json and .jsonc = error)
- File access model (ro, rw, exclude)
- Path patterns (tilde expansion, globs with `*`, multiple wildcards)
- Symlink resolution
- Specificity rules (longer path wins, later layer wins)
- Filesystem presets (@base, @caches, @agents, @git, @lint/*)
- Secrets excluded (~/.ssh, ~/.gnupg, ~/.aws show as empty)
- Config files protected (read-only inside sandbox)
- Lint configs protected (biome.json, eslint.config.js, tsconfig.json, etc.)
- Git hooks and config protected (.git/hooks, .git/config)
- @git command wrapper (blocks checkout, reset --hard, push --force, etc.)
- Custom command wrappers (scripts receive AGENT_SANDBOX_<CMD> env var)
- Command blocking (`--cmd rm=false`)
- Config merging (arrays concatenated, booleans overwritten)
- Nested sandboxes (restrictions inherited, --cmd inside = error)
- Network isolation (`--network=false` works)
- Docker socket access (`--docker` enables /var/run/docker.sock)
- Sandbox detection (tamperproof `/run/.sandbox-marker`)
- Environment variables passed through unchanged
- Root filesystem read-only by default
- Working directory writable
- Cache paths writable (~/.cache, ~/.npm, ~/.bun, etc.)
- Agent config paths writable (~/.claude, ~/.codex, ~/.pi)
- Error handling (invalid JSON, unknown preset, $PWD in excluded path)
- Relative paths in global config resolve to effective pwd
- Non-existent paths skip silently
- Command arguments with dashes passed through correctly

---

## Notes from the original reports

- One report described the implementation as highly complete and closely matching the spec, with issues characterized as minor documentation/spec sync gaps, stated it found seven discrepancies in total, and said the security guarantees appeared properly enforced.
- The other report recorded numerous functional and security mismatches, including several critical bypasses of excluded paths and git wrapper protections.
