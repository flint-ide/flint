import type { EditorView } from '@codemirror/view'
import { forEachDiagnostic, type Action, type Diagnostic } from '@codemirror/lint'
import { rootFor } from '../store/activeRoot'
import { showToast } from '../store/dialog'
import { askAgent } from './askAgent'
import { fileProblemsPrompt, problemAttachments, problemLine, problemPrompt, type Problem } from './agentPrompt'

// "Fix with agent" for language-server diagnostics. Lives with the editor chunk
// (CodeMirror is loaded lazily), not in the app-wide agentContext.

/** Lint tooltip / panel buttons for one diagnostic; ⌥-click sends right away. */
export function agentActions(path: string, diagnostic: Diagnostic, count: number): Action[] {
  trackAltClicks()
  const fix: Action = {
    name: 'Fix with agent',
    apply: (view, from, to) => fixProblemWithAgent(view, path, diagnostic, from, to, altClick),
  }
  const fixAll: Action = { name: `Fix all ${count} in file`, apply: (view) => void fixFileProblemsWithAgent(view, path, altClick) }
  return count > 1 ? [fix, fixAll] : [fix]
}

/** One diagnostic (from..to is its current range in the view) with the lines around it. */
export function fixProblemWithAgent(view: EditorView, path: string, d: Diagnostic, from: number, to: number, send: boolean): void {
  const problem = toProblem(view, d, from, to)
  const root = rootFor(path)
  void askAgent({ prompt: problemPrompt(path, root, problem), attachments: problemAttachments(path, view.state.doc, [problem]), send })
}

/** "Fix all problems in file with agent": every diagnostic of the file in one message. */
export function fixFileProblemsWithAgent(view: EditorView, path: string, send = false): boolean {
  const problems: Problem[] = []
  forEachDiagnostic(view.state, (d, from, to) => problems.push(toProblem(view, d, from, to)))
  if (problems.length === 0) {
    showToast('No problems in this file')
    return true
  }
  const root = rootFor(path)
  void askAgent({ prompt: fileProblemsPrompt(path, root, problems), attachments: problemAttachments(path, view.state.doc, problems), send })
  return true
}

/** The view's diagnostics as "- line:col severity: message" lines (the {{problems}} of a prompt). */
export function problemsText(view: EditorView): string {
  const lines: string[] = []
  forEachDiagnostic(view.state, (d, from, to) => lines.push(`- ${problemLine(toProblem(view, d, from, to))}`))
  return lines.join('\n')
}

function toProblem(view: EditorView, d: Diagnostic, from: number, to: number): Problem {
  const doc = view.state.doc
  const start = doc.lineAt(from)
  return {
    line: start.number,
    col: from - start.from + 1,
    endLine: doc.lineAt(Math.max(from, to)).number,
    severity: d.severity,
    source: d.source,
    message: d.message,
  }
}

// Lint actions are not given the click event: remember whether ⌥ was held on the
// latest press (captured before the button's own mousedown handler runs).
let altClick = false
let trackingAlt = false
function trackAltClicks(): void {
  if (trackingAlt) return
  trackingAlt = true
  window.addEventListener('mousedown', (e) => (altClick = e.altKey), true)
  window.addEventListener('keydown', () => (altClick = false), true)
}
