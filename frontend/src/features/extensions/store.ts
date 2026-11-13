// The installed extensions as Go reports them (ExtensionsAPI.List), what each
// one currently contributes, and the events their processes send: status
// texts, view refreshes, toasts, files to open and chat drafts. Go owns the
// processes and checks everything an extension asks for; this side only
// draws what Go allowed.
import { createElement, lazy, type ComponentType } from 'react'
import { createStore } from '../../lib/store'
import { registerCommands } from '../../lib/commands'
import { contribute } from '../../lib/contributions'
import { askAgent } from '../../lib/askAgent'
import { EventsOn } from '../../wails/runtime'
import * as API from '../../../bindings/flint/internal/features/extensions/api'
import type { Info, View } from '../../../bindings/flint/internal/extensions/host/models'
import { Workspace, errorMessage } from '../../api/backend'
import { showToast } from '../../store/dialog'
import { useEditor } from '../../store/editor'
import { useTerminal } from '../../store/terminal'
import { activeRoot } from '../../store/activeRoot'
import { shellCommand } from '../../lib/shellArgs'
import { useWorkspace } from '../../store/workspace'
import type { PanelProps } from '../../panels/registry'
import { contributes, contributionKey, extensionParts, type ExtensionInfo } from './model'
import { ExtensionStatusItem } from './ExtensionStatusItem'
import { deliverWebview, tabKeyOf } from './webviewEvents'
import type { ContributedTab } from '../../lib/features'
import { setExtensionLanguages } from '../../lib/extensionLanguages'
import { setExtensionTools } from '../../lib/extensionTools'
import { syncDecorationSets } from '../../lib/editorDecorations'
import { useSettings } from '../../store/settings'
import { decorationSetEnabled, withDecorationSet } from './settings'
import { quickFix, resyncDocuments, subscribeDocuments } from './documents'

export type { Info, View }

export interface StatusText {
  text: string
  tooltip: string
  command: string
  severity: string
}

interface ExtensionsState {
  list: readonly Info[]
  loaded: boolean
  /** Status item texts by extension, then item id. */
  status: Readonly<Record<string, Readonly<Record<string, StatusText>>>>
  /** Bumped by view/refresh: "<ext>\n<view>" → counter. */
  refresh: Readonly<Record<string, number>>
  /** Default shortcuts left out, by extension. */
  conflicts: Readonly<Record<string, readonly string[]>>
}

export const useExtensions = createStore<ExtensionsState>(() => ({ list: [], loaded: false, status: {}, refresh: {}, conflicts: {} }))

export const refreshKey = (ext: string, view: string) => `${ext}\n${view}`

// ---------- contributions ----------

const ExtensionPanel = lazy(() => import('./ExtensionPanel').then((m) => ({ default: m.ExtensionPanel })))
const WebviewFrame = lazy(() => import('./WebviewFrame').then((m) => ({ default: m.WebviewFrame })))
const ExtensionSettingsPage = lazy(() => import('./ExtensionSettingsPage').then((m) => ({ default: m.ExtensionSettingsPage })))

// One component per contribution, made once per extension version: React keeps
// a panel's state as long as its component type stays the same.
function panelComponent(ext: ExtensionInfo, panel: ExtensionInfo['panels'][number]): ComponentType<PanelProps> {
  const Panel = () => createElement(ExtensionPanel, { ext: ext.id, view: panel.id, title: panel.title, kind: panel.view })
  Panel.displayName = `ExtensionPanel(${panel.id})`
  return Panel
}

function webviewPanelComponent(ext: ExtensionInfo, panel: ExtensionInfo['panels'][number]): ComponentType<PanelProps> {
  const Panel = () => createElement(WebviewFrame, { ext: ext.id, view: panel.id, tabKey: '', data: null, active: true })
  Panel.displayName = `ExtensionWebview(${panel.id})`
  return Panel
}

function webviewTabComponent(ext: ExtensionInfo, view: string): ComponentType<{ tab: ContributedTab; active: boolean }> {
  const Tab = ({ tab, active }: { tab: ContributedTab; active: boolean }) =>
    createElement(WebviewFrame, { ext: ext.id, view, tabKey: tabKeyOf(tab), data: tab.data, active, tabId: tab.id })
  Tab.displayName = `ExtensionTab(${view})`
  return Tab
}

