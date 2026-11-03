# Docker

Docker is an extension now, not part of Flint itself. It adds a Docker panel with your Compose projects and containers, their status and ports, and the usual actions on them: logs, a shell inside a container, start, stop, restart and remove, and Compose up, down and pull. Logs and shells open as terminal tabs that run the `docker` CLI (`docker logs -f`, `docker exec -it`), so they behave like any other terminal.

## Install it

1. Open **Settings ▸ Extensions** and find the Docker extension under **Repository**.
2. Click **Install…**. Flint builds it with `go build` (Go must be installed) and shows its program and permissions. Click **Install**.

**Update…** appears there when the repository has a newer version. To install from a folder instead, build the `docker/` folder of [flint-ide/extensions](https://github.com/flint-ide/extensions) (`go build -o docker-ext .`) and run **Install Extension from Folder…**. See [Install an extension](extensions.md#install-an-extension).

The extension needs the `docker` CLI and a running engine (Docker Desktop, Colima, OrbStack or a plain daemon). It gets `DOCKER_HOST` and the related variables of your environment because its manifest names them (`run.env`); the install dialog lists them.

## Settings

**Settings ▸ Docker** is the extension's own page. Its values are stored in `settings.json` under `extensions.flint.docker` (see [extensions](../reference/settings.md#extensions)). The top-level `docker` key of the old built-in panel is ignored.

## Related

- [Use Docker](../how-to/use-docker.md)
- [Extensions](extensions.md)
