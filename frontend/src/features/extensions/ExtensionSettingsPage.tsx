import { useEffect, useState } from 'react'
import { useSettings } from '../../store/settings'
import { NumberField, Row, TextField } from '../../components/settings/fields'
import { Select } from '../../components/ui/Select'
import { storedValues, withValue } from './settings'
import { checkValue, currentValue, linesToList, type SchemaProperty } from './schema'
import { useExtensions } from './store'

/**
 * An extension's Settings page, generated from the schema in its manifest
 * with the controls of Flint's own pages. Values are saved under
 * "extensions" → <id> in settings.json, only when the schema accepts them.
 */
export function ExtensionSettingsPage({ id }: { id: string }) {
  const info = useExtensions((s) => s.list.find((i) => i.id === id))
  const stored = useSettings((s) => storedValues(s.settings.extensions, id))
  const update = useSettings((s) => s.update)
  if (!info?.settings) return null
  const commit = (key: string, value: unknown) => update((s) => ({ extensions: withValue(s.extensions, id, key, value) }))
  return (
    <section>
      <h2>{info.settings.title || info.name}</h2>
      <p className="muted settings-note">
        Settings of the {info.name} extension ({info.id}), saved in settings.json under "extensions". The extension is told when they change.
      </p>
      {info.settings.properties.map((p) => (
        <Row key={p.key} label={p.title || p.key} hint={p.description}>
          <Field prop={p} value={currentValue(p, stored)} onCommit={(v) => commit(p.key, v)} />
        </Row>
      ))}
    </section>
  )
}

function Field({ prop, value, onCommit }: { prop: SchemaProperty; value: unknown; onCommit: (v: unknown) => void }) {
  switch (prop.type) {
    case 'boolean':
      return <input type="checkbox" checked={value === true} onChange={(e) => onCommit(e.target.checked)} />
    case 'integer':
    case 'number':
      return (
        <NumberField
          value={typeof value === 'number' ? value : 0}
          min={prop.minimum ?? -1e15}
          max={prop.maximum ?? 1e15}
          step={prop.type === 'integer' ? 1 : 0.1}
          onCommit={(n) => checkValue(prop, n) === '' && onCommit(n)}
        />
      )
    case 'array':
      return <ListField prop={prop} value={Array.isArray(value) ? (value as string[]) : []} onCommit={onCommit} />
  }
  if (prop.enum?.length) {
    return <Select value={String(value)} options={prop.enum.map((v) => ({ value: v, label: v }))} onChange={onCommit} />
  }
  return <TextField value={typeof value === 'string' ? value : ''} valid={(v) => checkValue(prop, v) === ''} onCommit={onCommit} />
}

/** A list of text, one entry per line, saved when the field loses focus. */
function ListField({ prop, value, onCommit }: { prop: SchemaProperty; value: readonly string[]; onCommit: (v: string[]) => void }) {
  const saved = value.join('\n')
  const [draft, setDraft] = useState(saved)
  useEffect(() => setDraft(saved), [saved])
  const list = linesToList(draft)
  const why = checkValue(prop, list)
  return (
    <div className="settings-control settings-control-stack">
      <textarea
        className={`settings-textarea${why ? ' invalid' : ''}`}
        value={draft}
        spellCheck={false}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={() => {
          if (!why && list.join('\n') !== saved) onCommit(list)
        }}
      />
      {why && <span className="error">{why}</span>}
    </div>
  )
}
