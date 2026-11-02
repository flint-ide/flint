# Contributing to Flint

Thanks for your interest in Flint. This document explains how to propose changes and what a pull request needs before it can be merged.

By participating you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

Read [docs/PRINCIPLES.md](docs/PRINCIPLES.md) first: minimal dependencies, simplicity, extensibility, AI-first and configurability decide what gets merged. A pull request that trades one of them away says so and why.

## Proposing changes

- **Bugs**: open an issue, ideally with **Help ▸ Report an Issue…** in Flint, which fills in versions and a redacted log tail. Include steps to reproduce.
- **Small fixes** (typos, obvious bugs, small improvements): open a pull request directly.
- **Big features or architectural changes**: open an issue first and describe the problem and the approach. Flint deliberately stays small and fast, so agreeing on scope before you write code saves everyone time.
- **Security vulnerabilities**: do not open a public issue. Follow [SECURITY.md](SECURITY.md).

## Development setup

See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for requirements, `make dev`, tests and bindings. [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) explains how the code is organized.

## Branches and commits

- Branch from `main`. Use a short descriptive name, for example `feat/http-client` or `fix/terminal-resize`.
- Use [Conventional Commits](https://www.conventionalcommits.org/):

  ```
  <type>: <description>

  <optional body explaining why>
  ```

  Types: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `perf`, `ci`.
- Keep commits focused. A pull request should do one thing.

## Tests

Every change in behavior needs tests.

- **Go**: table-driven tests next to the code (`*_test.go`). Run `make test` (it uses `-race`). Database code that talks to a real server goes behind the `integration` build tag (`make test-db-up && make test-integration`).
- **Frontend**: pure logic belongs in `frontend/src/lib` and is tested with `node:test` and `node:assert` in `*.test.ts` files next to it. Keep components thin so the logic can be tested without a DOM.
- Tests must not touch the network or your real configuration. Use `FLINT_CONFIG_DIR` for a temporary config directory, fakes on `PATH` for external CLIs, and local fakes for network APIs (see the fake GitHub in `internal/github/githubtest`).

## Code style

- Go is formatted with `gofmt` and must pass `go vet` (`make lint`).
- TypeScript must pass `tsc --noEmit` (`make test` runs it).
- Prefer small, focused files and functions. Organize by feature, not by type.
- Comments explain why, not what. Give new Go packages and exported identifiers a doc comment.
- Heavy work (diffs, matching, parsing) belongs in Go; the UI renders results.
- **No new npm dependency without discussion** in an issue first. Flint keeps a minimal frontend dependency set on purpose. Do not use `npx`, and do not add npm-based build tools.
- If you change a Go service (`api_*.go`) or a type it returns, regenerate the bindings with `wails3 task bindings` and commit `frontend/bindings`. CI fails if they are stale.
- Update [docs/FEATURES.md](docs/FEATURES.md) and [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) when you add or change a feature or a stored file.

## Security rules for contributors

Flint opens untrusted repositories and renders untrusted content, and every Go binding is callable from the WebView. Keep these rules in mind (the threat model is in [ARCHITECTURE.md](docs/ARCHITECTURE.md#security-model)):

- **Validate anything passed to a CLI.** Names that come from the UI or from command output (containers, contexts, namespaces, branches, profiles) must be checked against a strict pattern before they reach `exec.Command`.
- **Never pass user strings as flags.** Put user values where they cannot be parsed as options (after `--`, as a validated positional argument, or through stdin). Never build a shell command line from user input.
- **No secrets in logs.** Anything that may contain credentials, tokens or personal paths goes through `internal/redact` before it reaches the log, a bug report, or an agent prompt. Database passwords belong in the Keychain only.
- **No raw HTML rendering.** Do not use `dangerouslySetInnerHTML` with untrusted content, and do not loosen the Content Security Policy in `cmd/webbuild`. Markdown is rendered in Go without raw HTML.
- **Keep bindings narrow.** Any new exported method on a service is callable by any UI code, including code that an injection could run. Expose specific operations (for example "rename this path inside a workspace root") instead of general ones (for example "run this command"), and validate arguments in Go.
- **Respect Restricted Mode.** Anything that could execute project code (language servers, hooks, project-level agent settings, auto-started terminals) must check workspace trust.

## Pull request checklist

Before you request a review:

- [ ] The change is focused and the description explains what and why
- [ ] `make test` passes
- [ ] `make lint` passes (and `make vuln` if you changed dependencies)
- [ ] New behavior has tests
- [ ] Bindings regenerated and committed if a Go service changed
- [ ] No new npm dependency (or it was agreed in an issue)
- [ ] User-visible changes are documented in `docs/FEATURES.md`
- [ ] The security rules above are followed
- [ ] Commits follow Conventional Commits

## Sign-off

A Developer Certificate of Origin (DCO) sign-off is not required. By submitting a pull request you agree that your contribution is licensed under the project's [MIT License](LICENSE).
