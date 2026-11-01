import { useEffect, useMemo, useState } from 'react'
import { EditorState, type Extension } from '@codemirror/state'
import type { HttpTab } from '../../store/editor'
import {
  cancelRequest,
  copyResponseAsCurl,
  saveResponseBody,
  showResponse,
  useHttpClient,
  type HttpResponse,
} from '../../store/httpClient'
import { bodyLanguagePath, formatBytes, formatTime, statusTone } from '../../lib/httpFiles'
import { languageFor } from '../../lib/language'
import { CodeEditor } from '../editor/CodeEditor'
import { Select } from '../ui/Select'
import { formatKeys } from '../../lib/commands'

const NO_HISTORY: HttpResponse[] = []

/** Responses to a .http file's requests: status line, headers, the body read-only. */
export function HttpResponseView({ tab, active }: { tab: HttpTab; active: boolean }) {
  const pending = useHttpClient((s) => s.files[tab.path]?.pending ?? null)
  const response = useHttpClient((s) => s.files[tab.path]?.response ?? null)
  const error = useHttpClient((s) => s.files[tab.path]?.error ?? null)
  const history = useHttpClient((s) => s.files[tab.path]?.history ?? NO_HISTORY)
  // After a failed send the previous response would look like its result: show the error alone.
  const shown = error ? null : response

  return (
    <div className="http-response">
      <div className="diff-header http-status-bar">
        {pending ? <Pending label={pending.label} startedAt={pending.startedAt} onCancel={() => cancelRequest(tab.path)} /> : shown && <StatusLine response={shown} />}
        <span className="spacer" />
        {history.length > 1 && shown && (
          <Select
            size="sm"
            label="history"
            align="end"
            value={shown.id}
            onChange={(id) => void showResponse(tab.path, id)}
            options={history.map((h) => ({
              value: h.id,
              label: `${h.status} ${h.name || `${h.method} ${shortUrl(h.url)}`}`,
              hint: new Date(h.at).toLocaleTimeString(),
            }))}
          />
        )}
        {shown && (
          <button className="link" title="Copy the request that produced this response as a curl command" onClick={() => void copyResponseAsCurl(shown.id)}>
            Copy as curl
          </button>
        )}
        {shown && shown.size > 0 && (
          <button className="link" title="Save the whole body to a file" onClick={() => void saveResponseBody(shown)}>
            Save to file
          </button>
        )}
      </div>
      {error && <div className="placeholder error selectable">{error}</div>}
      {!error && !response && !pending && (
        <div className="placeholder muted">No response yet. Put the cursor in a request and press {formatKeys('Mod+Enter')} (or click ▶ on the margin).</div>
      )}
      {shown && <ResponseDetails response={shown} active={active} />}
    </div>
  )
}

function Pending({ label, startedAt, onCancel }: { label: string; startedAt: number; onCancel: () => void }) {
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 100)
    return () => clearInterval(timer)
  }, [])
  return (
    <>
      <span className="http-status pending">Sending</span>
      <span className="truncate" title={label}>{label}</span>
      <span className="muted">{formatTime(now - startedAt)}</span>
      <button className="link" onClick={onCancel}>Cancel</button>
    </>
  )
}

function StatusLine({ response: r }: { response: HttpResponse }) {
  const redirected = r.finalUrl && r.finalUrl !== r.url
  return (
    <>
      <span className={`http-status ${statusTone(r.status)}`}>{r.statusText}</span>
      <span className="truncate selectable" title={redirected ? `${r.url}\n→ ${r.finalUrl}` : r.url}>
        {r.method} {redirected ? r.finalUrl : r.url}
      </span>
      <span className="muted" title="From sending the request until the whole body arrived">{formatTime(r.timeMs)}</span>
      <span className="muted">{formatBytes(r.size)}</span>
      {r.contentType && <span className="muted">{r.contentType}</span>}
    </>
  )
}

function ResponseDetails({ response: r, active }: { response: HttpResponse; active: boolean }) {
  const headers = r.headers ?? []
  return (
    <>
      <details className="http-headers">
        <summary>
          Headers <span className="muted">({headers.length}) · {r.proto}</span>
        </summary>
        <div className="http-header-list selectable">
          {headers.map((h, i) => (
            <div key={i}>
              <span className="http-header-name">{h.name}:</span> {h.value}
            </div>
          ))}
        </div>
      </details>
      <BodyNotice response={r} />
      {r.body && <Body key={r.id} response={r} active={active} />}
    </>
  )
}

function BodyNotice({ response: r }: { response: HttpResponse }) {
  const save = (
    <button className="link" onClick={() => void saveResponseBody(r)}>
      Save to file
    </button>
  )
  if (r.kind === 'empty') return <div className="http-notice muted">No body</div>
  if (!r.body) {
    return (
      <div className="http-notice muted">
        {r.kind === 'image' ? 'Image' : 'Binary'} body ({r.contentType || 'unknown type'}, {formatBytes(r.size)}) — {save}
      </div>
    )
  }
  if (r.truncated || r.incomplete) {
    return (
      <div className="http-notice">
        {r.incomplete ? `The body is larger than the read limit; only the first ${formatBytes(r.size)} were received.` : `Showing the start of ${formatBytes(r.size)}.`} {save}
      </div>
    )
  }
  return null
}

/** The body in a read-only editor (search, folding, JSON / XML / HTML highlighting). */
function Body({ response, active }: { response: HttpResponse; active: boolean }) {
  const [language, setLanguage] = useState<Extension>([])
  useEffect(() => {
    let cancelled = false
    void languageFor(bodyLanguagePath(response.kind)).then((lang) => !cancelled && setLanguage(lang))
    return () => {
      cancelled = true
    }
  }, [response.kind])
  const extensions = useMemo(() => [language, EditorState.readOnly.of(true)], [language])
  return (
    <div className="http-body">
      <CodeEditor initialDoc={response.body} language={extensions} active={active} />
    </div>
  )
}

function shortUrl(url: string): string {
  try {
    const u = new URL(url)
    return `${u.pathname}${u.search}`
  } catch {
    return url
  }
}
