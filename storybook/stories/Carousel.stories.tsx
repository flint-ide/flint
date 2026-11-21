import type { Meta, StoryObj } from '@storybook/react-vite'
import { Badge, Button, Carousel } from '@wunderui/react'
import { useState } from 'react'

const RELEASES = [
  { version: 'v0.6.0', title: 'WunderUI kit', body: 'The whole UI now runs on WunderUI components, with a Storybook to browse them.', tone: 'var(--primary)' },
  { version: 'v0.5.2', title: 'Review drafts through symlinks', body: 'Folders opened through a symlink now show the right working tree in review drafts.', tone: 'var(--success)' },
  { version: 'v0.5.0', title: 'Agent panel', body: 'Run Claude Code and Codex side by side, approve tool calls and follow each task.', tone: 'var(--chart-3)' },
  { version: 'v0.4.0', title: 'Kubernetes view', body: 'Pods, logs and exec for the current context, right next to your code.', tone: 'var(--brand-secondary)' },
]

function Slide({ version, title, body, tone, width }: (typeof RELEASES)[number] & { width?: number }) {
  return (
    <div
      style={{
        width,
        minHeight: 180,
        boxSizing: 'border-box',
        padding: 24,
        borderRadius: 12,
        border: '1px solid var(--border)',
        background: `linear-gradient(135deg, color-mix(in srgb, ${tone} 22%, transparent), transparent 70%)`,
        display: 'flex',
        flexDirection: 'column',
        gap: 8,
      }}
    >
      <div>
        <Badge color="indigo" badgeStyle="light" shape="pill">
          {version}
        </Badge>
      </div>
      <div style={{ fontSize: 18, fontWeight: 600 }}>{title}</div>
      <div style={{ fontSize: 13, color: 'var(--fg-muted)' }}>{body}</div>
    </div>
  )
}

const meta = {
  title: 'Data Display/Carousel',
  component: Carousel,
  args: {
    label: 'Release notes',
    navigationShape: 'rounded',
    showArrows: true,
    slideSize: 'full',
    autoPlay: 0,
    drag: true,
    children: RELEASES.map((r) => <Slide key={r.version} {...r} />),
  },
  argTypes: {
    navigationShape: { control: 'inline-radio', options: ['rounded', 'circle'] },
    slideSize: { control: 'inline-radio', options: ['auto', 'full'] },
    autoPlay: { control: { type: 'number', min: 0, step: 500 } },
    children: { control: false },
    index: { control: false },
  },
  decorators: [
    (Story) => (
      <div style={{ maxWidth: 560 }}>
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof Carousel>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const AutoSizedSlides: Story = {
  name: 'Auto-sized slides',
  args: {
    slideSize: 'auto',
    navigationShape: 'circle',
    children: RELEASES.map((r) => <Slide key={r.version} {...r} width={240} />),
  },
}

export const AutoPlay: Story = {
  args: { autoPlay: 3000 },
}

function ControlledCarousel() {
  const [index, setIndex] = useState(0)
  return (
    <div className="sb-stack">
      <Carousel label="Release notes" index={index} onIndexChange={setIndex} showArrows={false} slideSize="full">
        {RELEASES.map((r) => (
          <Slide key={r.version} {...r} />
        ))}
      </Carousel>
      <div className="sb-row" style={{ justifyContent: 'center' }}>
        {RELEASES.map((r, i) => (
          <Button key={r.version} size="sm" variant={i === index ? 'primary' : 'plain'} onClick={() => setIndex(i)}>
            {r.version}
          </Button>
        ))}
      </div>
    </div>
  )
}

export const Controlled: Story = {
  name: 'Controlled (version buttons)',
  render: () => <ControlledCarousel />,
}
