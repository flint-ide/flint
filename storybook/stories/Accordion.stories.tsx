import type { Meta, StoryObj } from '@storybook/react-vite'
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@wunderui/react'
import { Bot, GitBranch, SquareTerminal, Braces } from 'lucide-react'

const ITEM_VARIANTS = ['card', 'divider'] as const
const INDICATORS = ['button', 'plus', 'chevron'] as const
const POSITIONS = ['leading', 'trailing'] as const
const ICON_VARIANTS = ['box', 'plain'] as const

type Args = {
  variant: (typeof ITEM_VARIANTS)[number]
  indicatorVariant: (typeof INDICATORS)[number]
  indicatorPosition: (typeof POSITIONS)[number]
  iconVariant: (typeof ICON_VARIANTS)[number]
  icons: boolean
  multiple: boolean
}

const SECTIONS = [
  {
    value: 'git',
    icon: <GitBranch />,
    title: 'Git',
    body: 'Fetch every 5 minutes, sign commits with SSH and keep review drafts next to the worktree.',
  },
  {
    value: 'terminal',
    icon: <SquareTerminal />,
    title: 'Terminal',
    body: 'Open zsh in the project root when the folder opens; scrollback keeps 10,000 lines.',
  },
  {
    value: 'agents',
    icon: <Bot />,
    title: 'Agents',
    body: 'Claude Code and Codex run in their own terminals and see only the files of this workspace.',
  },
  {
    value: 'lsp',
    icon: <Braces />,
    title: 'Language servers',
    body: 'gopls and typescript-language-server start on demand; rust-analyzer is disabled in Restricted Mode.',
  },
]

/** Stacked sections that expand in place; one at a time unless `multiple`. */
const meta = {
  title: 'Layout/Accordion',
  args: {
    variant: 'divider',
    indicatorVariant: 'chevron',
    indicatorPosition: 'trailing',
    iconVariant: 'box',
    icons: true,
    multiple: false,
  },
  argTypes: {
    variant: { control: 'inline-radio', options: ITEM_VARIANTS },
    indicatorVariant: { control: 'inline-radio', options: INDICATORS },
    indicatorPosition: { control: 'inline-radio', options: POSITIONS },
    iconVariant: { control: 'inline-radio', options: ICON_VARIANTS },
  },
  render: ({ variant, indicatorVariant, indicatorPosition, iconVariant, icons, multiple }) => (
    <div style={{ maxWidth: 520 }}>
      <Accordion defaultValue={['git']} multiple={multiple}>
        {SECTIONS.map((section) => (
          <AccordionItem key={section.value} value={section.value} variant={variant}>
            <AccordionTrigger
              icon={icons ? section.icon : undefined}
              iconVariant={iconVariant}
              indicatorVariant={indicatorVariant}
              indicatorPosition={indicatorPosition}
            >
              {section.title}
            </AccordionTrigger>
            <AccordionContent>{section.body}</AccordionContent>
          </AccordionItem>
        ))}
      </Accordion>
    </div>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const Cards: Story = { args: { variant: 'card', indicatorVariant: 'button' } }

export const PlusLeading: Story = { args: { indicatorVariant: 'plus', indicatorPosition: 'leading', icons: false } }

export const Multiple: Story = { args: { multiple: true } }

/** Every indicator next to each other, with the first item open. */
export const Indicators: Story = {
  render: () => (
    <div className="sb-grid" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))' }}>
      {INDICATORS.map((indicator) => (
        <div key={indicator} className="sb-stack">
          <span className="sb-label">{indicator}</span>
          <Accordion defaultValue={['git']}>
            {SECTIONS.slice(0, 3).map((section) => (
              <AccordionItem key={section.value} value={section.value} variant="card">
                <AccordionTrigger indicatorVariant={indicator}>{section.title}</AccordionTrigger>
                <AccordionContent>{section.body}</AccordionContent>
              </AccordionItem>
            ))}
          </Accordion>
        </div>
      ))}
    </div>
  ),
}