function statusComponent(ext: ExtensionInfo, item: string): ComponentType {
  const Item = () => createElement(ExtensionStatusItem, { ext: ext.id, item })
  Item.displayName = `ExtensionStatus(${item})`
  return Item
}

function settingsComponent(ext: ExtensionInfo): ComponentType<{ active: boolean }> {
  const Page = () => createElement(ExtensionSettingsPage, { id: ext.id })
  Page.displayName = `ExtensionSettings(${ext.id})`
  return Page
}

/** What each extension contributes now: its key and how to withdraw it. */
const installed = new Map<string, { key: string; withdraw: () => void }>()

/** Brings the contributions in line with the list: added, replaced or withdrawn per extension. */
function syncContributions(list: readonly Info[]): void {
  const wanted = new Map(list.filter((i) => contributes(i as ExtensionInfo)).map((i) => [i.id, i]))
  for (const [id, have] of installed) {
    const info = wanted.get(id)
    if (!info || contributionKey(info as ExtensionInfo) !== have.key) {
      have.withdraw()
      installed.delete(id)
    }
  }
  const conflicts: Record<string, readonly string[]> = { ...useExtensions.getState().conflicts }
  for (const [id, info] of wanted) {
    if (installed.has(id)) continue
    const parts = extensionParts(info as ExtensionInfo, {
      panel: panelComponent,
      webviewPanel: webviewPanelComponent,
      webviewTab: webviewTabComponent,
      statusItem: statusComponent,
      settingsPage: settingsComponent,
      run: (ext, command, args) => API.Execute(ext, command, args ?? null),
      setEnabled: (set, byDefault) => decorationSetEnabled(useSettings.getState().settings.extensions, set, byDefault),
      toggleSet: (set, byDefault) =>
        useSettings.getState().update((s) => ({ extensions: withDecorationSet(s.extensions, set, !decorationSetEnabled(s.extensions, set, byDefault), byDefault) })),
      quickFix,
      show: (panel) => useWorkspace.getState().setSidebar(panel),
    })
    try {
      const withdrawParts = contribute(`extension:${id}`, parts.contributions)
      const unregister = registerCommands(parts.commands)
      installed.set(id, {
        key: contributionKey(info as ExtensionInfo),
        withdraw: () => {
          unregister()
          withdrawParts()
        },
      })
      conflicts[id] = parts.conflicts
    } catch (err) {
      conflicts[id] = [`Not added: ${errorMessage(err)}`]
    }
  }
  useExtensions.setState({ conflicts })
}

// ---------- loading and actions ----------

function statusFrom(list: readonly Info[]): ExtensionsState['status'] {
  const out: Record<string, Record<string, StatusText>> = {}
  for (const info of list) {
    out[info.id] = Object.fromEntries(info.statusItems.map((s) => [s.id, { text: s.text, tooltip: s.tooltip, command: s.command, severity: s.severity }]))
  }
  return out
}

// One request at a time; a change reported meanwhile asks once more after it.
let loading: Promise<void> | null = null
let again = false

/** Asks Go for the list again and updates the contributions; rescan reads the folder again first. */
export function loadExtensions(rescan = false): Promise<void> {
  if (loading) {
    again = true
    return loading
  }
  loading = (rescan ? API.Rescan() : API.List())
    .then((list) => {
      useExtensions.setState({ list, loaded: true, status: statusFrom(list) })
      syncContributions(list)
      setExtensionLanguages(list.filter((i) => i.enabled && i.state !== 'invalid').flatMap((i) => i.languages ?? []))
      setExtensionTools(list)
      resyncDocuments() // an extension came, went or changed: who decorates which open file
    })
    .catch((err: unknown) => showToast(`Extensions: ${errorMessage(err)}`, 'error'))
    .finally(() => {
      loading = null
      if (again) {
        again = false
        void loadExtensions()
      }
    })
  return loading
}

/** "Install Extension from Folder…": the folder picker, then Go's confirmation. */
export async function installFromFolder(): Promise<void> {
  let dir = ''
  try {
    dir = await Workspace.PickFolder()
  } catch (err) {
    showToast(`Not installed: ${errorMessage(err)}`, 'error')
  }
  if (dir) await installExtension(dir)
}

