import type { Meta, StoryObj } from '@storybook/react-vite'
import { Badge, Tabs, TabsContent, TabsIndicator, TabsList, TabsTrigger } from '@wunderui/react'
import { FileDiff, GitCommitHorizontal, GitPullRequest } from 'lucide-react'

type Args = { variant: 'default' | 'line'; size: 'sm' | 'md' | 'lg'; shape: 'rounded' | 'pill' }

const meta = {
  title: 'Navigation/Tabs',
  args: { variant: 'default', size: 'sm', shape: 'rounded' },
  argTypes: {
    variant: { control: 'inline-radio', options: ['default', 'line'] },
    size: { control: 'inline-radio', options: ['sm', 'md', 'lg'] },
    shape: { control: 'inline-radio', options: ['rounded', 'pill'] },
  },
  render: ({ variant, size, shape }) => (
    <Tabs defaultValue="changes">
      <TabsList variant={variant} size={size} shape={shape}>
        <TabsTrigger value="changes">Changes</TabsTrigger>
        <TabsTrigger value="log">Log</TabsTrigger>
        <TabsTrigger value="prs">Pull Requests</TabsTrigger>
        <TabsTrigger value="stash" disabled>
          Stash
        </TabsTrigger>
        {variant === 'line' && <TabsIndicator />}
      </TabsList>
      <TabsContent value="changes">3 files changed</TabsContent>
      <TabsContent value="log">42 commits</TabsContent>
      <TabsContent value="prs">2 open</TabsContent>
    </Tabs>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Default: Story = {}
export const Line: Story = { args: { variant: 'line' } }
export const Pill: Story = { args: { shape: 'pill' } }

export const WithIconsAndCounts: Story = {
  render: () => (
    <Tabs defaultValue="changes">
      <TabsList variant="line">
        <TabsTrigger value="changes">
          <FileDiff /> Changes <Badge color="indigo" badgeStyle="light">3</Badge>
        </TabsTrigger>
        <TabsTrigger value="log">
          <GitCommitHorizontal /> Log
        </TabsTrigger>
        <TabsTrigger value="prs">
          <GitPullRequest /> Pull Requests <Badge color="grey" badgeStyle="light">2</Badge>
        </TabsTrigger>
        <TabsIndicator />
      </TabsList>
    </Tabs>
  ),
}
