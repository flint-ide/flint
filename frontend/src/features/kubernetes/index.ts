// The Kubernetes feature (Go: internal/features/kubernetes): the Kubernetes panel
// (contexts, namespaces, apps, workloads and their actions through kubectl). Pod
// logs and exec shells open as terminal tabs; the refresh interval stays in the
// core "refresh" settings (Settings ▸ Refresh).
import { lazyPreload } from '../../lib/lazyPreload'
import { defineFeature } from '../../lib/features'

// Not parsed at startup; fetched in the background once the app is idle (or when first opened).
const KubePanel = lazyPreload(() => import('./KubePanel').then((m) => ({ default: m.KubePanel })))

export default defineFeature({
  id: 'kubernetes',
  optional: true,
  name: 'Kubernetes',
  description: 'Clusters of your kubeconfig: apps, workloads and pods, rollouts, scaling, logs and shells, and the get_kubernetes_status agent tool.',
  panels: [
    {
      // The panel id predates the feature: keymap overrides and the open sidebar are stored by it.
      id: 'kube', title: 'Kubernetes', icon: 'helm', order: 100, component: KubePanel,
      command: { title: 'Kubernetes', keywords: 'k8s kubectl cluster pods context namespace' },
    },
  ],
})
