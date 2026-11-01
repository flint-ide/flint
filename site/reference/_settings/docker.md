| `docker` | object | see below | How the Docker panel finds the engine and how much log it starts with. See [docker](#docker). |

### docker

Used by the [Docker panel](../guides/docker.md).

| Key | Type | Range | Default | Meaning |
|---|---|---|---|---|
| `host` | string | | `""` | Where the Docker Engine is: `unix:///path/docker.sock`, `tcp://host:2376` or `https://host:2376`. Empty finds it like the docker CLI: `DOCKER_HOST`, then the current docker context, then the default sockets (Docker Desktop, Colima, OrbStack, Rancher Desktop, `/var/run/docker.sock`). With `DOCKER_TLS_VERIFY` set, `tcp://` uses TLS with the certificates in `DOCKER_CERT_PATH` (default `~/.docker`). Plain `tcp://` without TLS works but Flint warns in the panel. `ssh://` hosts are not supported. |
| `logTailLines` | integer | 1–10000 | `500` | Past lines a container's log tab starts with before it follows new output. |

Response bodies from the engine are capped at 32 MiB; this limit is not a setting.
