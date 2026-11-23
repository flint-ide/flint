---
description: Plan a feature, implement it, have it reviewed, then approve
---

## Plan

Read the task below and the code it touches. Write a short plan to PLAN.md: what to change, in which files, and how you will check it works. Keep it to one page. Do not change any code. Commit PLAN.md.

## Implement

Do what PLAN.md says, and nothing more. Add tests for the new behaviour, run the tests, and make sure they pass. Commit your work.

## Review
type: review
on-fail: Implement

Check that the change does what the task asks and follows PLAN.md, that it has tests, and that it has no bugs or leftovers (debug output, unfinished code). List what must change, if anything.

## Approve
type: gate

Read the diff and try the feature if you can. Approve to finish, or send the run back to a step with a note.
