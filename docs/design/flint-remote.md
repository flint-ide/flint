# Design: Flint Remote

Status: proposal · 2026-11-01

Use Flint from a phone while it keeps running on the laptop: follow the agents, answer their questions, approve what they ask for, look at the diff and the tests, start or merge an agent task. The phone is a remote control for the Flint that already runs; it doesn't run agents, git or terminals itself.

## Why

An agent works for minutes at a time and then waits: for an approval ("run `go test`?"), an answer, or a review. Today that means staying at the laptop. With Flint Remote the wait happens on the phone: a notification says the agent needs you, you read what it wants and answer, and it carries on.

## Goals

- Watch agent chats live, including tool calls, diffs and plans.
- Answer permission requests and reply to the agent (the same approval policy as on the laptop).
- Agent tasks: list with state, diff stat, Show diff (read-only), Create PR, Merge.
- Last test results and failing tests; "Ask the agent" from them.
- A push notification when an agent waits for you, a turn ends, a task fails or tests fail.
- Works over the user's own network path (Tailscale, WireGuard, SSH tunnel); Flint runs no servers of its own.

## Non-goals (v1)

- Editing files, terminals, databases, Kubernetes actions from the phone. Terminals stay off; they are a remote shell (see Security).
- Running anything on the phone: iOS can't start the CLIs Flint drives.
- Reaching a sleeping laptop. Flint Remote works while the laptop is awake (on power, or with `caffeinate`); the docs say so.
- Several users or sharing with others: one owner, their own devices.

## Architecture

```
iPhone                                   Laptop (Flint, running)
┌────────────────────────┐   HTTPS +     ┌──────────────────────────────────────┐
│ Client UI (React)      │   WebSocket   │ internal/remote: HTTP server on      │
│  - PWA in Safari, or   │ ────────────▶ │ 127.0.0.1 (opt-in), token auth       │
│  - Wails iOS app       │  over Tailscale│  ├─ REST: chats, tasks, tests, diff  │
└────────────────────────┘  / SSH tunnel │  ├─ WS: event stream with replay     │
                                         │  └─ Web Push sender (VAPID)          │
                                         │ calls the same Go APIs as the UI     │
                                         │ (AgentChatAPI, TaskAPI, TestAPI …)   │
                                         └──────────────────────────────────────┘
```

- **One remote API, two clients.** The laptop side doesn't care which client connects. The client code is shared React in `frontend/src/remote/` (mobile layouts reusing the chat, diff and task components). The PWA is served by Flint itself; the iOS app bundles the same code.
- **Not the Wails bindings.** Server mode (`-tags server`) already exposes every binding over HTTP, but it is a dev tool without authentication, and all bindings include terminals and file writes. Flint Remote is a separate, narrow API (`internal/remote`), an allow-list of operations that call the existing Go APIs. Each operation is checked against the paired device's scope.
- **Same rules as the desktop.** Remote actions go through the same approval policy, Restricted Mode and redaction. A permission request answered on the phone is settled on the laptop too (`KindPermissionSettled` already exists), and vice versa.

### Event stream and replay

The frontend keeps chat history today; Go only streams events. A phone that connects mid-conversation needs the past:

- `internal/agentsession` gets a bounded per-session event log (by bytes and count, oldest dropped; events already carry `Seq`).
- The WebSocket takes `since` (last `Seq` per session) and replays from the log, then streams live. A gap beyond the log is reported, and the client shows "earlier messages are on the laptop".
- Other streams the client needs: `tasks:changed`, test runs finished, update of a chat's state. They come through one fan-out next to `App.emit`, so desktop and remote see the same events.

### API sketch (v1)

| Method | Path | Does |
|---|---|---|
| GET | `/api/chats` | Open chats: agent, project, state, waiting for approval |
| GET | `/api/stream?since=…` | WebSocket: replay + live events (chats, tasks, tests) |
| POST | `/api/chats/{id}/prompt` | Send a message |
| POST | `/api/chats/{id}/permissions/{req}` | Answer a permission request (`optionId`) |
| POST | `/api/chats/{id}/cancel` | Stop the turn |
| GET | `/api/tasks` | Agent tasks with state and diff stat |
| GET | `/api/tasks/{id}/diff` | Changed files and per-file diff (read-only, redacted) |
| POST | `/api/tasks/{id}/pr` · `/merge` | Create PR · Merge (both confirm on the phone) |
| POST | `/api/tasks` | New Agent Task (scope `tasks:create`) |
| GET | `/api/tests/last` | Last runs and failures |
| POST | `/api/push/subscribe` | Register a Web Push subscription |

JSON, capped bodies, per-device rate limits. No file write, terminal, shell, database or cluster operations in v1.

## Security

Remote access to Flint is remote access to the laptop: agents run commands, and an approval from the phone is an approval. So:

