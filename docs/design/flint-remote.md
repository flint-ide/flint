# Design: Flint Remote

Status: phase 1 (laptop-side API, read-only) implemented · client: native Wails v3 apps for iOS and Android (decided 2026-11-01) · 2026-11-01

Use Flint from a phone while it keeps running on the laptop: follow the agents, answer their questions, approve what they ask for, look at the diff and the tests, start or merge an agent task. The phone is a remote control for the Flint that already runs; it doesn't run agents, git or terminals itself.

## Why

An agent works for minutes at a time and then waits: for an approval ("run `go test`?"), an answer, or a review. Today that means staying at the laptop. With Flint Remote the wait happens on the phone: the app shows that the agent needs you, you read what it wants and answer, and it carries on.

## Goals

- Watch agent chats live, including tool calls, diffs and plans.
- Answer permission requests and reply to the agent (the same approval policy as on the laptop).
- Agent tasks: list with state, diff stat, Show diff (read-only), Create PR, Merge.
- Last test results and failing tests; "Ask the agent" from them.
- Notifications when an agent waits for you, a turn ends, a task fails or tests fail (while the app is open first; real push is an open question, see Notifications).
- Works over the user's own network path (Tailscale, WireGuard, SSH tunnel); Flint runs no servers of its own.

## Non-goals (v1)

- Editing files, terminals, databases, Kubernetes actions from the phone. Terminals stay off; they are a remote shell (see Security).
- Running anything on the phone: neither iOS nor Android can start the CLIs Flint drives.
- Reaching a sleeping laptop. Flint Remote works while the laptop is awake (on power, or with `caffeinate`); the docs say so.
- Several users or sharing with others: one owner, their own devices.

## Architecture

```
Phone (iOS / Android)                    Laptop (Flint, running)
┌────────────────────────┐   HTTP(S) +   ┌──────────────────────────────────────┐
│ Wails v3 mobile app    │   WebSocket   │ internal/remote: HTTP server on      │
│  - React client        │ ────────────▶ │ 127.0.0.1 (opt-in), token auth       │
│  - thin Go client      │ over Tailscale│  ├─ REST: chats, tasks, (tests, diff)│
└────────────────────────┘  / SSH tunnel │  └─ WS: event stream with replay     │
                                         │ calls the same Go APIs as the UI     │
                                         │ (AgentChatAPI, TaskAPI, TestAPI …)   │
                                         └──────────────────────────────────────┘
```

- **One remote API, native clients.** The phone app is a Wails v3 app for iOS and Android (v3.0.0 builds both: `pkg/application/*_ios.go` / `*_android.go`, the `build_assets/ios` and `build_assets/android` Taskfiles, `examples/ios-poc`). Its React client code lives in `frontend/src/remote/` (mobile layouts reusing the chat, diff and task components); its Go side is thin: on the phone, Go only talks to the laptop (`internal/remote/client` is the start of it), since no CLI can run there.
- **Not the Wails bindings.** Server mode (`-tags server`) already exposes every binding over HTTP, but it is a dev tool without authentication, and all bindings include terminals and file writes. Flint Remote is a separate, narrow API (`internal/remote`), an allow-list of operations that call the existing Go APIs. Each operation is checked against the paired device's scope.
- **Same rules as the desktop.** Remote actions go through the same approval policy, Restricted Mode and redaction. A permission request answered on the phone is settled on the laptop too (`KindPermissionSettled` already exists), and vice versa.

### Event stream and replay

The frontend keeps chat history today; Go only streams events. A phone that connects mid-conversation needs the past:

- `internal/agentsession` has a bounded per-session event log (by bytes and count, oldest dropped; events carry `Seq`). It is kept only while Flint Remote is on.
- The WebSocket takes `since` (last `Seq` per session) and replays from the log, then streams live. A gap beyond the log is reported, and the client shows "earlier messages are on the laptop".
- Other streams the client needs: `tasks:changed`, test runs finished, a chat's state. They come through one fan-out in `App.emit`, so desktop and remote see the same events.
- WebSocket rather than Server-Sent Events: the native client needs bidirectional messages from phase 2 on. The server side is RFC 6455 written with the standard library.

