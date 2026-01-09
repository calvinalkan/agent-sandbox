---
schema_version: 1
id: d5ghadr
status: closed
closed: 2026-01-09T15:17:02Z
blocked-by: []
created: 2026-01-09T14:48:23Z
type: bug
priority: 2
---
# CLI flags don't override project config for same path

When the same path is specified in both project config and CLI flags with different access levels, the CLI should win (per spec: 'CLI > project > global > preset'), but currently the project config's setting is used instead.

Example:
- Project config: ro: ["examples/"]
- CLI: --rw examples/
- Expected: examples/ is writable (CLI wins)
- Actual: examples/ is read-only (project wins)

Debug output shows both entries but generates --ro-bind-try instead of --bind-try.
