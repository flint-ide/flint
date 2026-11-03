# Kubernetes

Kubernetes is an extension now, not part of Flint itself. It adds a Kubernetes panel that shows what runs in a namespace of a kubeconfig context, grouped into apps, with the rollout state, failing pods and their reasons, and recent warning events. **Diagnose with AI** puts what it found into an agent chat draft for you to review and send. It uses `kubectl` with your kubeconfig, so any sign-in method works (`aws eks get-token`, GKE, OIDC and others). Logs, shells, port forwards and a terminal in the selected context open as terminal tabs.

## Install it

1. Open **Settings ▸ Extensions** and find the Kubernetes extension under **Repository**.
2. Click **Install…**. Flint builds it with `go build` (Go must be installed) and shows its program, permissions and agent tools. Click **Install**.

**Update…** appears there when the repository has a newer version. To install from a folder instead, build the `kubernetes/` folder of [flint-ide/extensions](https://github.com/flint-ide/extensions) (`go build -o kubernetes-ext .`) and run **Install Extension from Folder…**. See [Install an extension](extensions.md#install-an-extension).

The extension needs `kubectl`. It gets `KUBECONFIG` (and, for EKS, the AWS variables) of your environment because its manifest names them (`run.env`); the install dialog lists them.

## Settings

**Settings ▸ Kubernetes** is the extension's own page. Its values are stored in `settings.json` under `extensions.flint.kubernetes` (see [extensions](../reference/settings.md#extensions)). The `refresh.kubernetesSeconds` and `mcpServer.tools.kubernetes` keys of the old built-in panel are ignored. Whether agents get the extension's tools is set in **Settings ▸ Extensions**.

## Related

- [Debug a failing deployment](../use-cases/debug-a-failing-deployment.md)
- [AWS](aws.md)
- [Extensions](extensions.md)
