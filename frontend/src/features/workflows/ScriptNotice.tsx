import { consentText, scriptNotice, type StepInfo } from './workflows'
import { forgetUnsandboxed, openWorkflowSettings } from './store'
import { Alert, Button } from '@flint-ide/ui'

/**
 * What the script steps of a workflow run, and how: one line per script step and the sandbox sentence. Warning
 * tone when the sandbox is not on. Commands are text inside <code>, never HTML. Nothing is drawn without a script step.
 * onSettings runs before Settings opens (a dialog closes itself there).
 */
export function ScriptNotice({
  steps,
  sandbox,
  allowed = false,
  dir = '',
  onSettings,
}: {
  steps: readonly StepInfo[]
  sandbox: string
  /** The user already allowed scripts without a sandbox in dir (Listing.unsandboxedOK). */
  allowed?: boolean
  dir?: string
  onSettings?: () => void
}) {
  const notice = scriptNotice(steps, sandbox)
  if (!notice) return null
  const consent = consentText(sandbox, allowed)
  return (
    <Alert size="inline" variant={notice.warn ? 'warning' : 'plain'} role="note">
      {notice.lines.map((l) => (
        <span key={l.key} className="wf-reason">
          {l.before}
          {l.command && <code className="fui-mono wf-command">{l.command}</code>}
          {l.after}
        </span>
      ))}
      <span>{notice.sandbox}</span>
      {consent && <span>{consent}</span>}
      {notice.warn && (
        <span className="wf-buttons">
          {allowed && dir && sandbox === 'off' && (
            <Button variant="link" size="sm" type="button" title="Flint will ask again before the next run with script steps here" onClick={() => void forgetUnsandboxed(dir)}>
              Take back
            </Button>
          )}
          <Button
            variant="link"
            size="sm"
            type="button"
            onClick={() => {
              onSettings?.()
              openWorkflowSettings()
            }}
          >
            Open Workflows settings
          </Button>
        </span>
      )}
    </Alert>
  )
}
