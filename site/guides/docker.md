# Docker

Docker is an extension now, not part of Flint itself. It adds a Docker panel with your Compose projects and containers, their status and ports, and the usual actions on them: logs, a shell inside a container, start, stop, restart and remove, and Compose up, down and pull. Logs and shells open as terminal tabs that run the `docker` CLI (`docker logs -f`, `docker exec -it`), so they behave like any other terminal.

## Install it

1. Get the [flint-ide/extensions](https://github.com/flint-ide/extensions) repository and build its `docker/` folder as the README there describes (`go build -o docker-ext .` in that folder).
2. Run **Install Extension from Folder…** from the command palette and pick the `docker/` folder. See [Install an extension](extensions.md#install-an-extension).

The extension needs the `docker` CLI and a running engine (Docker Desktop, Colima, OrbStack or a plain daemon). It gets `DOCKER_HOST` and the related variables of your environment because its manifest names them (`run.env`); the install dialog lists them.

## Settings

**Settings ▸ Docker** is the extension's own page. Its values are stored in `settings.json` under `extensions.flint.docker` (see [extensions](../reference/settings.md#extensions)). The top-level `docker` key of the old built-in panel is ignored.

## Related

- [Use Docker](../how-to/use-docker.md)
- [Extensions](extensions.md)
