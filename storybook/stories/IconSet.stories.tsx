import type { Meta, StoryObj } from '@storybook/react-vite'
import { useMemo, useState } from 'react'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
  Checkbox,
  DEFAULT_ICONS,
  Icon,
  IconProvider,
  useIcon,
  type IconName,
  type IconProps,
  type IconSet,
} from '@wunderui/react'
import { ArrowBigDown, CheckCheck, CircleX, Plus } from 'lucide-react'

const NAMES = Object.keys(DEFAULT_ICONS) as IconName[]

/** A replacement set: bolder glyphs for four roles. Every other name keeps the Lucide default. */
const BOLD_SET: IconSet = {
  chevronDown: (props: IconProps) => <ArrowBigDown {...props} />,
  check: (props: IconProps) => <CheckCheck {...props} />,
  checkboxCheck: (props: IconProps) => <CheckCheck {...props} />,
  close: (props: IconProps) => <CircleX {...props} />,
  plus: (props: IconProps) => <Plus strokeWidth={3} {...props} />,
}

type Args = { name: IconName; size: number }

/** The glyphs the components draw themselves, one name per role, swappable with `IconProvider`. */
const meta = {
  title: 'Foundations/Icon Set',
  args: { name: 'branch', size: 24 },
  argTypes: {
    name: { control: 'select', options: NAMES },
    size: { control: { type: 'range', min: 12, max: 48, step: 2 } },
  },
  render: ({ name, size }) => (
    <div className="sb-row" style={{ gap: 12 }}>
      <Icon name={name} size={size} />
      <code className="sb-label" style={{ width: 'auto' }}>
        &lt;Icon name="{name}" /&gt;
      </code>
    </div>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** Every built-in role of `DEFAULT_ICONS`, searchable. */
export const AllRoles: Story = {
  name: 'All roles',
  render: function Render() {
    const [query, setQuery] = useState('')
    const names = useMemo(() => NAMES.filter((n) => n.toLowerCase().includes(query.toLowerCase())), [query])
    return (
      <div className="sb-stack">
        <input
          className="sb-search"
          placeholder={`Filter ${NAMES.length} roles…`}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        <div className="sb-icons">
          {names.map((name) => (
            <div key={name} className="sb-icon" title={name}>
              <Icon name={name} size={20} />
              <span>{name}</span>
            </div>
          ))}
        </div>
      </div>
    )
  },
}

function Sample() {
  return (
    <div className="sb-stack" style={{ width: 280 }}>
      <Checkbox label="Run tests before commit" defaultChecked />
      <Accordion appearance="contained" defaultValue={['git']}>
        <AccordionItem value="git">
          <AccordionTrigger indicatorVariant="chevron">Git</AccordionTrigger>
          <AccordionContent>Fetch every 5 minutes.</AccordionContent>
        </AccordionItem>
        <AccordionItem value="lsp">
          <AccordionTrigger indicatorVariant="plus">Language servers</AccordionTrigger>
          <AccordionContent>gopls, tsserver, rust-analyzer.</AccordionContent>
        </AccordionItem>
      </Accordion>
      <div className="sb-row">
        {(['chevronDown', 'check', 'close', 'plus', 'branch'] as const).map((n) => (
          <Icon key={n} name={n} size={18} />
        ))}
      </div>
    </div>
  )
}

/** Same components with and without a provider: only the listed roles change. */
export const ProviderOverride: Story = {
  name: 'IconProvider override',
  render: () => (
    <div className="sb-row" style={{ gap: 40, alignItems: 'flex-start' }}>
      <div className="sb-stack">
        <span className="sb-label">Lucide (default)</span>
        <Sample />
      </div>
      <div className="sb-stack">
        <span className="sb-label">IconProvider</span>
        <IconProvider icons={BOLD_SET}>
          <Sample />
        </IconProvider>
      </div>
    </div>
  ),
}

function ResolvedChevron() {
  const Chevron = useIcon('chevronDown')
  return <Chevron size={20} />
}

/** `useIcon(name)` returns the component of the nearest provider; nested providers override only what they list. */
export const UseIconHook: Story = {
  name: 'useIcon & nesting',
  render: () => (
    <div className="sb-stack">
      <div className="sb-row">
        <span className="sb-label">no provider</span>
        <ResolvedChevron />
      </div>
      <IconProvider icons={BOLD_SET}>
        <div className="sb-row">
          <span className="sb-label">outer provider</span>
          <ResolvedChevron />
          <Icon name="close" size={20} />
        </div>
        <IconProvider icons={{ close: (p: IconProps) => <Plus style={{ transform: 'rotate(45deg)' }} {...p} /> }}>
          <div className="sb-row">
            <span className="sb-label">nested (close only)</span>
            <ResolvedChevron />
            <Icon name="close" size={20} />
          </div>
        </IconProvider>
      </IconProvider>
    </div>
  ),
}
