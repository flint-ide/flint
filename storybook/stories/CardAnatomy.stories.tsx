import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import {
  Badge,
  Button,
  Card,
  CardBanner,
  CardBody,
  CardContent,
  CardDescription,
  CardDragHandle,
  CardFeedback,
  CardFooter,
  CardHeader,
  CardIcon,
  CardIndicator,
  CardMedia,
  CardMeta,
  CardTitle,
  CardTrigger,
  type CardFeedbackKind,
  type CardFooterLayout,
  type CardMediaRatio,
  type CardOrientation,
} from '@wunderui/react'
import { Clock, GitBranch, GitPullRequest, MessageSquare, Rocket, Server, Workflow } from 'lucide-react'

const ORIENTATIONS: CardOrientation[] = ['vertical', 'horizontal']
const RATIOS: CardMediaRatio[] = ['16:9', '4:3', '1:1', '3:1']
const LAYOUTS: CardFooterLayout[] = ['end', 'between', 'stretch', 'stacked']
const FEEDBACK: CardFeedbackKind[] = ['loading', 'empty', 'error', 'denied']

type Args = { orientation: CardOrientation; ratio: CardMediaRatio; layout: CardFooterLayout; media: boolean }

function PrCard({ orientation, ratio, layout, media }: Args) {
  return (
    <Card orientation={orientation} style={{ maxWidth: orientation === 'horizontal' ? 520 : 340 }}>
      {media && <CardMedia ratio={orientation === 'vertical' ? ratio : undefined} />}
      <CardBody>
        <CardHeader
          leading={
            <CardIcon>
              <GitPullRequest />
            </CardIcon>
          }
        >
          <CardTitle>feat: Storybook for WunderUI</CardTitle>
          <CardDescription>flint-ide/flint#76</CardDescription>
        </CardHeader>
        <CardMeta>
          <span className="sb-row" style={{ gap: 4 }}>
            <GitBranch /> feat/ui-kit
          </span>
          <span className="sb-row" style={{ gap: 4 }}>
            <MessageSquare /> 8
          </span>
          <span className="sb-row" style={{ gap: 4 }}>
            <Clock /> 2h ago
          </span>
        </CardMeta>
        <CardFooter layout={layout}>
          <Button variant="plain" size="sm">
            View diff
          </Button>
          <Button size="sm">Approve</Button>
        </CardFooter>
      </CardBody>
    </Card>
  )
}

