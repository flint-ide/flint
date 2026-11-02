# Kubernetes

Kubernetes is an extension now, not part of Flint itself. It adds a Kubernetes panel that shows what runs in a namespace of a kubeconfig context, grouped into apps, with the rollout state, failing pods and their reasons, and recent warning events. **Diagnose with AI** puts what it found into an agent chat draft for you to review and send. It uses `kubectl` with your kubeconfig, so any sign-in method works (`aws eks get-token`, GKE, OIDC and others). Logs, shells, port forwards and a terminal in the selected context open as terminal tabs.

## Install it

1. Get the [flint-ide/extensions](https://github.com/flint-ide/extensions) repository and build its `kubernetes/` folder as the README there describes (`go build -o kubernetes-ext .` in that folder).
2. Run **Install Extension from Folder…** from the command palette and pick the `kubernetes/` folder. See [Install an extension](extensions.md#install-an-extension).

The extension needs `kubectl`. It gets `KUBECONFIG` (and, for EKS, the AWS variables) of your environment because its manifest names them (`run.env`); the install dialog lists them.

## Settings

**Settings ▸ Kubernetes** is the extension's own page. Its values are stored in `settings.json` under `extensions.flint.kubernetes` (see [extensions](../reference/settings.md#extensions)). The `refresh.kubernetesSeconds` and `mcpServer.tools.kubernetes` keys of the old built-in panel are ignored. Whether agents get the extension's tools is set in **Settings ▸ Extensions**.

## Related

- [Debug a failing deployment](../use-cases/debug-a-failing-deployment.md)
- [AWS](aws.md)
- [Extensions](extensions.md)
