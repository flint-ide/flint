# Docker

The Docker panel shows your Compose projects and containers, with their status and ports, and gives you logs, shells and lifecycle actions without leaving Flint. It uses the `docker` CLI, so it works with Docker Desktop, Colima and OrbStack.

Open it with the `⛴` icon on the activity bar or "Docker" in the command palette.

## What the panel shows

- **Compose projects** found from the labels of running containers and from `compose.yaml` / `docker-compose.yml` files in your project folders (and one level below), each with its services.
- **Other containers** that don't belong to a Compose project.

Each container shows its status (running, unhealthy, paused or stopped) and its published ports. Click a port to open it in your browser.

The panel follows `docker events` while it is visible, so changes appear as they happen. The ↻ button refreshes it by hand.

## Container actions

Double-click a running container to open its logs. Right-click a container for:

| Action | What it does |
|---|---|
| Logs | Follows the logs (`docker logs -f`) in a terminal tab |
| Open Shell | Opens `docker exec -it` with bash, or sh when bash isn't available, in a terminal tab |
| Start / Stop / Restart | Changes the container's state |
| Remove… | Removes the container, after a confirmation |
| Copy Container ID | Copies the ID |

Running containers also have Logs and Shell (`❯_`) buttons on their row.

## Project actions

Right-click a Compose project for:

| Action | What it does |
|---|---|
| Up | Starts the project (`docker compose up -d`) |
| Restart | Restarts its services |
| Stop | Stops its services |
| Down… | Stops and removes the project's containers, after a confirmation |
| Pull Images | Pulls the latest images |

Flint validates container names before passing them to a terminal.

## Related

- [Use Docker](../how-to/use-docker.md)
