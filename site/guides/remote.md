# Flint Remote (preview)

> **Early preview.** This first version is read-only and has no phone app yet: it is the part of Flint Remote that runs on your Mac. The iPhone and Android app that uses it comes next. Until then you can try it with the small command-line client described below.

Flint Remote lets you follow Flint from your phone while it keeps running on your Mac: which agent chats are open, what they are doing and whether one is waiting for your approval, and how your agent tasks stand. Your phone is a window onto the Flint that already runs; it doesn't run agents, git or terminals itself.

## How it works

- Flint serves a small API on `127.0.0.1` (port 7419 by default) while **Settings ▸ Remote ▸ Allow paired devices** is on. It is off by default, and nothing listens while it is off.
- Flint never opens a public port and runs no server or relay of its own. Your phone reaches the Mac through **your own tunnel**: Tailscale, WireGuard or SSH.
- Only **paired** devices get in. Pairing uses a one-time code that is valid for two minutes and for one device.
- What a device sees is **redacted** like everything Flint sends to an agent: credentials are removed and your home folder is shown as `~`. Diffs of files that hold credentials (`.env`, keys) are not shown.
- **The Mac must be awake.** Flint Remote works while the Mac is on and awake (on power, or with `caffeinate`); a sleeping Mac can't answer.

## Switch it on

1. Open **Settings ▸ Remote** and turn on **Allow paired devices**.
2. Set up a tunnel to `127.0.0.1:7419` (see below).
3. Click **Pair a Device…**. Flint shows a QR code, the address and a code like `ABCD-EFGH-IJKL-MNOP`. The phone app will scan the QR code; you can also type the address and code.

The QR code holds a link of the form `flint-remote://pair?url=<address>&secret=<code>`.

## Reach your Mac

### Tailscale

On the Mac:

```bash
tailscale serve --bg 7419
```

This serves Flint Remote over HTTPS, with a real certificate, to your tailnet only. Add the name Tailscale gives the Mac (for example `laptop.tail1234.ts.net`) under **Extra allowed hosts**, then choose it next to **Pair a Device…** so the QR code holds `https://laptop.tail1234.ts.net`.

### SSH

From the other machine:

```bash
ssh -N -L 7419:127.0.0.1:7419 you@your-mac
```

Then use `http://127.0.0.1:7419` there. The local port may differ from 7419; Flint accepts `localhost` and `127.0.0.1` on any port. This needs Remote Login (SSH) on the Mac.

## What a device can do

In this preview a paired device can only **read**:

- the open agent chats: agent, project, state (idle, working, or waiting for your approval), and whether the folder is in [Restricted Mode](restricted-mode.md) (such chats will stay read-only from a phone);
- the agent tasks with their state and how many lines they changed;
- a live stream of the chats' events (messages, tool calls, plans, permission requests), starting with what happened before it connected.

It can't answer an agent, send a prompt, open a terminal, read or write files, or touch databases or clusters. Answering permission requests from the phone is planned for the app, with its own permission (scope) per device.

Flint keeps the latest events of each chat (by default 2000 events and 4 MB per chat) so a device that connects in the middle of a conversation sees what it missed. Older events stay on the Mac: the device is told that earlier messages are there.

## Paired devices

**Settings ▸ Remote ▸ Paired devices** lists each device with when it was paired and last seen. **Revoke** cuts a device off at once, including a stream it has open; **Revoke All** does it for every device.

Flint keeps only a hash of each device's token, in the macOS Keychain (service `flint-remote`); the list of devices is in `state.json`. Every request is written to Flint's log with the device, the operation and the result, never what was sent.

## Try it from the command line

Until the phone app exists, `remotecheck` in Flint's source tree does what the app will do:

```bash
# Pair with the code Flint shows (prints the token to export).
go run ./cmd/remotecheck -url http://127.0.0.1:7419 pair ABCD-EFGH-IJKL-MNOP
export FLINT_REMOTE_TOKEN=…

go run ./cmd/remotecheck -url http://127.0.0.1:7419 chats    # open chats
go run ./cmd/remotecheck -url http://127.0.0.1:7419 tasks    # agent tasks
go run ./cmd/remotecheck -url http://127.0.0.1:7419 follow   # replay, then live events, one JSON line each
```

`follow <chatId>:<seq>` continues after an event you already have. Or with `curl`:

```bash
curl -H "Authorization: Bearer $FLINT_REMOTE_TOKEN" http://127.0.0.1:7419/api/chats
```

## API (preview)

All requests need `Authorization: Bearer <token>`, except pairing.

| Method | Path | Does |
|---|---|---|
| POST | `/api/pair` | `{"secret": "…", "name": "My iPhone"}` → `{"token": "…", "device": {…}}`. The code works once, for two minutes; five wrong codes cancel it. |
| GET | `/api/chats` | `{"chats": [{id, agent, project, state, waitingForApproval, pending, readOnly, lastSeq}]}` |
| GET | `/api/tasks` | `{"tasks": [{id, title, agent, branch, base, state, ahead, merged, pullRequest, changes: {files, added, deleted}}]}` |
| GET | `/api/stream?since=<chatId>:<seq>,…` | WebSocket. JSON messages: `hello` (the open chats), `gap` (events no longer kept), `agent` (an event), `replayed` (what follows is live), `chat` and `chatClosed` (state changes), `tasks` (the task list changed). |

Answers: `401` without a valid token or pairing code, `403` when the request is addressed to a host that isn't allowed (protection against DNS rebinding), `429` past the device's rate, `413` for a body over 4 KB. A stream that falls behind is closed with code 1013; reconnect with `since`. A revoked device's stream is closed with 1008.

## Related

- [Settings file: remote](../reference/settings.md#remote)
- [Privacy and security](../reference/privacy-and-security.md)
