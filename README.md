# agent-sandbox

A filesystem sandbox for agentic coding workflows using [bubblewrap](https://github.com/containers/bubblewrap). Protects system files and sensitive data from modification while allowing AI coding agents to work within designated areas.

**Platform:** Linux only (requires bwrap and Linux namespaces)

## Features

- **Filesystem Protection:** Prevent agents from modifying system files, configs, and secrets
- **Smart Defaults:** The `@all` preset protects common secrets (~/.ssh, ~/.gnupg, ~/.aws), lint configs, and git hooks out of the box
- **Git Safety:** Built-in `@git` wrapper blocks dangerous operations like `git reset --hard` and `git push --force`
- **Sandbox Detection:** Agents can detect when they're sandboxed via `agent-sandbox check`
- **Zero Config Start:** Works immediately with sensible defaults
- **Flexible Configuration:** Project and global config files for customization

## Installation

### From Binary

Download the latest release from the [releases page](https://github.com/calvinalkan/agent-sandbox/releases):

```bash
# Download (replace with your platform)
curl -LO https://github.com/calvinalkan/agent-sandbox/releases/latest/download/agent-sandbox-linux-amd64

# Make executable
chmod +x agent-sandbox-linux-amd64

# Move to PATH
sudo mv agent-sandbox-linux-amd64 /usr/local/bin/agent-sandbox
```

### From Source

Requires Go 1.24+:

```bash
go install github.com/calvinalkan/agent-sandbox/cmd/agent-sandbox@latest
```

Or build from source:

```bash
git clone https://github.com/calvinalkan/agent-sandbox.git
cd agent-sandbox
make build
sudo mv agent-sandbox /usr/local/bin/
```

### Dependencies

Install bubblewrap (required):

```bash
# Debian/Ubuntu
sudo apt install bubblewrap

# Fedora
sudo dnf install bubblewrap

# Arch
sudo pacman -S bubblewrap
```

Verify installation:

```bash
agent-sandbox --version
agent-sandbox echo "Hello from sandbox!"
```

## Quick Start

Run any command inside the sandbox:

```bash
# Run a command
agent-sandbox npm install

# The command and all its arguments are passed through
agent-sandbox npm run build -- --verbose
```

The sandbox applies sensible defaults:
- Working directory is writable
- Home directory is readable (new files allowed, existing protected)
- Common caches (~/.cache, ~/.npm, ~/.bun, ~/go) are writable
- Secrets (~/.ssh, ~/.gnupg, ~/.aws) are hidden
- Git hooks and lint configs are read-only
- Network access is enabled

## Configuration

### CLI Flags

```bash
# Disable network access
agent-sandbox --network=false npm test

# Add read-only protection
agent-sandbox --ro src/auth/ node build.js

# Add writable path
agent-sandbox --rw ./dist npm run build

# Hide sensitive files
agent-sandbox --exclude .env npm test

# Enable docker socket access
agent-sandbox --docker docker build .

# Override command wrapper
agent-sandbox --cmd git=true git checkout main

# Show what bwrap command would run
agent-sandbox --dry-run npm install

# Debug output
agent-sandbox --debug npm install
```

### Project Config

Create `.agent-sandbox.json` or `.agent-sandbox.jsonc` in your project:

```jsonc
{
  "filesystem": {
    // Don't protect Python lint configs (not used in this project)
    "presets": ["!@lint/python"],
    
    // Extra read-only protection
    "ro": ["src/auth/", "config/*/secrets.json"],
    
    // Allow writing to generated files
    "rw": [".generated/"]
  },
  
  "commands": {
    "rm": false  // Block rm entirely
  }
}
```

### Global Config

Create `~/.config/agent-sandbox/config.json` or `config.jsonc`:

```jsonc
{
  // Enable docker for all projects
  "docker": true,
  
  "filesystem": {
    // Extra writable paths
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

### Config Loading Order

Configuration is merged in this order (later overrides earlier):

1. Built-in defaults (`@all` preset, network on, docker off)
2. Global config (`~/.config/agent-sandbox/config.json`)
3. Project config (`.agent-sandbox.json` in current directory)
4. CLI flags

## Filesystem Presets

Presets are built-in named configurations. The `@all` preset is always applied by default.

| Preset | Description |
|--------|-------------|
| `@all` | Everything below combined |
| `@base` | Working dir writable, home protected, secrets excluded |
| `@caches` | Build tool caches writable (~/.cache, ~/.bun, ~/go, ~/.npm, ~/.cargo) |
| `@git` | Git hooks and config protected (.git/hooks, .git/config) |
| `@lint/ts` | TypeScript/JS lint configs protected (biome, eslint, prettier, tsconfig) |
| `@lint/go` | Go lint configs protected (golangci) |
| `@lint/python` | Python lint configs protected (ruff, flake8, mypy, pylint, pyproject.toml) |
| `@lint/all` | All lint presets combined |

Remove a preset with `!@preset`:

```jsonc
{
  "filesystem": {
    "presets": ["!@lint/python", "!@lint/go"]
  }
}
```

## Command Wrappers

Command wrappers intercept specific binaries to enforce safety rules.

```jsonc
{
  "commands": {
    "git": "@git",                  // Use built-in wrapper (default)
    "rm": false,                    // Block entirely
    "npm": true,                    // Raw command (remove any wrapper)
    "curl": "~/bin/curl-wrapper"    // Custom wrapper script
  }
}
```

### @git Wrapper

The built-in `@git` wrapper blocks dangerous operations:

| Blocked | Reason | Alternative |
|---------|--------|-------------|
| `git checkout` | Can discard changes | `git switch` for branches |
| `git restore` | Discards changes | Commit or stash first |
| `git reset --hard` | Discards commits | `git reset --soft` or `git revert` |
| `git clean -f` | Deletes untracked files | Manual review |
| `git push --force` | Rewrites remote history | `git push --force-with-lease` |
| `git stash drop/clear/pop` | Can lose work | `git stash apply` |
| `git branch -D` | Force deletes branch | `git branch -d` (safe delete) |
| `git commit --no-verify` | Bypasses hooks | Fix the hook issues |

Override for specific commands:

```bash
agent-sandbox --cmd git=true git checkout main
```

### Custom Wrappers

Write a wrapper script for custom logic:

```bash
#!/bin/bash
# ~/.config/agent-sandbox/npm-wrapper.sh
case "$1" in
  publish|unpublish|deprecate)
    echo "npm $1 blocked by sandbox" >&2
    exit 1
    ;;
esac
exec "$AGENT_SANDBOX_NPM" "$@"
```

Configure it:

```jsonc
{
  "commands": {
    "npm": "~/.config/agent-sandbox/npm-wrapper.sh"
  }
}
```

The wrapper receives the real binary path via `$AGENT_SANDBOX_<CMD>` environment variable.

## Sandbox Detection

Check if running inside a sandbox:

```bash
# In scripts
if agent-sandbox check -q; then
  echo "inside sandbox"
fi

# Human readable
agent-sandbox check
```

Exit codes: `0` = inside sandbox, `1` = outside sandbox.

The detection is tamperproof — it cannot be faked from inside the sandbox.

## Examples

### Protect Secrets During Development

```bash
# .env is hidden, can't be read or detected
agent-sandbox --exclude .env node server.js
```

### Build with Limited Access

```bash
agent-sandbox \
  --ro src/ \
  --rw dist/ \
  --network=false \
  npm run build
```

### Test Without Network

```bash
agent-sandbox --network=false npm test
```

### Run Docker Commands

```bash
agent-sandbox --docker docker build -t myapp .
```

### Different Working Directory

```bash
agent-sandbox -C ~/other-project npm install
```

### Debug Configuration

```bash
# See what paths are protected/writable
agent-sandbox --debug npm install

# See the bwrap command that would run
agent-sandbox --dry-run npm install
```

## Troubleshooting

### "bwrap not found in PATH"

Install bubblewrap:

```bash
sudo apt install bubblewrap  # Debian/Ubuntu
sudo dnf install bubblewrap  # Fedora
sudo pacman -S bubblewrap    # Arch
```

### "agent-sandbox requires Linux"

agent-sandbox only works on Linux because it uses bwrap (bubblewrap), which requires Linux namespaces.

### "cannot run as root"

agent-sandbox refuses to run as root for security reasons. Use a regular user account.

### "cannot determine home directory"

Set the `$HOME` environment variable:

```bash
HOME=/home/myuser agent-sandbox npm install
```

### Command wrapper blocks something I need

Override the wrapper for that command:

```bash
agent-sandbox --cmd git=true git checkout main
```

Or in config:

```jsonc
{
  "commands": {
    "git": true
  }
}
```

### Path I need is read-only

Add it to the `rw` list:

```bash
agent-sandbox --rw /path/to/dir npm install
```

Or in config:

```jsonc
{
  "filesystem": {
    "rw": ["/path/to/dir"]
  }
}
```

### Glob patterns not matching

- Globs only match existing paths at sandbox startup
- No `**` recursive glob support — use `*` for single level
- Environment variables are not expanded (`$HOME` is literal)

### Config file not being loaded

Check with `--debug`:

```bash
agent-sandbox --debug npm install
```

The output shows which config files were found and loaded.

## Reference

See [SPEC.md](SPEC.md) for the complete specification.

## License

MIT
