import { useEffect, useState } from 'react'
import { useSettings } from '../../store/settings'
import { showToast } from '../../store/dialog'
import { errorMessage } from '../../api/backend'
import * as Completions from '../../../bindings/flint/completionsapi'
import {
  COMPLETION_LIMITS,
  DEFAULT_COMPLETIONS,
  DEFAULT_INLINE_EDIT,
  INLINE_EDIT_LIMITS,
  PROVIDER_ENDPOINTS,
  validEndpoint,
  validModel,
  type CompletionProvider,
  type CompletionSettings,
} from '../../lib/editorAiSettings'
import { Select } from '../ui/Select'
import { NumberField, Row, TextField } from './fields'
import { formatKeys } from '../../lib/commands'
import { keyringName } from '../../lib/platform'

const PROVIDERS: { value: CompletionProvider; label: string }[] = [
  { value: 'ollama', label: 'Ollama' },
  { value: 'openai', label: 'OpenAI-compatible (/v1/completions)' },
]

/** Settings ▸ AI in the Editor: Edit with Agent (⌘I) and inline completions. */
export function EditorAiSection() {
  const c = useSettings((s) => s.settings.completions)
  const inlineEdit = useSettings((s) => s.settings.inlineEdit)
  const update = useSettings((s) => s.update)
  const set = (change: Partial<CompletionSettings>) => update((s) => ({ completions: { ...s.completions, ...change } }))
  const isDefault = JSON.stringify({ ...c, enabled: false }) === JSON.stringify(DEFAULT_COMPLETIONS)
  return (
    <section>
      <h2>AI in the Editor</h2>
      <h3>Edit with Agent ({formatKeys('Mod+I')})</h3>
      <Row label="Context lines" hint="Lines above and below the selection the default agent sees with your instruction. Secrets in them are redacted.">
        <NumberField
          value={inlineEdit.contextLines}
          min={INLINE_EDIT_LIMITS.contextLines.min}
          max={INLINE_EDIT_LIMITS.contextLines.max}
          step={1}
          onCommit={(v) => update({ inlineEdit: { contextLines: Math.round(v) } })}
        />
      </Row>
      <div className="settings-actions">
        <button disabled={inlineEdit.contextLines === DEFAULT_INLINE_EDIT.contextLines} onClick={() => update({ inlineEdit: DEFAULT_INLINE_EDIT })}>
          Reset to Default
        </button>
      </div>

      <h3>Inline completions</h3>
      <Row label="Suggest code as I type" hint="Ghost text from a code model after a typing pause; Tab accepts, Esc dismisses. Off by default: it sends the code around the cursor to the endpoint.">
        <input type="checkbox" checked={c.enabled} onChange={(e) => set({ enabled: e.target.checked })} />
      </Row>
      <Row label="Provider">
        <Select
          value={c.provider}
          options={PROVIDERS}
          onChange={(p) => set({ provider: p, endpoint: c.endpoint === PROVIDER_ENDPOINTS[c.provider] ? PROVIDER_ENDPOINTS[p] : c.endpoint })}
        />
      </Row>
      <Row label="Endpoint" hint="Base URL of the server. Plain http only for localhost; anything else needs https, and secrets in the code are redacted before it is sent.">
        <TextField value={c.endpoint} valid={validEndpoint} onCommit={(v) => set({ endpoint: v.trim() })} />
      </Row>
      <Row label="Model" hint="A fill-in-the-middle code model, e.g. qwen2.5-coder:1.5b-base (ollama pull qwen2.5-coder:1.5b-base).">
        <TextField value={c.model} valid={validModel} onCommit={(v) => set({ model: v.trim() })} />
      </Row>
      <Row label="Typing pause">
        <NumberField value={c.debounceMs} {...COMPLETION_LIMITS.debounceMs} step={50} suffix="ms" onCommit={(v) => set({ debounceMs: Math.round(v) })} />
      </Row>
      <Row label="Longest suggestion">
        <NumberField value={c.maxTokens} {...COMPLETION_LIMITS.maxTokens} step={8} suffix="tokens" onCommit={(v) => set({ maxTokens: Math.round(v) })} />
      </Row>
      <Row label="Context lines" hint="Lines above and below the cursor sent with each request.">
        <NumberField value={c.contextLines} {...COMPLETION_LIMITS.contextLines} step={10} onCommit={(v) => set({ contextLines: Math.round(v) })} />
      </Row>
      <ApiKeyRow endpoint={c.endpoint} />
      <div className="settings-actions">
        <button disabled={isDefault} onClick={() => set({ ...DEFAULT_COMPLETIONS, enabled: c.enabled })}>Reset to Defaults</button>
      </div>
    </section>
  )
}

/**
 * The endpoint's API key, kept in the OS keyring (Keychain, Secret Service; never in settings.json)
 * for that endpoint's origin: another endpoint never gets it.
 */
function ApiKeyRow({ endpoint }: { endpoint: string }) {
  const [stored, setStored] = useState<boolean | null>(null)
  const [draft, setDraft] = useState('')
  useEffect(() => {
    let cancelled = false
    Completions.HasAPIKey().then((has) => !cancelled && setStored(has), () => !cancelled && setStored(false))
    return () => {
      cancelled = true
    }
  }, [endpoint])
  const save = async (key: string) => {
    try {
      await Completions.SetAPIKey(key)
      setStored(key.trim() !== '')
      setDraft('')
    } catch (err) {
      showToast(`API key not saved: ${errorMessage(err)}`, 'error')
    }
  }
  return (
    <Row
      label="API key"
      hint={stored
        ? `A key for this endpoint is stored in the ${keyringName()} and sent to it as a bearer token.`
        : `Only if the endpoint needs one. Stored in the ${keyringName()} for this endpoint only.`}
    >
      <span className="settings-control">
        <input
          type="password"
          className="settings-text"
          value={draft}
          placeholder={stored ? '••••••••' : 'none'}
          autoComplete="off"
          spellCheck={false}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && draft.trim() && void save(draft)}
        />
        <button disabled={!draft.trim()} onClick={() => void save(draft)}>Save</button>
        {stored && <button onClick={() => void save('')}>Remove</button>}
      </span>
    </Row>
  )
}
