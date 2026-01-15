---
schema_version: 1
id: d5gr4s8
status: open
blocked-by: []
created: 2026-01-09T22:34:13Z
type: chore
priority: 2
---
# Review skipped sandbox tests

Review three skipped tests that require running inside a sandbox:

1. Test_WrapBinaryCmd_Works_Inside_Sandbox - E2E test deferred to a ticket
2. Test_Nested_Sandbox_CmdFlag_Errors_Inside_Sandbox - requires running inside sandbox
3. Test_Nested_Sandbox_Config_Commands_Ignored_Inside_Sandbox - requires running inside sandbox

Determine if these tests can be implemented as E2E tests or if they should remain skipped.
