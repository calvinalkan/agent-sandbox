---
schema_version: 1
id: d5gkaj0
status: open
blocked-by: []
created: 2026-01-09T17:05:12Z
type: task
priority: 2
---
# Improve error handling for bwrap startup failures

When bwrap itself fails to start (e.g., 'bwrap: setting up uid map: Permission denied'), the error is surfaced directly without context. Users may not realize the error comes from sandbox setup rather than their command.

**Current behavior:**
- bwrap errors are shown raw, e.g., 'bwrap: setting up uid map: Permission denied'
- No indication this is a sandbox startup issue vs command failure

**Desired behavior:**
- Wrap bwrap startup errors with context like 'sandbox startup failed: <bwrap error>'
- Command errors (from user's command inside sandbox) should be surfaced as-is

**Possible approach:**
- String match for 'bwrap:' prefix in stderr to detect bwrap-originated errors
- Or check exit code patterns if bwrap has specific exit codes for setup failures
- Need to find where bwrap is actually executed (not in bwrap.go which only generates args)
