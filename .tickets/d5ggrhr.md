---
schema_version: 1
id: d5ggrhr
status: closed
closed: 2026-01-09T15:29:54Z
blocked-by: []
created: 2026-01-09T14:10:15Z
type: task
priority: 1
---
# Refactor wrappers to use --ro-bind-data instead of temp files

Replace temp file wrapper scripts with FD-based injection.

## Current approach
1. Create /tmp/agent-sandbox-wrappers-XXXX/
2. Write wrap-git, deny-binary as files
3. chmod +x
4. --ro-bind /tmp/.../wrap-git /usr/bin/git
5. Cleanup on exit (currently broken)

## Problems
- Security: other sandboxes can read/overwrite wrappers in shared /tmp
- Cleanup: orphaned dirs accumulate (see d5gg80g)
- Complexity: temp dir management, defer cleanup

## New approach
1. Generate wrapper script as string
2. Create pipe, write script to write-end, close
3. --perms 0555 --ro-bind-data <fd> /usr/bin/git
4. Done - no files, no cleanup

## Benefits
- Security: wrappers only exist in sandbox memory
- No cleanup needed
- Simpler code: remove temp dir logic, WrapperSetup cleanup
- Faster: no disk I/O

## Changes needed
- wrapper.go: remove temp file creation, add FD-based injection
- bwrap.go: use --perms 0555 --ro-bind-data for wrappers
- cmd_exec.go: remove cleanup defer
- tests: remove any that check temp file internals

## Closes
- d5gg80g (cleanup not working - no longer needed)
