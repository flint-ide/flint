import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { labelOf } from '../../lib/workspaceRoots'
import {
  TESTS_KEY, configKey, currentKey, editRunConfig, loadRunConfigs, newRunConfig, rerunFailedTests, runSelected, runTestsInFile,
  runTestsInPackage, selectRunConfig, selectedRunConfig, stopRun, useRun, type RunConfig,
} from '../../store/run'
import { failedNodes } from '../../lib/runTree'
import { testLang } from '../../lib/testFiles'
import { TabKindIcon } from '../icons/FileIcon'
import { debugSelected, debugTestsInFile, useDebug } from '../../store/debug'
import { Dropdown, type DropdownItem } from '../ui/Dropdown'

/** Title bar run toolbar like IntelliJ: configuration selector, ▶ Run / ↻ Rerun, ■ Stop. */
export function RunWidget() {
  const saved = useRun((s) => s.saved)
  const detected = useRun((s) => s.detected)
  const loadError = useRun((s) => s.loadError)
  const selected = useRun(selectedRunConfig)
  // With several workspace folders the widget shows the active tab's folder's configurations.
  const runRoot = useRun((s) => s.root)
  const project = useWorkspace((s) => (s.roots.length > 1 && runRoot ? labelOf(s.roots, runRoot) : null))
  const selectedKey = useRun((s) => (selected && s.root ? configKey(s.root, selected.id) : null))
  const running = useRun((s) => (selectedKey ? !!s.sessions[selectedKey]?.running : false))
  // Stop targets the selected configuration's process, else the run in front.
  const stopKey = useRun((s) => {
    if (selectedKey && s.sessions[selectedKey]?.running) return selectedKey
    const key = currentKey()
    return key && s.sessions[key]?.running ? key : null
  })
  const hasFailures = useRun((s) => {
    const tests = s.sessions[TESTS_KEY]
    return !!tests && !tests.running && failedNodes(tests.tests).length > 0
  })
  const debuggable = useRun((s) => !!selected && !!s.capabilities[selected.id]?.debug)
  const debugging = useDebug((s) => s.session?.status === 'running' || s.session?.status === 'paused')
  const testFile = useEditor((s) => {
    const tab = s.tabs.find((t) => t.id === s.activeId)
    return tab?.kind === 'file' && testLang(tab.path) ? tab.path : null
  })

  const configItem = (c: RunConfig): DropdownItem => ({
    key: c.id,
    label: c.name,
    hint: describe(c),
    checked: c.id === selected?.id,
    keywords: c.type,
    onSelect: () => selectRunConfig(c.id),
  })

  const items = (): DropdownItem[] => [
    ...(project ? [{ type: 'section', label: `Configurations of ${project}` } as DropdownItem] : []),
    ...(loadError ? [{ type: 'section', label: `⚠ .flint/run.json: ${loadError}` } as DropdownItem] : []),
    ...saved.map(configItem),
    ...(detected.length ? [{ type: 'section', label: 'Detected' } as DropdownItem, ...detected.map(configItem)] : []),
    { type: 'separator' },
    { label: 'Edit Configuration…', disabled: !selected, onSelect: () => editRunConfig() },
    { label: 'Add Configuration…', onSelect: () => newRunConfig() },
    ...(testFile
      ? [
          { type: 'section', label: 'Tests' } as DropdownItem,
          { label: `Run Tests in ${testFile.split('/').pop()}`, kbd: '⌃⇧R', onSelect: () => void runTestsInFile(testFile) },
          { label: testLang(testFile) === 'go' ? 'Run Package Tests' : 'Run All Tests', onSelect: () => void runTestsInPackage(testFile) },
          ...(testLang(testFile) === 'go'
            ? [{ label: `Debug Tests in ${testFile.split('/').pop()}`, onSelect: () => void debugTestsInFile(testFile) }]
            : []),
        ]
      : []),
    ...(hasFailures ? [{ label: 'Rerun Failed Tests', onSelect: () => void rerunFailedTests() }] : []),
  ]

  const name = selected?.name ?? 'configuration'
  return (
    <div className="tb-run">
      <Dropdown
        minWidth={320}
        searchable
        placeholder="Filter configurations…"
        onOpen={() => void loadRunConfigs()}
        items={items}
        trigger={(t) => (
          <button {...t} className="tb-widget run-config" title={project ? `Run configurations of ${project}` : 'Run configurations'}>
            <TabKindIcon kind="run" busy={running} />
            <span className="tb-label">{selected?.name ?? 'Add Configuration…'}</span>
            {project && <span className="tb-extra">{project}</span>}
            <span className="tb-chevron">⌄</span>
          </button>
        )}
      />
      <button className="tb-icon run-start" title={`${running ? 'Rerun' : 'Run'} '${name}' (⌃R)`} onClick={() => void runSelected()}>
        {running ? <RerunIcon /> : <PlayIcon />}
      </button>
      <button
        className="tb-icon run-debug"
        title={debuggable ? `Debug '${name}' (⌃D)` : 'Debugging needs a Go configuration'}
        disabled={!debuggable}
        onClick={() => void debugSelected()}
      >
        <DebugIcon active={debugging} />
      </button>
      <button className="tb-icon run-stop" title="Stop (⌘F2)" disabled={!stopKey} onClick={() => void stopRun(stopKey)}>
        <StopIcon />
      </button>
    </div>
  )
}

/** Short description of what a configuration runs. */
export function describe(c: RunConfig): string {
  const args = c.args?.length ? ` ${c.args.join(' ')}` : ''
  switch (c.type) {
    case 'go':
      return `go ${c.package ?? ''}${args}`
    case 'npm':
      return `run ${c.script ?? ''}${args}${c.cwd ? ` · ${c.cwd}` : ''}`
    case 'make':
      return `make ${c.target ?? ''}`
    default:
      return `${c.command ?? ''}${args}`
  }
}

export function PlayIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden>
      <path d="M4.5 2.8v10.4l8.6-5.2z" fill="#5fbf7f" />
    </svg>
  )
}

export function RerunIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden>
      <path d="M13 8a5 5 0 1 1-1.5-3.6" fill="none" stroke="#5fbf7f" strokeWidth="1.6" strokeLinecap="round" />
      <path d="M12.6 1.8v3.3H9.3" fill="none" stroke="#5fbf7f" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

export function StopIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden>
      <rect x="3.5" y="3.5" width="9" height="9" rx="1.5" fill="currentColor" />
    </svg>
  )
}

export function DebugIcon({ active }: { active?: boolean }) {
  return (
    <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden className={active ? 'pulse' : undefined}>
      <ellipse cx="8" cy="9.2" rx="3.6" ry="4.3" fill="none" stroke="#f0a35e" strokeWidth="1.4" />
      <path d="M8 5.5v7.6M4.4 9.2H2.3M13.7 9.2h-2.1M5 6.4L3.2 4.9M11 6.4l1.8-1.5M5 12l-1.8 1.4M11 12l1.8 1.4" fill="none" stroke="#f0a35e" strokeWidth="1.2" strokeLinecap="round" />
    </svg>
  )
}