export async function installExtension(dir: string): Promise<void> {
  try {
    const info = await API.Install(dir)
    if (!info) return // cancelled in the confirmation
    showToast(`Installed ${info.name} ${info.version}`)
    await loadExtensions()
  } catch (err) {
    showToast(`Not installed: ${errorMessage(err)}`, 'error')
  }
}

export async function uninstallExtension(id: string): Promise<void> {
  try {
    if (await API.Uninstall(id)) await loadExtensions()
  } catch (err) {
    showToast(`Not uninstalled: ${errorMessage(err)}`, 'error')
  }
}

export async function restartExtension(id: string): Promise<void> {
  try {
    await API.Restart(id)
    await loadExtensions()
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

/** `flint --install-extension <dir>`: the folder goes through the same confirmation as the picker's. */
function installRequested(req: { dir?: string; error?: string } | null | undefined): void {
  if (req?.error) showToast(`Not installed: ${req.error}`, 'error')
  else if (req?.dir) void installExtension(req.dir)
}

// ---------- events ----------

interface StatusEvent {
  extension: string
  item: StatusText & { id: string }
}

interface MessageEvent {
  extension: string
  name: string
  type: 'info' | 'warning' | 'error'
  message: string
}

interface OpenTerminalEvent {
  extension: string
  title?: string
  cwd?: string
  env?: Record<string, string>
  command?: string[]
  tag?: string
}

/** window/openTerminal (checked by Go): a terminal tab typing the extension's command. */
function openExtensionTerminal(e: OpenTerminalEvent): void {
  const name = useExtensions.getState().list.find((i) => i.id === e.extension)?.name ?? e.extension
  useTerminal.getState().create(e.cwd || activeRoot(), {
    title: e.title || undefined,
    input: e.command?.length ? shellCommand(e.command) : undefined,
    env: e.env ?? undefined,
    tag: e.tag || name,
  })
}

/** Subscribes to the events Go sends about extensions; returns the unsubscribe. */
export function subscribeExtensions(): () => void {
  const offs = [
    EventsOn('extensions:changed', () => void loadExtensions()),
    EventsOn('extensions:status', (e: StatusEvent) =>
      useExtensions.setState((s) => ({ status: { ...s.status, [e.extension]: { ...s.status[e.extension], [e.item.id]: e.item } } })),
    ),
    EventsOn('extensions:refresh', (e: { extension: string; view: string }) =>
      useExtensions.setState((s) => {
        const key = refreshKey(e.extension, e.view)
        return { refresh: { ...s.refresh, [key]: (s.refresh[key] ?? 0) + 1 } }
      }),
    ),
    EventsOn('extensions:message', (e: MessageEvent) => showToast(`${e.name}: ${e.message}`, e.type === 'error' ? 'error' : 'info')),
    EventsOn('extensions:openFile', (e: { path: string; line?: number }) =>
      useEditor.getState().openFile(e.path, e.line ? { line: e.line, from: 0, to: 0 } : undefined),
    ),
    EventsOn('extensions:webview', (e: { extension: string; view: string; key?: string; message: unknown }) => void deliverWebview(e)),
    EventsOn('extensions:openTab', (e: { extension: string; view: string; key: string; title: string; data?: unknown }) =>
      useEditor.getState().openContributed(e.view, e.key, e.title, e.data ?? null),
    ),
    EventsOn('extensions:openTerminal', (e: OpenTerminalEvent) => openExtensionTerminal(e)),
    subscribeDocuments(),
    // A decoration set turned off disappears at once; Go then tells the extension.
    useSettings.subscribe((s, prev) => {
      if (s.settings.extensions !== prev.settings.extensions) syncDecorationSets()
    }),
    EventsOn('extensions:ask', (e: { prompt: string; attachments?: string[] }) =>
      void askAgent({ prompt: e.prompt, attachments: (e.attachments ?? []).map((path) => ({ path })) }),
    ),
  ]
  offs.push(EventsOn('extensions:install-request', installRequested))
  // The list first, so a replaced version shows as such; then the folder this Flint was started to install.
  void loadExtensions().then(() => API.PendingInstall().then(installRequested, () => {}))
  return () => {
    offs.forEach((off) => off())
    for (const have of installed.values()) have.withdraw()
    installed.clear()
  }
}
