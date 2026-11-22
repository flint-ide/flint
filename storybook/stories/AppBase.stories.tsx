import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Segment } from '@flint-ide/ui'
import { Select } from '../../frontend/src/components/ui/Select'

/**
 * The app's own controls before they become kit components: plain `<button>`,
 * `.primary`, `.danger`, `.link`, `.icon`, `<input>`, `.select` (with the kit's Segment),
 * `.badge` and `.modal` from frontend/src/style.css, drawn in the kit's look.
 */
const meta = { title: 'Flint/Base controls' } satisfies Meta

export default meta
type Story = StoryObj<typeof meta>

const row = { display: 'flex', gap: 8, alignItems: 'center', marginBottom: 14 } as const

export const Controls: Story = {
  render: function Render() {
    const [seg, setSeg] = useState('split')
    const [limit, setLimit] = useState(100)
    return (
      <div style={{ fontSize: 13 }}>
        <div style={row}>
          <button>Cancel</button>
          <button className="primary">Commit</button>
          <button className="danger">Uninstall…</button>
          <button className="link">Show details</button>
          <button className="icon" title="More">⋯</button>
          <button disabled>Disabled</button>
        </div>
        <div style={row}>
          <input placeholder="Branch name" />
          <Select label="limit" value={limit} onChange={setLimit} options={[50, 100, 500].map((n) => ({ value: n, label: String(n) }))} />
          <Segment size="sm" aria-label="Diff view" value={seg} onValueChange={setSeg} options={['unified', 'split', 'raw'].map((o) => ({ value: o, label: o }))} />
          <span className="badge" style={{ marginLeft: 0 }}>12</span>
        </div>
        <div className="modal" style={{ position: 'static', translate: 'none' }}>
          <div className="modal-title">New worktree</div>
          <div style={{ display: 'grid', gap: 8 }}>
            <input placeholder="feat/ui-kit" />
            <div className="form-actions">
              <span style={{ flex: 1 }} />
              <button>Cancel</button>
              <button className="primary">Create</button>
            </div>
          </div>
        </div>
      </div>
    )
  },
}
