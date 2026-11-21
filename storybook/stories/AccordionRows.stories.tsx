import type { Meta, StoryObj } from '@storybook/react-vite'
import {
  Accordion,
  AccordionContent,
  AccordionFeedback,
  AccordionItem,
  AccordionRow,
  AccordionTrigger,
  Button,
  Collapsible,
  CollapsibleContent,
  CollapsibleFeedback,
  CollapsibleRow,
  CollapsibleTrigger,
  Switch,
  type AccordionAppearance,
  type AccordionDensity,
  type AccordionHeadingLevel,
  type CollapsibleAppearance,
  type CollapsibleContentAlign,
  type CollapsibleIndicator,
} from '@wunderui/react'
import { Bot, GitBranch, KeyRound, SquareTerminal } from 'lucide-react'

const APPEARANCES: AccordionAppearance[] = ['plain', 'divided', 'contained', 'separated']
const DENSITIES: AccordionDensity[] = ['default', 'compact']
const LEVELS: AccordionHeadingLevel[] = [2, 3, 4, 5, 6]

const RULES = [
  { value: 'lint', icon: <SquareTerminal />, title: 'Run linters on save', body: 'golangci-lint and eslint run on the saved file only.' },
  { value: 'agent', icon: <Bot />, title: 'Let agents edit files', body: 'Claude Code may write inside the workspace; every edit lands in a review draft.' },
  { value: 'sign', icon: <KeyRound />, title: 'Sign commits', body: 'Commits are signed with the SSH key from ~/.ssh/id_ed25519.' },
]

type Args = { appearance: AccordionAppearance; density: AccordionDensity; headingLevel: AccordionHeadingLevel }

/** Rows that put a control before the trigger and an action after it, plus the panel feedback states. */
const meta = {
  title: 'Layout/Accordion & Collapsible Rows',
  args: { appearance: 'divided', density: 'default', headingLevel: 3 },
  argTypes: {
    appearance: { control: 'inline-radio', options: APPEARANCES },
    density: { control: 'inline-radio', options: DENSITIES },
    headingLevel: { control: 'inline-radio', options: LEVELS },
  },
  render: ({ appearance, density, headingLevel }) => (
    <div style={{ maxWidth: 520 }}>
      <Accordion appearance={appearance} density={density} headingLevel={headingLevel} defaultValue={['agent']}>
        {RULES.map((r, i) => (
          <AccordionItem key={r.value} value={r.value}>
            <AccordionRow
              leading={<Switch size="sm" defaultChecked={i !== 2} aria-label={r.title} />}
              action={
                <Button variant="plain" size="sm">
                  Edit
                </Button>
              }
            >
              <AccordionTrigger indicatorVariant="chevron">{r.title}</AccordionTrigger>
            </AccordionRow>
            <AccordionContent>{r.body}</AccordionContent>
          </AccordionItem>
        ))}
      </Accordion>
    </div>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** The four appearances side by side, compact density. */
export const Appearances: Story = {
  render: () => (
    <div className="sb-grid" style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))' }}>
      {APPEARANCES.map((appearance) => (
        <div key={appearance} className="sb-stack" style={{ gap: 6 }}>
          <span className="sb-label">{appearance}</span>
          <Accordion appearance={appearance} density="compact">
            {RULES.map((r) => (
              <AccordionItem key={r.value} value={r.value}>
                <AccordionRow action={<Button variant="plain" size="sm">Edit</Button>}>
                  <AccordionTrigger icon={r.icon} iconVariant="plain" indicatorVariant="chevron">
                    {r.title}
                  </AccordionTrigger>
                </AccordionRow>
                <AccordionContent>{r.body}</AccordionContent>
              </AccordionItem>
            ))}
          </Accordion>
        </div>
      ))}
    </div>
  ),
}

