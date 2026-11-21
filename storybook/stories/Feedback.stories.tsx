import type { Meta, StoryObj } from '@storybook/react-vite'
import { Button, Card, CardContent, CardDescription, CardHeader, CardTitle, EmptyState, Progress, Separator, Skeleton, Spinner } from '@wunderui/react'
import { GitPullRequest } from 'lucide-react'

const meta = { title: 'Feedback/Loading & Surfaces' } satisfies Meta

export default meta
type Story = StoryObj<typeof meta>

export const Loading: Story = {
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 420 }}>
      <Progress value={64} />
      <Progress value={null} />
      <div className="sb-row">
        <Spinner /> Indexing workspace…
      </div>
      <Skeleton style={{ height: 12, width: '100%' }} />
      <Skeleton style={{ height: 12, width: '60%' }} />
    </div>
  ),
}

export const Cards: Story = {
  render: () => (
    <div className="sb-grid">
      {(['default', 'elevated', 'outlined'] as const).map((variant) => (
        <Card key={variant} variant={variant} size="sm">
          <CardHeader>
            <CardTitle>{variant}</CardTitle>
            <CardDescription>Card variant</CardDescription>
          </CardHeader>
          <CardContent>Run configurations, pull requests, extensions.</CardContent>
        </Card>
      ))}
    </div>
  ),
}

export const Empty: Story = {
  render: () => (
    <div className="sb-stack">
      {(['default', 'outline', 'dashed'] as const).map((variant) => (
        <EmptyState
          key={variant}
          variant={variant}
          icon={<GitPullRequest />}
          title="No pull requests"
          description="Pull requests you open or review show up here."
          action={<Button variant="plain">Sign in to GitHub</Button>}
        />
      ))}
      <Separator />
    </div>
  ),
}
