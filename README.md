# agent-sandbox

Filesystem-protected wrapper for coding agents using [bwrap](https://github.com/containers/bubblewrap).

## What it does

| Path | Access |
|------|--------|
| `/` (system files) | Read-only |
| `$HOME` (directory) | Writable (can create new files) |
| `$HOME/*` (existing content) | Read-only |
| `$HOME/.claude`, `.claude.json`, `.pi`, `.opencode`, `.codex`, `.bun` | Writable (agent configs) |
| `$PWD` | Writable (working directory) |
| `/tmp` | Writable |
| `.git/hooks`, `.git/config` | Read-only (protected) |
| Network | Full access (localhost, internet) |
| Docker | Available |

## How it works

The sandbox uses bwrap mount overlays in a specific order:

1. `--ro-bind / /` — Everything read-only as base
2. `--bind $HOME $HOME` — Home directory writable (allows creating new files)
3. `--ro-bind $HOME/<item>` — Each existing file/dir in home made read-only (except agent configs)
4. `--bind-try <paths>` — Specific paths made writable (agent configs, `$PWD`, etc.)

This design allows agents to create temp files for atomic writes (e.g. `.claude.json.tmp.xxx`) while protecting existing home directory content from modification.

## Usage

```bash
agent-sandbox <agent> [args...]
```

Supported agents: `claude`, `pi`, `opencode`, `codex`

```bash
agent-sandbox claude -p "refactor this function"
agent-sandbox pi "explain this code"
agent-sandbox opencode run "fix the bug"
agent-sandbox codex exec "write tests"
```

## Agent behavior

### pi

pi has no permission system - it runs everything without prompts by default. The sandbox adds filesystem protection without changing pi's behavior.

### opencode

OpenCode allows most operations by default, except `external_directory` (files outside cwd) which prompts. Since the sandbox already handles filesystem protection, add this to `~/.config/opencode/config.json` to skip the redundant prompt:

```json
{
  "permission": {
    "external_directory": "allow"
  }
}
```

Other OpenCode permission settings (edit, bash, webfetch, doom_loop) are respected as configured.

### claude

Claude runs with `--permission-mode acceptEdits`, which auto-approves file edits and filesystem operations (mkdir, rm, mv, cp) but still prompts for other bash commands. This pairs well with bwrap's filesystem protection while maintaining some oversight on command execution.

### codex

Codex runs with `--sandbox danger-full-access`, disabling Codex's internal sandbox (bwrap handles filesystem protection). This also enables network access. Codex's approval system remains active for command execution.

## Installation

The script is at `~/.local/bin/agent-sandbox`.

## Recommended aliases

Add to your `~/.bashrc` or `~/.bash_aliases`:

```bash
# Sandboxed by default
alias pi='agent-sandbox pi'
alias claude='agent-sandbox claude'
alias opencode='agent-sandbox opencode'
alias codex='agent-sandbox codex'

# Optional: reduce codex approval prompts (up to you)
alias codex='agent-sandbox codex --ask-for-approval on-request'
```

To bypass the sandbox when needed:
```bash
\pi                    # backslash bypasses alias
command pi             # explicit bypass
```

## Troubleshooting

**Ubuntu 24.04+ "Operation not permitted" error**: You may need an AppArmor profile for bwrap. See [this guide](https://github.com/containers/bubblewrap/issues/612).
