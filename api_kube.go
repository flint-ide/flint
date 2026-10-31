package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"flint/internal/config"
	"flint/internal/kube"
	"flint/internal/shellenv"
)

// KubeAPI shows Kubernetes clusters through kubectl, any context of the
// kubeconfig without switching the global one.
type KubeAPI struct {
	app *App
}

func (k *KubeAPI) kubectl() (*kube.Kubectl, error) {
	shellenv.Wait()
	return kube.New()
}

// Contexts lists the kubeconfig's contexts, the current one first.
func (k *KubeAPI) Contexts() ([]kube.Context, error) {
	kc, err := k.kubectl()
	if err != nil {
		return nil, err
	}
	return kc.Contexts(k.app.ctx)
}

// Namespaces lists a cluster's namespaces.
func (k *KubeAPI) Namespaces(kubeContext string) ([]string, error) {
	kc, err := k.kubectl()
	if err != nil {
		return nil, err
	}
	return kc.Namespaces(k.app.ctx, kubeContext)
}

// Overview lists a namespace's workloads, pods, services and jobs.
func (k *KubeAPI) Overview(kubeContext, namespace string) (kube.Overview, error) {
	kc, err := k.kubectl()
	if err != nil {
		return kube.Overview{}, err
	}
	return kc.Overview(k.app.ctx, kubeContext, namespace)
}

// RolloutRestart restarts a Deployment, StatefulSet or DaemonSet.
func (k *KubeAPI) RolloutRestart(kubeContext, namespace, kind, name string) error {
	kc, err := k.kubectl()
	if err != nil {
		return err
	}
	return kc.RolloutRestart(k.app.ctx, kubeContext, namespace, kind, name)
}

// Scale sets a Deployment's or StatefulSet's replicas.
func (k *KubeAPI) Scale(kubeContext, namespace, kind, name string, replicas int) error {
	kc, err := k.kubectl()
	if err != nil {
		return err
	}
	return kc.Scale(k.app.ctx, kubeContext, namespace, kind, name, replicas)
}

// DeletePod deletes a pod.
func (k *KubeAPI) DeletePod(kubeContext, namespace, name string) error {
	kc, err := k.kubectl()
	if err != nil {
		return err
	}
	return kc.DeletePod(k.app.ctx, kubeContext, namespace, name)
}

// UseContext switches the kubeconfig's current context (for all tools).
func (k *KubeAPI) UseContext(name string) error {
	kc, err := k.kubectl()
	if err != nil {
		return err
	}
	return kc.UseContext(k.app.ctx, name)
}

// TerminalEnv is the environment of a terminal working in a context (and
// namespace) without touching the global current-context: KUBECONFIG starts
// with a small file that sets only current-context and that context's
// namespace (kubectl takes the first file to set a key), followed by the
// user's own kubeconfig. No credentials are copied.
func (k *KubeAPI) TerminalEnv(name, namespace string) (map[string]string, error) {
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
	dir := filepath.Join(k.app.configDir, "kube")
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
