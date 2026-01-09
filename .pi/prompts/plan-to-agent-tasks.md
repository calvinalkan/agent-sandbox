---
description: Convert a refined plan to agents tasks
hardness: pi:opus-4.5
---

Read the entire plan at @SPEC.md and the @TECHNICAL_STEERING.md ,and elaborate on it more and then create a comprehensive and granular set of `agent-tasks` for all this with tasks, subtasks, and dependency structure overlaid, with detailed comments so that the whole thing is totally self-contained and self-documenting (including relevant background, reasoning/justification, considerations, etc.-- anything we'd want our "future self" to know about the goals and intentions and thought process and how it serves the over-arching goals of the project.)  Use only the `tk` tool to create and modify the `agent-tasks` and add the dependencies.  

Correctly set up all the dependencies/blockers between the different tasks, so that we can work effectively in parallel.

Each agent task needs clear acceptance criteria, a list of key invariants that exist, and all different error
states that should be handled. Make tasks granular, you can split a big task into smaller ones. Use your best judgement to decide what is the right granularity for a task.

Run `tk --help`, and `tk create --help` for more information on how to use the `tk` ticket tool.
