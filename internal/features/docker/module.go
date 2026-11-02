// Package docker is the Docker feature: the Docker panel's backend (containers
// and Compose projects through the Engine API, internal/docker), its
// "docker" settings and the docker CLI in the tool check. The frontend half
// is frontend/src/features/docker.
package docker

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"flint/internal/feature"
	"flint/internal/settings"
	"flint/internal/tools"
)

// Module declares the feature (listed in package main's features.go).
var Module = feature.Module{
	ID:       "docker",
	Optional: true,
	Settings: []settings.AnySection{Section},
	Tools: []tools.Tool{
		tools.Tool{
			ID: "docker", Name: "Docker", Group: tools.GroupInfra, Order: 10,
			Feature: "Compose up, down and pull (the Docker panel itself talks to the Engine API)",
			Install: tools.Install("brew install --cask docker-desktop", "sudo apt install docker.io"), Docs: "guides/docker.md",
		}.Find("docker", "--version"),
	},
	New: func(h feature.Host) feature.Instance {
		api := &API{host: h}
		return feature.Instance{Services: []application.Service{application.NewService(api)}, Close: api.close}
	},
}
