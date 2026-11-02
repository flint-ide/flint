// Package kubernetes is the Kubernetes feature: the Kubernetes panel's backend
// (contexts, namespaces, workloads and actions through kubectl, internal/kube)
// and kubectl in the tool check. The frontend half is
// frontend/src/features/kubernetes.
//
// Its agent tool, get_kubernetes_status, lives with Flint's other MCP tools
// (internal/mcptools) and is offered only while this feature runs: package main
// gives the MCP server a cluster reader only then (Active).
package kubernetes

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"flint/internal/feature"
	"flint/internal/tools"
)

// ID is the feature's id, also its key under settings.json's features.enabled.
const ID = "kubernetes"

// Module declares the feature (listed in package main's features.go).
var Module = feature.Module{
	ID:       ID,
	Optional: true,
	Tools: []tools.Tool{
		tools.Tool{
			ID: "kubectl", Name: "kubectl", Group: tools.GroupInfra, Order: 20,
			Feature: "Kubernetes panel",
			Install: tools.Install("brew install kubectl", "sudo snap install kubectl --classic"), Docs: "guides/kubernetes.md",
		}.Find("kubectl", "version", "--client=true"),
	},
	New: func(h feature.Host) feature.Instance {
		return feature.Instance{Services: []application.Service{application.NewService(&API{host: h})}}
	},
}
