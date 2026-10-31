import * as Diagnostics from '../../bindings/flint/diagnosticsapi'
import { errorMessage } from '../api/backend'
import { ClipboardSetText } from '../wails/runtime'
import { showToast } from './dialog'

/** Opens a prefilled GitHub issue (reviewed in the browser before anything is sent); the full report is on the clipboard. */
export async function reportIssue(): Promise<void> {
  try {
    await Diagnostics.OpenIssue('')
    showToast('Diagnostics copied — review the issue in your browser before submitting')
  } catch (err) {
    showToast(`Couldn't open the issue: ${errorMessage(err)}`, 'error')
  }
}

export async function copyDiagnostics(): Promise<void> {
  try {
    const report = await Diagnostics.Report()
    if (await ClipboardSetText(report)) showToast('Diagnostics copied (secrets and home paths removed)')
  } catch (err) {
    showToast(errorMessage(err), 'error')
  }
}

export function showLogs(): void {
  Diagnostics.RevealLogs().catch((err) => showToast(errorMessage(err), 'error'))
}

/** Records a UI error in Flint's log (best effort: logging must never throw). */
export function logFrontendError(source: string, error: unknown, extra = ''): void {
  try {
    const e = error instanceof Error ? error : new Error(String(error))
    Diagnostics.LogFrontend(source, e.message, `${e.stack ?? ''}${extra ? `\n${extra}` : ''}`).catch(() => {})
  } catch {
    // the bindings may not be ready (very early errors)
  }
}

let installed = false

/** Uncaught exceptions and rejected promises of the UI go to the log. */
export function installErrorReporting(): void {
  if (installed) return
  installed = true
  window.addEventListener('error', (e) => logFrontendError('window', e.error ?? e.message))
  window.addEventListener('unhandledrejection', (e) => logFrontendError('promise', e.reason))
}
