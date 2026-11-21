import type { Meta, StoryObj } from '@storybook/react-vite'
import { Badge, HoverCard, HoverCardContent, HoverCardTrigger } from '@wunderui/react'
import { GitPullRequest } from 'lucide-react'

const SIDES = ['top', 'right', 'bottom', 'left'] as const

type Args = { side: (typeof SIDES)[number]; sideOffset: number; defaultOpen: boolean }

const link: React.CSSProperties = { color: 'var(--accent, inherit)', textDecoration: 'underline', cursor: 'pointer' }

/** A preview that opens while the pointer rests on a link, e.g. a pull request reference. */
const meta = {
  title: 'Overlays/Hover Card',
  args: { side: 'bottom', sideOffset: 8, defaultOpen: false },
  argTypes: {
    side: { control: 'inline-radio', options: SIDES },
    sideOffset: { control: { type: 'range', min: 0, max: 32, step: 2 } },
  },
  render: ({ side, sideOffset, defaultOpen }) => (
    <div style={{ minHeight: 320, display: 'grid', placeItems: 'center' }}>
      <p style={{ margin: 0, fontSize: 13 }}>
        Merged in{' '}
        <HoverCard defaultOpen={defaultOpen}>
          <HoverCardTrigger href="#" style={link}>
            #76
          </HoverCardTrigger>
          <HoverCardContent side={side} sideOffset={sideOffset}>
            <div className="sb-stack" style={{ gap: 8 }}>
              <div className="sb-row" style={{ color: 'var(--fg-muted)', fontSize: 12 }}>
                <GitPullRequest size={14} /> flint-ide/flint #76
              </div>
              <div style={{ fontWeight: 600 }}>Project feature proposals</div>
              <div style={{ color: 'var(--fg-muted)', fontSize: 12 }}>
                Review drafts read the working tree of a folder opened through a symlink.
              </div>
              <div className="sb-row">
                <Badge>merged</Badge>
                <span style={{ fontSize: 12, color: 'var(--fg-muted)' }}>+412 −87 · 12 files</span>
              </div>
            </div>
          </HoverCardContent>
        </HoverCard>{' '}
        two days ago.
      </p>
    </div>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** Rendered open so it shows up in screenshots. */
export const Open: Story = { args: { defaultOpen: true } }