### API sketch (v1)

| Method | Path | Does | Phase |
|---|---|---|---|
| POST | `/api/pair` | Exchange a pairing secret for a device token | 1 ✓ |
| GET | `/api/chats` | Open chats: agent, project, state, waiting for approval, read-only | 1 ✓ |
| GET | `/api/stream?since=…` | WebSocket: replay + live events (chats, tasks, tests) | 1 ✓ |
| GET | `/api/tasks` | Agent tasks with state and diff stat | 1 ✓ |
| POST | `/api/chats/{id}/prompt` | Send a message | 2 |
| POST | `/api/chats/{id}/permissions/{req}` | Answer a permission request (`optionId`) | 2 |
| POST | `/api/chats/{id}/cancel` | Stop the turn | 2 |
| GET | `/api/tasks/{id}/diff` | Changed files and per-file diff (read-only, redacted) | 4 |
| POST | `/api/tasks/{id}/pr` · `/merge` | Create PR · Merge (both confirm on the phone) | 4 |
| POST | `/api/tasks` | New Agent Task (scope `tasks`) | 4 |
| GET | `/api/tests/last` | Last runs and failures | 4 |

JSON, capped bodies, per-device rate limits. No file write, terminal, shell, database or cluster operations in v1. The phase 1 endpoints are documented in `site/guides/remote.md`; `cmd/remotecheck` exercises them.

## Security

Remote access to Flint is remote access to the laptop: agents run commands, and an approval from the phone is an approval. So:

- **Off by default.** Settings ▸ Remote: on/off, the port, extra allowed hosts, and the list of paired devices.
- **Loopback only.** The server listens on `127.0.0.1`. The way in is the user's own tunnel: `tailscale serve` (HTTPS with a real certificate, only their tailnet), WireGuard, or `ssh -L`. Flint never opens a public port and runs no relay. Binding to a tailnet interface directly can be an advanced option later.
- **Pairing.**
  - "Pair a device" shows a QR code holding `flint-remote://pair?url=<address>&secret=<secret>` (a custom scheme the app handles; it scans the code with the camera), plus the address and secret for typing. The secret is valid for 2 minutes, for one device; five wrong tries cancel it.
  - The device exchanges it for a long random device token, kept in the iOS Keychain or the Android Keystore.
  - On the laptop, only a hash of the token is kept, in the Keychain.
  - Tokens can be revoked one by one; revoking all is one click. Revoking closes the device's open stream at once.
- **Scopes per device:** `read` (default), `respond` (answer permissions, prompt, cancel), `tasks` (create, PR, merge). The pairing screen asks which (phase 2; phase 1 grants only `read`).
- **Each request:** token check (constant time), Host check (DNS rebinding), body caps, rate limits (per device, bad tokens, pairing), and an audit line in Flint's log (device, operation, never content).
- **No browsers, ever.** Requests with an `Origin` or `Sec-Fetch-*` header are refused, because any web page can make a browser call a loopback port. So the phone apps (phase 2) call the API from their Go side, never from the WebView's JavaScript, and the API never adds CORS headers or accepts a token from a cookie or the query string. The Go client refuses plain http to anything but a loopback address.
- **Content:** what leaves the laptop is redacted with `internal/redact`, like everything sent to an agent. Tool calls that touch a credentials file (`redact.SecretFile`) have their input, output and diffs withheld.
- **Trust:** chats in untrusted folders are listed but read-only from the phone.

