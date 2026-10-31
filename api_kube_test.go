package main

import (
	"strings"
	"testing"

	"flint/internal/kube"
)

func TestOverrideKubeconfigSetsOnlyContextAndNamespace(t *testing.T) {
	c := kube.Context{Name: "arn:aws:eks:eu-central-1:1:cluster/prod", Cluster: "arn:aws:eks:eu-central-1:1:cluster/prod", User: "u: \"x\"", Namespace: "default"}
	got := string(overrideKubeconfig(c, "apps"))
	want := `apiVersion: v1
kind: Config
current-context: "arn:aws:eks:eu-central-1:1:cluster/prod"
contexts:
- name: "arn:aws:eks:eu-central-1:1:cluster/prod"
  context:
    cluster: "arn:aws:eks:eu-central-1:1:cluster/prod"
    user: "u: \"x\""
    namespace: "apps"
`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "users:") || strings.Contains(got, "clusters:") {
		t.Fatal("credentials or clusters copied")
	}
}
