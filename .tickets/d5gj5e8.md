---
schema_version: 1
id: d5gj5e8
status: closed
closed: 2026-01-09T16:02:51Z
blocked-by: []
created: 2026-01-09T15:46:01Z
type: feature
priority: 3
---
# @git-strict preset: lock down all branches except current

Add a new @git-strict preset that includes all @git protections plus branch lockdown - agent can only commit on current branch, all other branches are read-only.

**Implementation:**
- Detect all branches in .git/refs/heads/*
- ro-bind all branch refs except current branch
- Also protect .git/refs/tags/* (prevent tag creation/modification)
- Works correctly with worktrees (follow gitdir to main repo)

**Behavior:**
- git commit on current branch: works
- git branch -f master HEAD: fails (can't modify other branches)
- git tag v1.0: fails (can't create tags)
- git push: works (but can only push current branch)

**E2E tests needed:**
- Commit on current branch works
- Cannot modify other branch refs (git branch -f)
- Cannot delete other branches (git branch -d)
- Cannot create/modify/delete tags
- Works in main repo
- Works in worktree (protects main repo's refs)
- Stashing still works (if it needs ref access)
- Cherry-pick onto current branch works
- Rebase current branch works
- Merge into current branch works
- Cannot force-push other branches
- Direct ref file manipulation blocked (.git/refs/heads/master)