The threat model and the phase 1 details are in [ARCHITECTURE.md](../ARCHITECTURE.md#flint-remote).

## Notifications

- APNs (iOS) and FCM (Android) need provider credentials: an APNs key tied to the app's team, a Firebase project. An open-source laptop app can't ship those inside every Flint.
- **First:** while the app is open (or briefly in the background, as each OS allows), it receives live events over the WebSocket and shows local notifications: agent waiting, turn finished, task failed, tests failed.
- **Later:** real push with the app closed. The options are open questions below.

## Client: iOS and Android

Both apps are the same Wails v3 project: the shared React client and the thin Go client, built for each platform.

| | iOS | Android |
|---|---|---|
| Toolchain | Xcode, `build_assets/ios` Taskfile, iOS simulator for development | Android SDK and NDK, `build_assets/android` Taskfile, emulator for development |
| Signing and distribution | Apple Developer Program ($99/year), TestFlight, App Store review; free provisioning only for one's own device, renewed weekly | Play Console (one-time fee, review), or a signed APK the user sideloads |
| Device token | Keychain | Keystore (an encrypted value whose key lives in the Keystore) |
| Notifications | Local while running; APNs needs a key (see above) | Local while running; FCM needs a Firebase project, or UnifiedPush / ntfy |
| QR scanning | Camera (AVFoundation) through a small native bridge, or typed values | Camera through a small native bridge, or typed values |

## Phases

1. **Laptop API, read-only** (done)
   - `internal/remote`: loopback server, pairing with a QR code, device tokens hashed in the Keychain, scopes, rate limits, audit log.
   - Bounded event log in `agentsession`; one fan-out in `App.emit`.
   - The `/api/pair`, `/api/chats`, `/api/stream` (WebSocket) and `/api/tasks` endpoints.
   - Settings ▸ Remote; `cmd/remotecheck` as a command-line client.
   - Done when: a paired client over `ssh -L` follows a live chat with replay; an unpaired request gets 401, a foreign Host or Origin 403. Verified with a fake ACP agent against a server build of Flint, directly and through an `ssh -L` tunnel.
2. **Wails mobile app and respond** (≈ 1–2 weeks)
   - `frontend/src/remote/` and a mobile Wails project: pairing by QR, chat list and chat view, the permission prompt.
   - Prompt, cancel and permission answers on the laptop side (scope `respond`).
   - Done when: on the iOS simulator and the Android emulator, approve a command the agent asked for and see it settled on the laptop.
3. **Notifications** (≈ 3–4 days)
   - Local notifications from the live stream for: agent waiting, turn finished, task failed, tests failed. A per-device setting for each.
   - Then real push, depending on the open question below.
4. **Tasks and tests** (≈ 1 week)
   - Task list, read-only diff, Create PR, Merge, New Agent Task (scope `tasks`).
   - Last test results with "Ask the agent".

Each phase: tests first (Go, httptest, frontend), an independent security review before merge, and docs (the guide `site/guides/remote.md` plus the security model in ARCHITECTURE.md).

## Risks and open questions

- **The laptop sleeps.** It is the main limit, so the docs and the Settings section say it plainly. The phone shows "Flint unreachable" with the last known state.
- **Big diffs and long chats on a phone.** Read-only, paged, and collapsed by default; stream messages over 1 MiB are cut.
- **Tailscale as the recommended path.** It is a third-party service, but the user's own choice and not a dependency of Flint. SSH tunnelling is always documented next to it.
- **App store distribution.** Shipping through the App Store and Play needs developer accounts and reviews; personal builds (free provisioning, sideloaded APK) work without them but are less convenient.
- **Overlap with Claude Code Remote Control.** That covers Claude Code sessions from the Claude app. Flint Remote covers every agent Flint runs (Claude Code, Codex, ACP agents), agent tasks across worktrees, diffs and tests in one place.
- **Open questions:**
  - Push with the app closed: (a) a relay the user runs themselves, (b) the user's own APNs key or Firebase project for personal builds, (c) UnifiedPush / ntfy on Android. Each keeps "no servers of our own" in a different way.
  - Should approving a shell command from the phone ever require an extra step (Face ID / biometric prompt in the app)?
  - Should the phone see chats of every open project, or only those the user marks as remote?
