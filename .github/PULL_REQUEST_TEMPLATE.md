## What and why

<!-- What does this change and why is it needed? Link the issue it closes. -->

## How it was tested

<!-- Tests added or updated, and what you checked by hand in the app. -->

## Checklist

- [ ] `make test` and `make lint` pass
- [ ] Bindings regenerated (`wails3 task bindings`) if Go service signatures changed
- [ ] Anything passed to a CLI is validated and never read as a flag; no secrets in logs (`internal/redact`)
- [ ] No new npm dependency (or it is discussed in the issue)
- [ ] Docs updated (`docs/FEATURES.md`, README) for user-visible changes
