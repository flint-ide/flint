---
description: Reproduce the bug, fix it, have it reviewed, then approve
---

## Reproduce
Find out why the bug happens. Read the task below and the code around it.

Write a test that fails because of the bug, and run it to see it fail. If a test is not practical here, write down exactly how to see the bug, in REPRODUCE.md. Do not fix anything yet. Commit what you wrote.

## Fix

Fix the bug the first step found, with the smallest change that does it. Run the new test and the tests around it, and make sure they pass. Do not clean up or change other things. Commit your work.

## Review
type: review
on-fail: Fix

Check that the change fixes the bug the task describes, that the test fails without the fix and passes with it, and that nothing else changed that should not have. List what must change, if anything.

## Approve
type: gate

Read the diff. Does it fix the bug, and only the bug? Approve to finish, or send the run back to a step with a note.
