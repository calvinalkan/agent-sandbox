---
schema_version: 1
id: d5ggzs0
status: closed
closed: 2026-01-09T14:43:30Z
blocked-by: []
created: 2026-01-09T14:25:40Z
type: task
priority: 3
---
# Shadow /usr/lib/git-core/git to prevent @git wrapper bypass

The @git wrapper can be bypassed by calling the real binary directly:

```
git checkout -- .                    # blocked by wrapper
/usr/lib/git-core/git checkout -- .  # bypasses wrapper
```

Threat model: over-eager agent stuck on task, not malicious actor.
Goal is friction, not perfect security.

## Fix
Add /usr/lib/git-core/git to the list of paths shadowed by the @git wrapper.

## Test
E2E test that verifies:
1. `git checkout` is blocked
2. `/usr/lib/git-core/git checkout` is also blocked
