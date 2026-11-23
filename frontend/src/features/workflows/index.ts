// The Workflows feature (Go: internal/features/workflows): repeatable ways to deliver a change, as steps that
// agents, scripts and you take in order. A panel lists the runs and the workflows, a tab steers one run, a
// status bar item says when a run waits for you. The panel, its commands and the tab exist only while the
// feature runs.
import { lazy } from 'react'
import { lazyPreload } from '../../lib/lazyPreload'
import { defineFeature } from '../../lib/features'
import { RUN_VIEW, WORKFLOW_VIEW, isWorkflowFile, workflowFileRoot } from './workflows'
import { workflowSettings } from './settings'
import { followWorkflows, openCoordinateDialog, openNewDialog, openRunDialog, showWorkflowDiagram, useNeedsYou } from './store'
import { WorkflowsStatus } from './StatusItem'

const WorkflowsPanel = lazyPreload(() => import('./WorkflowsPanel').then((m) => ({ default: m.WorkflowsPanel })))
const WorkflowsSection = lazy(() => import('./WorkflowsSection').then((m) => ({ default: m.WorkflowsSection })))
const RunView = lazy(() => import('./RunView').then((m) => ({ default: m.RunView })))

const WorkflowView = lazy(() => import('./WorkflowView').then((m) => ({ default: m.WorkflowView })))

const KEYWORDS = 'pipeline sop steps automation review gate approve run loop agents workflow process checklist plan implement test'

export default defineFeature({
  id: 'workflows',
  optional: true,
  name: 'Workflows',
  description: 'Run a change through the same steps every time: agents, scripts, an independent review and your approval. A panel lists the runs, and each run is steered from one tab.',
  settings: [workflowSettings],
  panels: [
    {
      id: 'workflows', title: 'Workflows', icon: 'workflow', order: 16, component: WorkflowsPanel, useBadge: useNeedsYou,
      command: { id: 'workflows.show', title: 'Show Workflows', category: 'Workflows', keywords: KEYWORDS },
    },
  ],
  commands: [
    { id: 'workflows.run', title: 'Run Workflow…', category: 'Workflows', keywords: `${KEYWORDS} start new task`, run: () => openRunDialog() },
    { id: 'workflows.new', title: 'New Workflow…', category: 'Workflows', keywords: `${KEYWORDS} create write file markdown`, run: openNewDialog },
    {
      id: 'workflows.diagram', title: 'Show Workflow Diagram…', category: 'Workflows', keywords: `${KEYWORDS} diagram graph visual flow canvas`,
      run: () => showWorkflowDiagram(),
    },
    {
      id: 'workflows.coordinate', title: 'Coordinate with Agent…', category: 'Workflows',
      keywords: `${KEYWORDS} coordinator orchestrate manage runs agents main bot goal`, run: openCoordinateDialog,
    },
  ],
  fileMenu: [
    {
      id: 'workflows.diagram', label: 'Show Workflow Diagram', when: (paths) => paths.length === 1 && isWorkflowFile(paths[0]),
      run: ([path]) => showWorkflowDiagram(workflowFileRoot(path), path),
    },
  ],
  settingsPages: [{ id: 'workflows', label: 'Workflows', order: 125, component: WorkflowsSection }],
  statusItems: [{ id: 'workflows', order: 50, component: WorkflowsStatus }],
  tabKinds: [
    { id: RUN_VIEW, component: RunView, icon: 'workflow', restore: true },
    { id: WORKFLOW_VIEW, component: WorkflowView, icon: 'workflow', restore: true },
  ],
  activate: followWorkflows,
})
