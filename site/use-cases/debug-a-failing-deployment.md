# Debug a failing deployment

You merged a change, the deployment started, and something is wrong: the rollout hangs, pods crash, or traffic doesn't reach the new version. This walkthrough uses the Kubernetes panel to find out why, an agent to help investigate, and your manifests to fix it.

## Before you start

- Install `kubectl` and make sure you can reach the cluster from Terminal.
- For EKS, have the AWS profile from your kubeconfig set up (see [AWS](../guides/aws.md)).
- If you deploy with Flux, install the `flux` CLI for the Reconcile action.

## 1. Check your credentials (EKS)

1. Open the Kubernetes panel (`☸` on the activity bar).
2. Select the EKS context. The panel shows the AWS profile it uses and whether its credentials are valid.
3. If they have expired, click refresh. For SSO, approve the login in your browser.
4. If refresh needs input, such as an MFA code, click **Log In** and finish in the terminal.

## 2. Pick the context and namespace

1. Choose the context and the namespace at the top of the panel.

Flint only uses this selection for the panel and the terminals it opens. Your kubeconfig's current context stays as it is, so a terminal pointing at production isn't affected by what you browse here.

## 3. Find the unhealthy app

The **Apps** view groups resources the way you deploy them: by Flux Kustomization, Argo CD application, Helm release or app labels. Unhealthy apps are sorted first and expanded, so the broken one is at the top.

Read its status:

- **Rollout state:** "rolling out" with the reason, "failed" when the progress deadline was exceeded, "paused" or "scaled down".
- **Images:** check that the new image tag is the one you expect.
- **Pods:** the concrete problem, such as "CrashLoopBackOff, last OOMKilled (exit 137)" or an image pull error. Pods of the previous revision are marked.
- **Routes:** Ingress → Service (ready/total backing pods) → workload. A Service with 0 ready pods explains a 503.
- **Flux:** Kustomization readiness and revision, if you use Flux.
- **Events:** recent Warning events for the app, such as failed scheduling or failing probes.

## 4. Read the logs and details

Right-click the workload or a pod:

- **Logs** follows the logs in a terminal tab. Double-clicking an item also opens its logs.
- **Describe** shows the full `kubectl describe` output.
- **Rollout Status** and **Rollout History** show the rollout from kubectl's point of view.
- On a pod, **Shell** opens a shell inside it, and **Port Forward** lets you call it from your machine.

The **Resources** view lists all pods with status, restarts and age if you need a flat view.

## 5. Diagnose with AI

1. Click **✦ AI** on the unhealthy app, or right-click the workload and choose **Diagnose with AI**.
2. Flint collects the rollout state, the detected problems, the events, and the last 60 log lines (plus the previous crashed run) of up to 4 failing pods. It removes secrets and your home path.
3. The text lands in the agent chat draft with a request for a read-only investigation. Read it before sending, and remove anything you don't want to share.
4. Press `↵`.

The agent explains what it sees and suggests causes. If it wants to run commands such as `kubectl` to look further, it asks you first.

> **Tip:** If the cause is in your code (for example a panic on startup), keep the conversation going in the same chat: ask it to find the code path and propose a fix.

## 6. Fix the manifests

1. Press `⌘P` and open the manifest, Helm values or Kustomize overlay that needs changing.
2. Make the change yourself, or select the relevant part, press `⌘L` and ask the agent to change it.
3. Review the diff in the Commit panel (`⌘K`), commit and push.

If you need to stop the bleeding first, right-click the workload:

- **Undo Rollout** goes back to the previous revision (after a confirmation).
- **Rollout Restart** restarts the pods, for example after fixing a config map.
- **Scale to N** changes the replica count; scaling to 0 asks for confirmation.

## 7. Apply the fix with Flux

If the app is managed by Flux:

1. Right-click the Flux Kustomization row of the app.
2. Choose **Reconcile**. Flint runs `flux reconcile kustomization … --with-source` in a terminal, so Flux fetches your pushed commit right away instead of waiting for its interval.

## 8. Watch the rollout

The panel refreshes every 5 seconds while it is visible. Watch the rollout state move from "rolling out" to "complete" with the new revision, and the pods of the previous revision disappear. **Rollout Status** opens `kubectl rollout status` in a terminal if you prefer to watch it there.

## Tips

- **Use a context terminal.** `❯_` opens a terminal where `kubectl` uses the selected context and namespace, without changing your global kubeconfig. The tab is named `k8s:<cluster>/<namespace>` so you always know where it points.
- **Make it global only when you mean it.** ★ makes the selected context your kubeconfig's current context, for all tools.
- **Combine with Git history.** Right-click a manifest and choose **Show History** to see what changed recently.
