# Docker

The Docker panel shows your Compose projects and containers, with their status and ports, and gives you logs, shells and lifecycle actions without leaving Flint. It talks to the Docker Engine directly through its API, so it works with Docker Desktop, Colima, OrbStack and a plain Docker daemon, and **it doesn't need the `docker` CLI**. Only Compose `up`, `down` and `pull` need the CLI.

Open it with the `⛴` icon on the activity bar or "Docker" in the command palette.

## How Flint finds the engine

Like the docker CLI does:

1. **Settings ▸ Docker ▸ Host**, if you set one ([docker](../reference/settings.md#docker)).
2. `DOCKER_HOST` (`unix://` or `tcp://`; with `DOCKER_TLS_VERIFY` and `DOCKER_CERT_PATH` for TLS).
3. The current docker context (`docker context use …`).
4. The default sockets: `/var/run/docker.sock`, Docker Desktop's `~/.docker/run/docker.sock`, Colima, OrbStack and Rancher Desktop.

Flint talks to the newest API version both sides know. A plain `tcp://` host without TLS works, but the panel shows a warning, because anyone on the network could then control your containers. `ssh://` hosts are not supported.

## What the panel shows

- **Compose projects** found from the labels of running containers and from `compose.yaml` / `docker-compose.yml` files in your project folders (and one level below), each with its services.
- **Other containers** that don't belong to a Compose project.

Each container shows its status (running, unhealthy, paused or stopped) and its published ports. Click a port to open it in your browser.

The panel follows the engine's event stream while it is visible, so changes appear as they happen; if Docker restarts, it reconnects. The ↻ button refreshes it by hand.

## Container actions

Double-click a running container to open its logs. Right-click a container for:

| Action | What it does |
|---|---|
| Logs | Follows the container's log in a terminal tab, starting with its last lines (500 by default; see Settings ▸ Docker) |
| Open Shell | Opens a shell inside the container, bash or sh when bash isn't available, in a terminal tab. Resizing the tab resizes the shell. |
| Start / Stop / Restart | Changes the container's state |
| Remove… | Removes the container, after a confirmation |
| Copy Container ID | Copies the ID |

Running containers also have Logs and Shell (`❯_`) buttons on their row. Logs and shell tabs are not restored after a restart of Flint.

## Project actions

Right-click a Compose project for:

| Action | What it does | Needs the docker CLI |
|---|---|---|
| Up | Starts the project (`docker compose up -d`) | yes |
| Restart | Restarts its running services | no (uses the CLI's Compose when present) |
| Stop | Stops its running services | no (uses the CLI's Compose when present) |
| Down… | Stops and removes the project's containers, after a confirmation | yes |
| Pull Images | Pulls the latest images | yes |

Without the `docker` CLI, Up, Down and Pull are disabled and say why; install Docker Desktop, or `docker-ce-cli` and `docker-compose-plugin`, to enable them. Everything else keeps working.

Flint validates container names before it uses them in a request.

## Related

- [Use Docker](../how-to/use-docker.md)
- [Settings ▸ docker](../reference/settings.md#docker)
