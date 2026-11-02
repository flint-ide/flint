# Use Docker

Recipes for the Docker panel, which comes with the Docker extension. To install it, see [Docker](../guides/docker.md).

## Open the Docker panel

1. Click `⛴` on the activity bar, or run "Docker" from the palette.

You need a running Docker engine (Docker Desktop, Colima, OrbStack or a plain daemon). The `docker` CLI is optional: only Compose **Up**, **Down** and **Pull Images** need it. If Flint doesn't find your engine, set Settings ▸ Docker ▸ Host (for example `unix:///home/me/.colima/default/docker.sock`).

## Follow a container's logs

1. Double-click a running container, or click its Logs button.

The logs follow in a terminal tab, starting with the container's last 500 lines (Settings ▸ Docker).

## Open a shell in a container

1. Click `❯_` on a running container, or right-click it and choose **Open Shell**.

Flint opens a shell inside the container in a terminal tab, with bash, or sh if bash isn't available. No CLI is needed.

## Open a published port in the browser

1. Click the port shown on the container's row.

## Start, stop or restart a container

1. Right-click the container.
2. Choose **Start**, **Stop** or **Restart**.

## Remove a container

1. Right-click the container and choose **Remove…**.
2. Confirm.

## Bring a Compose project up or down

1. Find the project in the panel. Flint lists projects from running containers and from `compose.yaml` / `docker-compose.yml` files in your project folders.
2. Right-click it, or use the buttons on its row:
   - **Up** runs `docker compose up -d` (needs the `docker` CLI; the button is disabled without it);
   - **Down…** stops and removes the project's containers, after a confirmation;
   - **Restart** and **Stop** do what they say and work without the CLI; **Pull Images** needs it.

## Copy a container ID

1. Right-click the container and choose **Copy Container ID**.