/** The newer Card parts: body, icon, meta row, banners, feedback states, selection indicators and drag handles. */
const meta = {
  title: 'Layout/Card Anatomy',
  args: { orientation: 'vertical', ratio: '3:1', layout: 'end', media: true },
  argTypes: {
    orientation: { control: 'inline-radio', options: ORIENTATIONS },
    ratio: { control: 'inline-radio', options: RATIOS },
    layout: { control: 'inline-radio', options: LAYOUTS },
  },
  render: (args) => <PrCard {...args} />,
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const Horizontal: Story = { args: { orientation: 'horizontal' } }

/** `CardMedia ratio`: the media area keeps its aspect ratio instead of the fixed 160 px. */
export const MediaRatios: Story = {
  name: 'Media ratios',
  render: () => (
    <div className="sb-grid">
      {RATIOS.map((ratio) => (
        <Card key={ratio} size="sm">
          <CardMedia ratio={ratio} />
          <CardHeader>
            <CardTitle>{ratio}</CardTitle>
          </CardHeader>
        </Card>
      ))}
      <Card size="sm">
        <CardMedia ratio="16:9" missing />
        <CardHeader>
          <CardTitle>missing</CardTitle>
        </CardHeader>
      </Card>
    </div>
  ),
}

/** `CardFooter layout`: end, between, stretch and stacked. */
export const FooterLayouts: Story = {
  name: 'Footer layouts',
  render: () => (
    <div className="sb-grid">
      {LAYOUTS.map((layout) => (
        <PrCard key={layout} orientation="vertical" ratio="3:1" layout={layout} media={false} />
      ))}
    </div>
  ),
}

/** `CardBanner` sits on top of the content: a refresh in progress or a failed load. */
export const Banners: Story = {
  render: () => (
    <div className="sb-grid">
      <Card>
        <CardBanner kind="refreshing">Refreshing checks…</CardBanner>
        <CardHeader>
          <CardTitle>CI · build-and-test</CardTitle>
          <CardDescription>12 of 14 jobs passed</CardDescription>
        </CardHeader>
      </Card>
      <Card>
        <CardBanner
          kind="error"
          action={
            <Button variant="plain" size="sm">
              Retry
            </Button>
          }
        >
          Could not reach GitHub.
        </CardBanner>
        <CardHeader>
          <CardTitle>CI · release</CardTitle>
          <CardDescription>Last updated 14 minutes ago</CardDescription>
        </CardHeader>
      </Card>
    </div>
  ),
}

/** `CardFeedback` replaces the content while loading, when empty, on error or without access. */
export const Feedback: Story = {
  render: () => (
    <div className="sb-grid">
      {FEEDBACK.map((kind) => (
        <Card key={kind}>
          <CardFeedback
            kind={kind}
            media={kind === 'loading'}
            loadingLabel="Loading deployments"
            title={{ loading: '', empty: 'No deployments yet', error: 'Deployments failed to load', denied: 'No access' }[kind]}
            description={
              {
                loading: '',
                empty: 'Push to main to trigger the first one.',
                error: 'The Vercel API returned 502.',
                denied: 'Ask an owner of flint-ide to grant you access.',
              }[kind]
            }
            action={kind === 'error' ? <Button size="sm">Retry</Button> : undefined}
          />
        </Card>
      ))}
    </div>
  ),
}

const TARGETS = [
  { id: 'staging', icon: <Server />, title: 'Staging', description: 'Deploy on every merge to main' },
  { id: 'production', icon: <Rocket />, title: 'Production', description: 'Deploy tagged releases only' },
  { id: 'preview', icon: <Workflow />, title: 'Preview', description: 'One environment per pull request' },
]

/** Selectable cards: `CardIndicator` draws the radio / checkbox, `CardTrigger` makes the whole card the target. */
export const SelectableCards: Story = {
  name: 'Selectable (indicator + trigger)',
  render: function Render() {
    const [target, setTarget] = useState('staging')
    const [extras, setExtras] = useState<string[]>(['preview'])
    const toggle = (id: string) => setExtras((xs) => (xs.includes(id) ? xs.filter((x) => x !== id) : [...xs, id]))
    return (
      <div className="sb-stack">
        <span className="sb-label">radio</span>
        <div className="sb-grid">
          {TARGETS.map((t) => (
            <Card key={t.id} interactive selected={target === t.id} style={{ position: 'relative' }}>
              <CardTrigger aria-label={t.title} aria-pressed={target === t.id} onClick={() => setTarget(t.id)} />
              <CardIndicator type="radio" checked={target === t.id} />
              <CardHeader leading={<CardIcon>{t.icon}</CardIcon>}>
                <CardTitle>{t.title}</CardTitle>
                <CardDescription>{t.description}</CardDescription>
              </CardHeader>
            </Card>
          ))}
        </div>
        <span className="sb-label">checkbox</span>
        <div className="sb-grid">
          {TARGETS.map((t) => (
            <Card key={t.id} interactive selected={extras.includes(t.id)} style={{ position: 'relative' }}>
              <CardTrigger aria-label={t.title} aria-pressed={extras.includes(t.id)} onClick={() => toggle(t.id)} />
              <CardIndicator type="checkbox" checked={extras.includes(t.id)} />
              <CardHeader>
                <CardTitle>{t.title}</CardTitle>
                <CardDescription>{t.description}</CardDescription>
              </CardHeader>
            </Card>
          ))}
        </div>
      </div>
    )
  },
}

/** `CardDragHandle` for reorderable boards; `dragging` shows the grabbed state. */
export const DragHandle: Story = {
  name: 'Drag handle',
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 380 }}>
      {['lint', 'test', 'build'].map((job, i) => (
        <Card key={job} size="sm">
          <CardHeader leading={<CardDragHandle dragging={i === 1} />}>
            <CardTitle>{job}</CardTitle>
            <CardDescription>{i === 1 ? 'Dragging…' : `Step ${i + 1} of the CI pipeline`}</CardDescription>
          </CardHeader>
          <CardContent>
            <Badge size="sm" color={i === 2 ? 'yellow' : 'green'} badgeStyle="light">
              {i === 2 ? 'queued' : 'passed'}
            </Badge>
          </CardContent>
        </Card>
      ))}
    </div>
  ),
}
