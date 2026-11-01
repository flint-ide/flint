# A Linux environment for building and testing Flint from any host (see
# docs/DEVELOPMENT.md, "Building on Linux"): Go, the GTK 4 / WebKitGTK 6
# headers Wails v3 links against, Node 22 for the frontend, wails3, and Xvfb
# with xwd/ImageMagick to run the window headless and screenshot it.
#
#   docker build -t flint-linux build/linux
#   docker run --rm -v "$PWD:/src" -w /src flint-linux go test ./...
#
# node_modules from a macOS checkout holds macOS binaries (the TypeScript
# compiler), so mount a volume over frontend/node_modules and run
# `npm ci --ignore-scripts` in the container before building the frontend.
#
# The base images are pinned by digest and wails3 by version; Dependabot doesn't
# track them, so bump them together with go.mod and the CI's Node version.
FROM node:22-trixie-slim@sha256:2117f713a417f3aff682ac8f5bc2ebbdf1beed51f36e67ab1e0fd3866b6d2d8f AS node

FROM golang:1.27.3-trixie@sha256:c65ecfe613b539ac74c62a4c3b2c27e8f8a6076384c8f27e8a3c0f3be17cc385

RUN apt-get update \
	&& apt-get install -y --no-install-recommends \
		build-essential pkg-config \
		libgtk-4-dev libwebkitgtk-6.0-dev libx11-dev \
		xvfb x11-apps imagemagick dbus-x11 \
		ripgrep \
	&& rm -rf /var/lib/apt/lists/*

COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/npm
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm

ENV GOPROXY=https://proxy.golang.org,direct \
	CGO_ENABLED=1 \
	GOFLAGS=-buildvcs=false

RUN go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0 && rm -rf /root/.cache/go-build /go/pkg/mod
