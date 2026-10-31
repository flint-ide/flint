import { createStore } from '../lib/store'
import * as ProfileAPI from '../../bindings/flint/profileapi'
import { runconfig, testrun, type profiling } from '../wails/models'
import { errorMessage } from '../api/backend'
import { testLang } from '../lib/testFiles'
import type { TestRequest } from '../lib/runTree'
import { showToast } from './dialog'
import { profileTabId, useEditor } from './editor'
import { configKey, editRunConfig, runConfiguration, runTests, selectedRunConfig, useRun, type RunConfig, type RunExit } from './run'

// Profiling: Go tests run with -cpuprofile / -memprofile into a temporary directory
// (the reliable path), or a running configuration's net/http/pprof endpoint
// (`profileURL` in .flint/run.json). Go parses and aggregates the profile
// (internal/profiling); the flame graph tab (components/profile, loaded lazily) only
// lays it out. Temporary profiles are deleted when their tab closes, and by Go when
// Flint quits. Profile tabs are not part of session restore.

export type ProfileReport = profiling.Report

/** One profile file of a tab: CPU and memory profiles of a test run are two. */
export interface ProfileFile {
  label: string
  path: string
  /** Sample type shown first ('' = the profile's default). */
  sampleType: string
}

export interface ProfileDoc {
  id: string
  title: string
  files: ProfileFile[]
  /** Temporary directory Flint created for the files; removed with the tab. */
  dir?: string
  /** Shown file (index into files) and sample type. */
  file: number
  sampleType: string
  report: ProfileReport | null
  loading: boolean
  error: string
}

interface ProfileState {
  docs: Record<string, ProfileDoc>
  /** Recordings in progress: run session key → recording id (ProfileAPI.Cancel). */
  collecting: Record<string, string>
}

export const useProfile = createStore<ProfileState>(() => ({ docs: {}, collecting: {} }))

/** Latest report request per profile, so a slow load doesn't overwrite a newer one. */
const loads = new Map<string, number>()

function patchDoc(id: string, patch: (d: ProfileDoc) => Partial<ProfileDoc>): void {
  useProfile.setState((s) => {
    const doc = s.docs[id]
    if (!doc) return {}
    return { docs: { ...s.docs, [id]: { ...doc, ...patch(doc) } } }
  })
}

/** Opens a profile tab; report: already loaded for files[0]. */
export function openProfile(title: string, files: ProfileFile[], dir?: string, report?: ProfileReport): string {
  const id = crypto.randomUUID()
  const doc: ProfileDoc = {
    id, title, files, dir, file: 0, sampleType: report?.sampleType ?? files[0]?.sampleType ?? '',
    report: report ?? null, loading: !report, error: '',
  }
  useProfile.setState((s) => ({ docs: { ...s.docs, [id]: doc } }))
  useEditor.getState().openProfile(id, title)
  if (!report) void loadReport(id)
  return id
}

/** Loads the report of the shown file and sample type. */
async function loadReport(id: string): Promise<void> {
  const doc = useProfile.getState().docs[id]
  const file = doc?.files[doc.file]
  if (!doc || !file) return
  const seq = (loads.get(id) ?? 0) + 1
  loads.set(id, seq)
  patchDoc(id, () => ({ loading: true, error: '' }))
  try {
    const report = await ProfileAPI.Open(file.path, doc.sampleType)
    if (loads.get(id) === seq) patchDoc(id, () => ({ report, sampleType: report.sampleType, loading: false }))
  } catch (err) {
    if (loads.get(id) === seq) patchDoc(id, () => ({ report: null, loading: false, error: errorMessage(err) }))
  }
}

/** Shows another file of the profile (CPU / memory) in its preferred sample type. */
export function selectProfileFile(id: string, index: number): void {
  const doc = useProfile.getState().docs[id]
  if (!doc || doc.file === index || !doc.files[index]) return
  patchDoc(id, (d) => ({ file: index, sampleType: d.files[index].sampleType }))
  void loadReport(id)
}

export function selectSampleType(id: string, sampleType: string): void {
  const doc = useProfile.getState().docs[id]
  if (!doc || doc.sampleType === sampleType) return
  patchDoc(id, () => ({ sampleType }))
  void loadReport(id)
}

/** The profile tab closed: forget it and delete its temporary files. */
export function closeProfile(id: string): void {
  const doc = useProfile.getState().docs[id]
  loads.delete(id)
  useProfile.setState((s) => {
    const { [id]: _, ...docs } = s.docs
    return { docs }
  })
  useEditor.getState().closeTab(profileTabId(id))
  if (doc?.dir) void ProfileAPI.Discard(doc.dir).catch(() => undefined)
}

// ---------- tests ----------

const dirname = (p: string) => p.slice(0, Math.max(p.lastIndexOf('/'), 1))
const basename = (p: string) => p.slice(p.lastIndexOf('/') + 1)