/** `AccordionFeedback` for a panel that is loading, empty or failed. */
export const AccordionFeedbackStates: Story = {
  name: 'Accordion feedback',
  render: () => (
    <div style={{ maxWidth: 520 }}>
      <Accordion appearance="contained" multiple defaultValue={['loading', 'empty', 'error']}>
        <AccordionItem value="loading">
          <AccordionTrigger>Open pull requests</AccordionTrigger>
          <AccordionContent>
            <AccordionFeedback kind="loading" loadingLabel="Loading pull requests…" />
          </AccordionContent>
        </AccordionItem>
        <AccordionItem value="empty">
          <AccordionTrigger>Stashes</AccordionTrigger>
          <AccordionContent>
            <AccordionFeedback kind="empty" title="No stashes" description="git stash saves work in progress here." />
          </AccordionContent>
        </AccordionItem>
        <AccordionItem value="error">
          <AccordionTrigger>CI checks</AccordionTrigger>
          <AccordionContent>
            <AccordionFeedback
              kind="error"
              title="Checks failed to load"
              description="GitHub returned 502 Bad Gateway."
              action={<Button size="sm">Retry</Button>}
            />
          </AccordionContent>
        </AccordionItem>
      </Accordion>
    </div>
  ),
}

type CollapsibleArgs = { appearance: CollapsibleAppearance; indicator: CollapsibleIndicator; align: CollapsibleContentAlign }

function BranchSection({ appearance, indicator, align }: CollapsibleArgs) {
  return (
    <Collapsible appearance={appearance} defaultOpen>
      <CollapsibleRow action={<Button variant="plain" size="sm">Fetch</Button>}>
        <CollapsibleTrigger icon={<GitBranch />} indicator={indicator} count={3} description="origin · last fetched 4m ago" summary="main, feat/ui-kit, fix/symlink-drafts">
          Remote branches
        </CollapsibleTrigger>
      </CollapsibleRow>
      <CollapsibleContent align={align}>
        <div className="sb-stack" style={{ gap: 4, font: '12px var(--font-mono)', color: 'var(--fg-muted)' }}>
          <span>main</span>
          <span>feat/ui-kit</span>
          <span>fix/symlink-drafts</span>
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

/** `CollapsibleRow`: the trigger plus a trailing action, in every appearance × indicator combination. */
export const CollapsibleRows: Story = {
  name: 'Collapsible rows',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 520, gap: 20 }}>
      {(['row', 'inline'] as const).flatMap((appearance) =>
        (['button', 'chevron'] as const).map((indicator) => (
          <div key={`${appearance}-${indicator}`} className="sb-stack" style={{ gap: 6 }}>
            <span className="sb-label" style={{ width: 'auto' }}>
              appearance={appearance} · indicator={indicator}
            </span>
            <BranchSection appearance={appearance} indicator={indicator} align="title" />
          </div>
        )),
      )}
    </div>
  ),
}

/** `CollapsibleContent align`: panel under the start of the row or under the title text. */
export const CollapsibleAlign: Story = {
  name: 'Collapsible content align',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 520, gap: 20 }}>
      {(['start', 'title'] as const).map((align) => (
        <div key={align} className="sb-stack" style={{ gap: 6 }}>
          <span className="sb-label">align={align}</span>
          <BranchSection appearance="row" indicator="chevron" align={align} />
        </div>
      ))}
    </div>
  ),
}

/** `CollapsibleFeedback`: loading, empty and error panels. */
export const CollapsibleFeedbackStates: Story = {
  name: 'Collapsible feedback',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 520 }}>
      {(
        [
          ['loading', 'Tags', {}],
          ['empty', 'Worktrees', { title: 'No worktrees', description: 'Create one to review a PR side by side.' }],
          ['error', 'Submodules', { title: 'git submodule failed', description: 'fatal: not a git repository', action: <Button size="sm">Retry</Button> }],
        ] as const
      ).map(([kind, label, props]) => (
        <Collapsible key={kind} appearance="row" defaultOpen>
          <CollapsibleTrigger indicator="chevron">{label}</CollapsibleTrigger>
          <CollapsibleContent>
            <CollapsibleFeedback kind={kind} {...props} />
          </CollapsibleContent>
        </Collapsible>
      ))}
    </div>
  ),
}