- **Off by default.** Settings ▸ Remote: on/off, and the list of paired devices.
- **Loopback only.** The server listens on `127.0.0.1`. The way in is the user's own tunnel: `tailscale serve` (HTTPS with a real certificate, only their tailnet), WireGuard, or `ssh -L`. Flint never opens a public port and runs no relay. Binding to a tailnet interface directly can be an advanced option later.
- **Pairing.**
  - "Pair a device" shows a QR code holding the address and a one-time pairing secret. It is valid for 2 minutes, for one device.
  - The device exchanges it for a long random device token: stored in the iOS Keychain (app), or in the PWA's storage (PWA).
  - On the laptop, only a hash of the token is kept, in the Keychain.
  - Tokens can be revoked one by one; revoking all is one click.
- **Scopes per device:** `read` (default), `respond` (answer permissions, prompt, cancel), `tasks` (create, PR, merge). The pairing screen asks which.
- **Each request:** token check (constant time), Host and Origin checks (DNS rebinding), body caps, rate limit, and an audit line in Flint's log (device, operation, never content).
- **Content:** what leaves the laptop is redacted with `internal/redact`, like everything sent to an agent. Diffs of secret files (`redact.SecretFile`) are refused.
- **Trust:** chats in untrusted folders are listed but read-only from the phone.

## Notifications

- **PWA:** Web Push works on iOS 16.4+ for web apps added to the home screen.
  - Flint signs the messages itself (VAPID, ECDSA P-256, standard library) and encrypts them (RFC 8291, `aes128gcm`, standard library).
  - Flint sends them straight to Apple's push service for that subscription. No Flint server is involved.
  - The payload is minimal ("Claude is waiting for you in flint"); the content is fetched over the tunnel when the user opens it.
- **iOS app:** APNs needs a provider that holds the app's signing key.
  - An open-source app can't ship that key inside every laptop's Flint.
  - So the native app either uses its own push relay, which conflicts with "no servers of our own", or only notifies while it is running, which is weak.
  - This is the main reason to start with the PWA.

## Client: PWA first, Wails iOS app second

| | PWA (Safari, home screen) | Wails iOS app |
|---|---|---|
| Effort | Serve `frontend/dist-remote` from `internal/remote` | Wails iOS build (`build_assets/ios`, ios-poc example), Xcode project, signing |
| Distribution | A URL, no review, no developer account | Apple Developer Program ($99/year), TestFlight / App Store review |
| Push | Web Push, no relay | APNs needs a relay, or local notifications only |
| Native features | Camera for QR in Safari, Share sheet | Keychain, Face ID before approvals, haptics, background refresh |
| Code shared | The same React code | Same React plus a thin Go client (on iOS the Go side only talks to the laptop) |

Plan: build the PWA in v1, with the client code written so it can also run inside a Wails iOS shell. Revisit the native app once the PWA is used and push works. Its main gains would be Face ID before approving a command, and App Store presence.

## Phases

1. **Remote API, read-only** (≈ 1 week)
   - `internal/remote`: loopback server, pairing, device tokens in the Keychain, scopes, audit log.
   - Bounded event log in `agentsession`.
   - The `/api/chats`, `/api/stream` and `/api/tasks` endpoints.
   - Settings ▸ Remote.
   - Done when: a paired browser on another machine (via `ssh -L`) follows a live chat with replay. An unpaired request gets 401, and a request with a foreign Host or Origin gets 403.
2. **Mobile client and respond** (≈ 1 week)
   - `frontend/src/remote/`: chat list and chat view, the permission prompt, a mobile layout, installable as a PWA.
   - Prompt, cancel and permission answers.
   - Done when: on an iPhone over Tailscale, approve a command the agent asked for, and see it settled on the laptop.
3. **Notifications** (≈ 3–4 days)
   - Web Push (VAPID + RFC 8291) for: agent waiting, turn finished, task failed, tests failed. A per-device setting for each.
   - Done when: the notification arrives with the PWA closed, and opening it shows the chat.
4. **Tasks and tests** (≈ 1 week)
   - Task list, read-only diff, Create PR, Merge, New Agent Task (with scopes).
   - Last test results with "Ask the agent".
5. **Optional: Wails iOS shell.** The same client in a native app, adding the Keychain, Face ID before approvals and QR scanning. Decide after phases 1–4.

Each phase: tests first (Go, httptest, frontend), an independent security review before merge, and docs (a guide "Use Flint from your phone" plus the security model in ARCHITECTURE.md).

## Risks and open questions

- **The laptop sleeps.** It is the main limit, so the docs and the Settings section say it plainly. The phone shows "Flint unreachable" with the last known state.
- **Big diffs and long chats on a phone.** Read-only, paged, and collapsed by default.
- **Tailscale as the recommended path.** It is a third-party service, but the user's own choice and not a dependency of Flint. SSH tunnelling is always documented next to it.
- **Overlap with Claude Code Remote Control.** That covers Claude Code sessions from the Claude app. Flint Remote covers every agent Flint runs (Claude Code, Codex, ACP agents), agent tasks across worktrees, diffs and tests in one place.
- **Open questions:**
  - Should approving a shell command from the phone ever require an extra step (Face ID in the app, or re-entering a PIN in the PWA)?
  - Should the phone see chats of every open project, or only those the user marks as remote?
