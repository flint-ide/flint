## What and why

<!-- What does this change and why is it needed? Link the issue it closes. -->

## How it was tested

<!-- Tests added or updated, and what you checked by hand in the app. -->

## Release notes

<!--
What users will notice, written for them: what it does, how to find it, and anything that changed behaviour or needs action. Markdown. It goes into the release notes under a heading made from this pull request's title. Write "none", or leave it empty, for changes users don't see (refactoring, tests, CI, docs for contributors).

For several parts, use ### headings. Put what users must do or know when they update (a renamed setting, a changed default, a behaviour that changed) under a "### Upgrade notes" heading: it goes to the release's Upgrade notes.

In the release, @mentions are shown as code and HTML as text. This comment is removed.
-->

## Checklist

- [ ] `make test` and `make lint` pass
- [ ] Bindings regenerated (`wails3 task bindings`) if Go service signatures changed
- [ ] Anything passed to a CLI is validated and never read as a flag; no secrets in logs (`internal/redact`)
- [ ] No new npm dependency (or it is discussed in the issue)
- [ ] Docs updated (`docs/FEATURES.md`, README) for user-visible changes
- [ ] Release notes written above for user-visible changes ("none" otherwise)
