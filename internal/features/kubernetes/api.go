package kubernetes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"flint/internal/config"
	"flint/internal/feature"
	"flint/internal/kube"
	"flint/internal/shellenv"
)

// API shows Kubernetes clusters through kubectl, any context of the
// kubeconfig without switching the global one (bound as the Kubernetes
// panel's backend).
type API struct {
	host feature.Host
}

func (k *API) kubectl() (*kube.Kubectl, error) {
	shellenv.Wait()
	return kube.New()
}

// Contexts lists the kubeconfig's contexts, the current one first.
func (k *API) Contexts() ([]kube.Context, error) {
	kc, err := k.kubectl()
	if err != nil {
		return nil, err
	}
	return kc.Contexts(k.host.Context())
}

// Namespaces lists a cluster's namespaces.
func (k *API) Namespaces(kubeContext string) ([]string, error) {
	kc, err := k.kubectl()
	if err != nil {
		return nil, err
	}
	return kc.Namespaces(k.host.Context(), kubeContext)
}

// Overview lists a namespace's workloads, pods, services and jobs.
func (k *API) Overview(kubeContext, namespace string) (kube.Overview, error) {
	kc, err := k.kubectl()
	if err != nil {
		return kube.Overview{}, err
	}
	return kc.Overview(k.host.Context(), kubeContext, namespace)
}

// RolloutRestart restarts a Deployment, StatefulSet or DaemonSet.
func (k *API) RolloutRestart(kubeContext, namespace, kind, name string) error {
	kc, err := k.kubectl()
	if err != nil {
		return err
	}
	return kc.RolloutRestart(k.host.Context(), kubeContext, namespace, kind, name)
}

// RolloutUndo rolls a workload back to its previous revision.
func (k *API) RolloutUndo(kubeContext, namespace, kind, name string) error {
	kc, err := k.kubectl()
	if err != nil {
		return err
	}
	return kc.RolloutUndo(k.host.Context(), kubeContext, namespace, kind, name)
}

// Scale sets a Deployment's or StatefulSet's replicas.
func (k *API) Scale(kubeContext, namespace, kind, name string, replicas int) error {
	kc, err := k.kubectl()
	if err != nil {
		return err
	}
	return kc.Scale(k.host.Context(), kubeContext, namespace, kind, name, replicas)
}

// DeletePod deletes a pod.
func (k *API) DeletePod(kubeContext, namespace, name string) error {
	kc, err := k.kubectl()
	if err != nil {
		return err
	}
	return kc.DeletePod(k.host.Context(), kubeContext, namespace, name)
}

// UseContext switches the kubeconfig's current context (for all tools).
func (k *API) UseContext(name string) error {
	kc, err := k.kubectl()
	if err != nil {
		return err
	}
	return kc.UseContext(k.host.Context(), name)
}

// TerminalEnv is the environment of a terminal working in a context (and
// namespace) without touching the global current-context: KUBECONFIG starts
// with a small file that sets only current-context and that context's
// namespace (kubectl takes the first file to set a key), followed by the
// user's own kubeconfig. No credentials are copied.
func (k *API) TerminalEnv(name, namespace string) (map[string]string, error) {
	if namespace != "" && !kube.ValidName(namespace) {
		return nil, fmt.Errorf("invalid namespace %q", namespace)
	}
	contexts, err := k.Contexts()
	if err != nil {
		return nil, err
	}
	var found *kube.Context
	for i := range contexts {
		if contexts[i].Name == name {
			found = &contexts[i]
		}
	}
	if found == nil {
		return nil, fmt.Errorf("no context %q in the kubeconfig", name)
	}
	if namespace == "" {
		namespace = found.Namespace
	}
	dir := filepath.Join(k.host.ConfigDir(), "kube")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(found.Name + "\x00" + namespace))
	path := filepath.Join(dir, hex.EncodeToString(sum[:8])+".yaml")
	if err := config.WriteFileAtomic(path, overrideKubeconfig(*found, namespace), 0o600); err != nil {
		return nil, err
	}
	base := os.Getenv("KUBECONFIG")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		base = filepath.Join(home, ".kube", "config")
	}
	return map[string]string{"KUBECONFIG": path + string(os.PathListSeparator) + base}, nil
}

// overrideKubeconfig is YAML (JSON strings are valid YAML scalars, so names
// with any characters stay one value).
func overrideKubeconfig(c kube.Context, namespace string) []byte {
	q := func(s string) string {
		b, _ := json.Marshal(s)
		return string(b)
	}
	out := "apiVersion: v1\nkind: Config\ncurrent-context: " + q(c.Name) + "\ncontexts:\n" +
		"- name: " + q(c.Name) + "\n  context:\n    cluster: " + q(c.Cluster) + "\n    user: " + q(c.User) + "\n"
	if namespace != "" {
		out += "    namespace: " + q(namespace) + "\n"
	}
	return []byte(out)
}

// Diagnose gathers an unhealthy app's state and failing pods' logs (redacted)
// for an AI agent.
func (k *API) Diagnose(kubeContext, namespace, appKey string) (string, error) {
	kc, err := k.kubectl()
	if err != nil {
		return "", err
	}
	return kc.Diagnose(k.host.Context(), kubeContext, namespace, appKey)
}
