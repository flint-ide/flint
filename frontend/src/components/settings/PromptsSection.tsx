import { useEffect, useState } from 'react'
import { useSettings } from '../../store/settings'
import { loadPrompts, openPromptDialog } from '../../store/prompts'
import { Row } from './fields'
import { Button, Switch } from '@flint-ide/ui'

/** Settings ▸ Agents ▸ Prompts: the "/" prompt library of the agent chat. */
export function PromptsSection() {
  const builtins = useSettings((s) => s.settings.prompts.builtins)
  const update = useSettings((s) => s.update)
  const [personalDir, setPersonalDir] = useState('')
  useEffect(() => {
    void loadPrompts('').then((lib) => lib && setPersonalDir(lib.personalDir))
  }, [])
  return (
    <>
      <h3>Prompts</h3>
      <Row label="Offer Flint's built-in prompts" hint="/review, /test, /explain and /fix. A personal or repository prompt with the same name replaces one.">
        <Switch checked={builtins} onCheckedChange={(checked) => update((s) => ({ prompts: { ...s.prompts, builtins: checked } }))} />
      </Row>
      <Row label="Personal prompts" hint="Markdown files for you only, in every folder. Repository prompts live in a folder's .flint/prompts and override personal ones with the same name.">
        <span className="selectable muted">{personalDir || '…'}</span>
      </Row>
      <div className="settings-actions">
        <Button variant="plain" onClick={() => openPromptDialog('new')}>New Prompt…</Button>
      </div>
    </>
  )
}
