# Use Docker

Recipes for the Docker panel. For what it shows, see [Docker](../guides/docker.md).

## Open the Docker panel

1. Click `⛴` on the activity bar, or run "Docker" from the palette.

You need the `docker` CLI with a running engine (Docker Desktop, Colima or OrbStack).

## Follow a container's logs

1. Double-click a running container, or click its Logs button.

The logs follow in a terminal tab (`docker logs -f`).

## Open a shell in a container

1. Click `❯_` on a running container, or right-click it and choose **Open Shell**.

Flint opens `docker exec -it` with bash, or sh if bash isn't available.

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
   - **Up** runs `docker compose up -d`;
   - **Down…** stops and removes the project's containers, after a confirmation;
   - **Restart**, **Stop** and **Pull Images** do what they say.

## Copy a container ID

1. Right-click the container and choose **Copy Container ID**.
