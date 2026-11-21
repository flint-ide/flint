import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { Avatar, AvatarFallback, Chip, EntityChip, EventChip, TokenChip, type ChipMark, type TokenState } from '@wunderui/react'
import { Bot, GitBranch, GitPullRequest } from 'lucide-react'

const MARKS: ChipMark[] = ['none', 'check', 'radio', 'checkbox', 'suggest']
const TOKEN_STATES: TokenState[] = ['default', 'focused', 'invalid', 'removing']
const EVENT_COLORS = ['indigo', 'blue', 'purple', 'pink', 'red', 'orange', 'yellow', 'green'] as const

type Args = { label: string; selected: boolean; mark: ChipMark; trailing: 'none' | 'chevron' | 'remove' }

/** Chip is one component with three variants: filter (default), entity and token. */
const meta = {
  title: 'Data Display/Chip',
  args: { label: 'Failing checks', selected: false, mark: 'check', trailing: 'none' },
  argTypes: {
    mark: { control: 'inline-radio', options: MARKS },
    trailing: { control: 'inline-radio', options: ['none', 'chevron', 'remove'] },
  },
  render: function Render({ label, selected: initial, mark, trailing }) {
    const [selected, setSelected] = useState(initial)
    return (
      <Chip selected={selected} mark={mark} trailing={trailing} onClick={() => setSelected((s) => !s)} onRemove={() => setSelected(false)}>
        {label}
      </Chip>
    )
  },
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** Every mark, unselected and selected. */
export const FilterMarks: Story = {
  render: () => (
    <div className="sb-stack">
      {[false, true].map((selected) => (
        <div key={String(selected)} className="sb-row">
          <span className="sb-label">{selected ? 'selected' : 'idle'}</span>
          {MARKS.map((mark) => (
            <Chip key={mark} mark={mark} selected={selected}>
              {mark}
            </Chip>
          ))}
        </div>
      ))}
    </div>
  ),
}

/** A filter bar over the pull request list: multi-select chips plus a chevron chip that opens a menu. */
export const FilterBar: Story = {
  render: function Render() {
    const filters = ['Open', 'Review requested', 'Draft', 'Failing checks']
    const [active, setActive] = useState<string[]>(['Open'])
    const [authorOpen, setAuthorOpen] = useState(false)
    const toggle = (f: string) => setActive((a) => (a.includes(f) ? a.filter((x) => x !== f) : [...a, f]))
    return (
      <div className="sb-row">
        {filters.map((f) => (
          <Chip key={f} role="checkbox" mark="checkbox" selected={active.includes(f)} onClick={() => toggle(f)}>
            {f}
          </Chip>
        ))}
        <Chip trailing="chevron" open={authorOpen} onClick={() => setAuthorOpen((o) => !o)}>
          Author
        </Chip>
      </div>
    )
  },
}

/** Removable chips: the applied filters of a search. */
export const Removable: Story = {
  render: function Render() {
    const [chips, setChips] = useState(['branch:main', 'status:failed', 'job:lint'])
    return (
      <div className="sb-row">
        {chips.map((c) => (
          <Chip key={c} selected trailing="remove" removeLabel={`Remove ${c}`} onRemove={() => setChips((cs) => cs.filter((x) => x !== c))}>
            {c}
          </Chip>
        ))}
        {chips.length === 0 && <span className="sb-label">All filters cleared</span>}
      </div>
    )
  },
}

/** variant="entity": a person, an agent or a branch with an avatar or icon in front. */
export const Entity: Story = {
  render: () => (
    <div className="sb-stack">
      <div className="sb-row">
        <Chip variant="entity" leading={<Avatar size="xs"><AvatarFallback>AD</AvatarFallback></Avatar>}>
          Aleksander
        </Chip>
        <Chip variant="entity" leading={<Bot size={14} />} onRemove={() => undefined} removeLabel="Remove reviewer agent">
          Review agent
        </Chip>
        <Chip variant="entity" leading={<GitBranch size={14} />}>feat/ui-kit</Chip>
        <Chip variant="entity" leading={<GitPullRequest size={14} />}>#76</Chip>
      </div>
      <div className="sb-row">
        <span className="sb-label">EntityChip (deprecated alias)</span>
        <EntityChip leading={<GitBranch size={14} />}>main</EntityChip>
      </div>
    </div>
  ),
}

/** variant="token": input tokens in each state, e.g. reviewers typed into a field. */
export const Token: Story = {
  render: () => (
    <div className="sb-stack">
      <div className="sb-row">
        {TOKEN_STATES.map((state) => (
          <Chip key={state} variant="token" state={state} onRemove={() => undefined} removeLabel={`Remove ${state}`}>
            {state === 'invalid' ? 'not-a-user' : `@${state}`}
          </Chip>
        ))}
      </div>
      <div className="sb-row">
        <span className="sb-label">TokenChip (deprecated alias)</span>
        <TokenChip onRemove={() => undefined}>@flint-bot</TokenChip>
      </div>
    </div>
  ),
}

/** EventChip (deprecated alias of AgendaEventChip): a coloured calendar event pill. */
export const Event: Story = {
  render: () => (
    <div className="sb-row" style={{ maxWidth: 520 }}>
      {EVENT_COLORS.map((color, i) => (
        <EventChip key={color} color={color}>
          {['Release 0.9', 'Standup', 'Design review', 'Pairing', 'Incident', 'CI freeze', 'Demo', 'Retro'][i]}
        </EventChip>
      ))}
    </div>
  ),
}