/** Runs Go tests in the Tests tab with CPU and memory profiling, then opens the profile. */
export async function profileTests(req: TestRequest): Promise<void> {
  if (req.lang !== 'go') return showToast('Profiling is supported for Go tests', 'error')
  let started: { title: string; dir: string; files: testrun.Profiles } | null = null
  await runTests(req, {
    start: async (runId) => {
      started = await ProfileAPI.StartTests(runId, testrun.Request.createFrom(req), true)
      return started.title
    },
    onExit: (exit) => {
      const run = started
      if (!run) return
      void openTestProfile(run.title, run.dir, run.files, exit)
    },
    again: () => profileTests(req),
  })
}

async function openTestProfile(title: string, dir: string, files: testrun.Profiles, exit: RunExit | null): Promise<void> {
  const discard = () => void ProfileAPI.Discard(dir).catch(() => undefined)
  if (!exit || exit.stopped || exit.error) return discard()
  let report: ProfileReport
  try {
    report = await ProfileAPI.Open(files.cpu, '')
  } catch {
    discard()
    return showToast('No profile was written (did the tests build?)', 'error')
  }
  const list: ProfileFile[] = [{ label: 'CPU', path: files.cpu, sampleType: '' }]
  // alloc_space: what the tests allocated; in-use memory at exit says little about a test.
  if (files.mem) list.push({ label: 'Memory', path: files.mem, sampleType: 'alloc_space' })
  openProfile(title.replace(/^Profile /, 'Profile: '), list, dir, report)
}

export function profileTestAt(path: string, name: string): Promise<void> {
  return profileTests({ lang: 'go', dirs: [dirname(path)], tests: [name] })
}

export function profileTestsInFile(path: string): Promise<void> {
  return profileTests({ lang: 'go', dirs: [dirname(path)], file: path })
}

export function profileTestsInPackage(path: string): Promise<void> {
  return profileTests({ lang: 'go', dirs: [dirname(path)] })
}

/** Go test files can be profiled. */
export const profilable = (path: string | null | undefined): path is string => !!path && testLang(path) === 'go'

// ---------- run configurations ----------

/**
 * Records a CPU profile from config's profileURL (net/http/pprof), starting the
 * configuration first when it isn't running. Without profileURL the dialog opens,
 * since Go programs don't profile themselves.
 */
export async function profileConfiguration(config: RunConfig, root: string | null = useRun.getState().root): Promise<void> {
  if (!root) return
  if (!config.profileURL) {
    showToast(`Set a Profile URL (the program's net/http/pprof endpoint) to profile ${config.name}`, 'error')
    return editRunConfig(config)
  }
  const key = configKey(root, config.id)
  if (useProfile.getState().collecting[key]) return showToast(`Already profiling ${config.name}`)
  if (!useRun.getState().sessions[key]?.running) await runConfiguration(config, root)
  const session = useRun.getState().sessions[key]
  if (!session?.running) return // failed to start; the run tab says why

  const recording = crypto.randomUUID()
  useProfile.setState((s) => ({ collecting: { ...s.collecting, [key]: recording } }))
  showToast(`Profiling ${config.name}…`)
  try {
    const got = await ProfileAPI.Collect(recording, session.runId, runconfig.Config.createFrom(config))
    const label = got.seconds ? `CPU (${got.seconds} s)` : basename(got.path)
    openProfile(`Profile: ${config.name}`, [{ label, path: got.path, sampleType: '' }], got.dir)
  } catch (err) {
    showToast(`Profile ${config.name}: ${errorMessage(err)}`, 'error')
  } finally {
    useProfile.setState((s) => {
      const { [key]: _, ...collecting } = s.collecting
      return { collecting }
    })
  }
}

/** Profiles the selected configuration (title bar menu, palette). */
export function profileSelected(): Promise<void> {
  const config = selectedRunConfig()
  return config ? profileConfiguration(config) : Promise.resolve()
}

/** Stops recordings in progress (of one run session, or all). */
export function cancelProfiling(key?: string): void {
  const { collecting } = useProfile.getState()
  for (const [k, id] of Object.entries(collecting)) if (!key || k === key) void ProfileAPI.Cancel(id)
}

export const isProfiling = (key?: string) => {
  const { collecting } = useProfile.getState()
  return key ? !!collecting[key] : Object.keys(collecting).length > 0
}

// ---------- files ----------

/** Opens a .pprof file from disk (never deleted by Flint). */
export async function openProfileFile(): Promise<void> {
  try {
    const path = await ProfileAPI.ChooseFile()
    if (path) openProfile(basename(path), [{ label: basename(path), path, sampleType: '' }])
  } catch (err) {
    showToast(`Open profile: ${errorMessage(err)}`, 'error')
  }
}

export async function revealProfile(path: string): Promise<void> {
  try {
    await ProfileAPI.Reveal(path)
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}
