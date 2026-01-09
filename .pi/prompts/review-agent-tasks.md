---
description: Review and improve agent tasks
hardness: pi:opus-4.5
---

1. Study @SPEC.md
2. Study @TECHNICAL_STEERING.md
3. Run `tk --help`
4. `tk ls --help` 
5. Understand how the ticket tool works.

Check over each ticket that we created with `tk` super carefully -- are you sure it makes sense? Is it optimal? Could we change anything to make the system work better for users? If so, revise the ticket. It's a lot easier and faster to operate in "plan space" before we start implementing these things!

You can see all tickets with `tk ls`, which also shows you the titles, and IDs, and block relatioinships between tickets.

You can add modifify dependencies between tasks with `tk block | unblock ` and you can edit ticket content
(not the frontmatter) inside `.tickets/<id>.md`.
