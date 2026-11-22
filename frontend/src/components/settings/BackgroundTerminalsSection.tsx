import { useSettings } from '../../store/settings'
import { REPLAY_KIB, type SessionsSettings } from '../../lib/sessionsSettings'
import { NumberField, Row } from './fields'
import { Switch } from '@flint-ide/ui'

/** Settings ▸ Terminal ▸ Background terminals: shells that keep running when Flint closes. */
export function BackgroundTerminalsSection() {
  const s = useSettings((st) => st.settings.sessions)
  const update = useSettings((st) => st.update)
  const set = (change: Partial<SessionsSettings>) => update((st) => ({ sessions: { ...st.sessions, ...change } }))
  return (
    <>
      <h3>Background terminals</h3>
      <Row
        label="Keep terminals running when Flint closes"
        hint='Off by default. When on, terminals run in a separate Flint process: closing the window or quitting leaves them running, and the next start reattaches their tabs. "Quit and Stop Background Sessions" in the menu ends them; closing a tab ends its shell. Agent chats are not kept yet. Takes effect the next time Flint starts.'
      >
        <Switch checked={s.background} onCheckedChange={(checked) => set({ background: checked })} />
      </Row>
      <Row label="Output kept per terminal" hint="How much recent output a background terminal keeps for the window that reattaches. Applies to terminals started afterwards.">
        <NumberField value={s.replayKiB} min={REPLAY_KIB.min} max={REPLAY_KIB.max} step={256} suffix="KiB" onCommit={(v) => set({ replayKiB: Math.round(v) })} />
      </Row>
    </>
  )
}
