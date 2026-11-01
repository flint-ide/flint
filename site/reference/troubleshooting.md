# Troubleshooting

Solutions to common problems. If yours isn't here, see [Report a problem](../how-to/report-a-problem.md).

## Flint won't open

**Symptom:** macOS says Flint can't be opened, or that it can't check it for malicious software.

Release builds that aren't notarized are blocked by Gatekeeper on first launch. Either:

- right-click Flint in Applications, choose **Open**, and confirm; or
- try to open it once, then go to **System Settings ▸ Privacy & Security** and click **Open Anyway**.

You only need to do this once per version. See [Installation](../installation.md#first-launch).

## The flint command doesn't work

- `command not found`: make sure `~/.local/bin` is on your `PATH`, and that the command is installed (see [the flint command](../installation.md#the-flint-command)).
- `Flint.app is not installed`: the command looks for Flint in `/Applications` and `~/Applications`. Move the app to one of them.
- `… is not a folder`: the command opens folders only. Open single files from inside Flint.

## A panel is empty or a feature is missing

Most panels drive a command-line tool: Pull Requests needs `gh`, Kubernetes needs `kubectl`, Docker needs a running Docker engine (and `docker` only for Compose up, down and pull), AWS Profiles needs `aws`, agent chat needs `claude` or `codex`. Choose **Help ▸ Check Tools…** (or "Check Tools…" in the command palette) to see which tools Flint found, their versions, and an install command for each missing one.

- **Not installed** although it is: Flint uses your login shell's `PATH`. Check that the tool runs in a new Terminal window; if you just added its folder to `PATH` (for example `~/go/bin`), restart Flint.
- **Not working**: the tool is on your `PATH` but its version command failed. The window shows the error. A common case is `rust-analyzer` provided by rustup without the component installed: run `rustup component add rust-analyzer`.

See [Recommended tools](../installation.md#recommended-tools).

## A language server doesn't start

Check the status bar; it shows each server's state and, when a server is missing, the command to install it.

1. **Install the server.**

   | Language | Install |
   |---|---|
   | Go | `go install golang.org/x/tools/gopls@latest` |
   | Rust | `rustup component add rust-analyzer` |
   | TypeScript / JavaScript | `npm i -g @vtsls/language-server` (or `npm i -g typescript-language-server typescript`) |
   | Python | `pipx install basedpyright` (or `npm i -g pyright`) |

   Flint finds servers through your login shell's `PATH`, so a server you can run in Terminal is found by Flint too.

2. **Check Restricted Mode.** In an untrusted folder, rust-analyzer and the Python servers don't start because they run project code. The message says "Restricted Mode: trust this folder to start its language server". Trust the folder if you trust its authors. See [Restricted Mode](../guides/restricted-mode.md).

3. **TypeScript pin message.** If you see "typescript-language-server: no TypeScript installed next to it, and the project's own is never used", Flint refuses to run the project's `node_modules/typescript`, for safety. Install TypeScript next to the server with `npm i -g typescript-language-server typescript`, or install vtsls instead, which uses its own bundled TypeScript.

## Search or Go to file is slow

Install ripgrep:

```bash
brew install ripgrep
```

Without it, Flint uses a slower built-in search for `⇧⌘F`, `⌘P` and the TODO list.

## No agent is available

If the `+` menu shows no chat option, or Settings ▸ Agents says "No agent installed (claude or codex)":

1. Install the Claude Code or Codex CLI and sign in to it in Terminal.
2. Check that `claude` or `codex` runs from a new Terminal window. Flint uses your login shell's `PATH`, so the CLI must be on it there.
3. Restart Flint.

If an agent is installed but you get the wrong one by default, pick it in Settings ▸ Agents ▸ **Default agent**.

## Kubernetes: authentication expired

If the Kubernetes panel shows authentication errors:

- **EKS:** the panel shows the AWS profile of the context and its validity. Click refresh next to it, or **Log In** if the login has expired. See [Manage AWS profiles](../how-to/manage-aws-profiles.md).
- **Other providers:** sign in again the way you do in Terminal (for example with your OIDC or `gcloud` login), then refresh the panel.

## AWS refresh fails with "needs input"

Refresh runs without a terminal, so it can't answer questions such as an MFA code. When the tool asks for input, refresh stops and shows a **Log In** button. Click it to finish the login in a terminal, where you can type the answer.

For SSO profiles, refresh waits up to 5 minutes for you to approve the login in your browser.

## Postgres: TLS certificate error

With **TLS** on, Flint verifies the server certificate and host name. Connections to servers with self-signed certificates or a private certificate authority fail this check.

If you trust the network path to the server, edit the connection and tick **Don't verify certificate (insecure)**. The connection stays encrypted but is no longer protected against someone in between reading the traffic.

## The debugger doesn't start

- Install Delve: `go install github.com/go-delve/delve/cmd/dlv@latest`.
- "Version of Delve is too old for Go version …": each Delve release supports Go versions up to its own; after upgrading Go, upgrade Delve with the same command.
- Enable developer mode once: `sudo DevToolsSecurity -enable`.
- Debugging works for `go` run configurations and Go tests only.

## Profiling a configuration opens the configuration editor

The configuration has no `profileURL`. Add your program's `net/http/pprof` endpoint, such as `http://localhost:6060/debug/pprof/profile?seconds=10` (localhost only). Profiling tests needs no setup.

## Pull Requests view is empty or shows a hint

- Install the GitHub CLI: `brew install gh`.
- Sign in: `gh auth login`.
- Make sure the repository has a GitHub remote.

## Remote images don't show in Markdown

That's intentional. Click **Load remote images** in the preview.

## Where the logs are

Flint's log is `~/Library/Logs/Flint/flint.log`. **Help ▸ Show Logs in Finder** opens it. Secrets and your home path are redacted in it.
