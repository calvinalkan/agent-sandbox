Study @SPEC.md
Study @TECHNICAL_STEERING.md

Start your assignined task by running `tk start $$ID`, this will give you detailed
informat about your assigned task. You must only work on this task.

Always compare the current state of the code with the specification.
If you think that a ticket is no longer applicable, you can close it and commit with a message
that explains why. If you discover new issues, you can create a new ticket (`tk create --help`)

Implement the task, ensure all acceptance criteria are met, and all e2e tests are authored for new functionality.

All functioanlity should have tests that erorr handling works as expected.

Remember, test names must be in the pattern of `Test_Foo_Does_Bar_When_Baz`,
look at `cmd/agent-sandbox/testing_test.go` for how use the cli tester.

Run `make lint`, `make modernize` and `make test` frequently.

Do not add lint suppressions rules ever, you will not be able to commit your code, and you can't change
lint rules either.

When you think you are done, run `make check` for comprehensive tests.

Then, complete the task by running `tk close $$ID`, and then commit your changes and the ticket itself with git. You must run `tk close $$ID` BEFORE committing.
Use conventional commit messages, and reference the ticket in the first line of your commit message.

Then, run `wt merge` to merge back your worktree.

Then tell the user what you accomplished as a quick tl;dr, and stop.
