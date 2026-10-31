# Installation

This page covers downloading Flint, opening it for the first time, the `flint` command, the optional tools, updating and uninstalling.

## Requirements

- macOS 12 (Monterey) or later.
- An Apple silicon or Intel Mac. Release builds are universal, so the same download runs on both.

## Download

1. Open the [Flint releases page](https://github.com/ODudek/flint/releases).
2. Download the `.dmg` or the `.zip` of the latest release, together with `SHA256SUMS.txt`.
3. Open the `.dmg` (or unzip the `.zip`) and drag Flint to your Applications folder.

### Verify the checksum

Every release comes with SHA-256 checksums. To check your download, put it in the same folder as `SHA256SUMS.txt` and run:

```bash
cd ~/Downloads
shasum -a 256 -c SHA256SUMS.txt
```

The file you downloaded should be reported as `OK`. A line for the file you did not download (for example the `.zip` when you took the `.dmg`) reports that it is missing; you can ignore it.

## First launch

Release builds are notarized only when the project's signing keys are configured; otherwise they are ad-hoc signed and macOS may refuse to open the app the first time. If you see a warning that Flint cannot be opened:

1. In Finder, open Applications, right-click Flint and choose **Open**.
2. Confirm with **Open** in the dialog.

Alternatively, try to open Flint once, then go to **System Settings ▸ Privacy & Security**, scroll to the message about Flint and click **Open Anyway**.

You only need to do this once per installed version.

## The flint command

The `flint` command opens a folder in Flint from your terminal:

```bash
flint .              # open the current directory
flint ~/src/my-app   # open another folder
```

If Flint is already running, it takes the folder over and comes to the front instead of starting a second copy. Relative paths are resolved from the directory you run the command in.

`make install` (when you build from source) installs the command as `~/.local/bin/flint`. If you installed Flint from a release, you can install the same small script by hand: save [scripts/flint](https://github.com/ODudek/flint/blob/main/scripts/flint) as `~/.local/bin/flint` and make it executable with `chmod +x ~/.local/bin/flint`. It expects Flint in `/Applications` or `~/Applications`.

> **Tip:** Make sure `~/.local/bin` is on your `PATH`.

## Recommended tools

Flint launches the tools already installed on your Mac, with your login shell's `PATH`, so sign-ins and configuration work the same as in your terminal. Flint starts without any of them, but Git is the one you will almost certainly want; each of the others unlocks the features that need it.

### Check which tools you have

On the first launch Flint opens the **Tools** window. It lists every tool below with the version it found, or "Not installed", the feature it enables, and for a missing tool an install command with a **Copy** button. Paste the command into Terminal, then click **Recheck**. Open the window again at any time with **Help ▸ Check Tools…**, "Check Tools…" in the command palette (`⇧⌘P`), or **Check installed tools…** on the Welcome screen.

The check only runs each tool's version command, outside your projects, and installs nothing. A tool marked "Not working" is on your `PATH` but its version command failed; the window shows the error (for example, a `rust-analyzer` that rustup knows about but hasn't installed).

| Tool | What it unlocks |
|---|---|
| `ripgrep` (`brew install ripgrep`) | Fast project search, the TODO list and `⌘P`. Without it, a slower built-in search is used. |
| `git` (`xcode-select --install`) | The Commit panel, diffs, blame, history, branches, stash and worktrees. |
| `gh` (GitHub CLI, `brew install gh`) | Pull request review. Sign in with `gh auth login`. |
| `claude` (`brew install --cask claude-code`) and/or `codex` (`brew install --cask codex`) | Agent chat, generated commit messages and conflict resolution. |
| `gopls` | Go language features (`go install golang.org/x/tools/gopls@latest`). |
| `rust-analyzer` | Rust language features (`rustup component add rust-analyzer`). |
| `vtsls` or `typescript-language-server` | TypeScript and JavaScript language features (`npm i -g @vtsls/language-server`, or `npm i -g typescript-language-server typescript`). |
| `basedpyright` or `pyright` | Python language features (`pipx install basedpyright`, or `npm i -g pyright`). |
| `dlv` (Delve) | The Go debugger (`go install github.com/go-delve/delve/cmd/dlv@latest`). |
| `kubectl` (`brew install kubectl`) | The Kubernetes panel. |
| `docker` | The Docker / Compose panel (Docker Desktop, Colima or OrbStack; `brew install --cask docker-desktop` installs Docker Desktop). |
| `aws` (`brew install awscli`), and `saml2aws` if you use it | The AWS profiles panel and credential refresh. |
| `node` (`brew install node`) | Only needed for the language servers installed with `npm`. |

> **Note:** The Go debugger also needs macOS developer mode: run `sudo DevToolsSecurity -enable` once.

## Updating

Flint doesn't update itself. To update, download the latest release, quit Flint and replace the app in Applications. Your settings, sessions and connections are kept, because they live outside the app (see [Files and data](reference/files-and-data.md)).

## Uninstalling

1. Quit Flint and move it from Applications to the Trash.
2. If you installed the command, delete `~/.local/bin/flint`.
3. To remove your data as well, delete:
   - `~/Library/Application Support/flint` (settings, recent folders, sessions, scratchpads, database connections and query history);
   - `~/Library/Logs/Flint` (the log file).
4. Database passwords are stored in the macOS Keychain. Delete each connection in Flint before uninstalling to remove its password, or remove the `flint-db` items in Keychain Access.

Project files such as `.flint/run.json` and `.http` environment files stay in your repositories. Agent conversation histories belong to the agent CLIs (`~/.claude`, `~/.codex`) and are not removed.

## Building from source

You need Go, Node, the Xcode Command Line Tools and the Wails v3 CLI. Then:

```bash
git clone https://github.com/ODudek/flint.git
cd flint
cd frontend && npm ci --ignore-scripts && cd ..
make install
```

`make install` builds the app, copies it to `/Applications` and installs the `flint` command in `~/.local/bin`. Exact versions and other build targets are described in [DEVELOPMENT.md](https://github.com/ODudek/flint/blob/main/docs/DEVELOPMENT.md).

## Next steps

Take the [10-minute tour](first-steps.md).
