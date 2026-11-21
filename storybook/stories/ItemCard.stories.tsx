import type { Meta, StoryObj } from '@storybook/react-vite'
import { Badge, Button, IconButton, ItemCard, ItemCardGroup } from '@wunderui/react'
import { Bot, Database, GitPullRequest, MoreHorizontal, Server, Sparkles } from 'lucide-react'

const meta = {
  title: 'Data Display/Item Card',
  component: ItemCard,
  args: {
    icon: <GitPullRequest />,
    title: 'feat: Storybook for WunderUI',
    description: 'flint-ide/flint#76 · opened by Ada Lovelace',
    meta: '2h ago',
  },
  argTypes: {
    icon: { control: false },
    action: { control: false },
    title: { control: 'text' },
    description: { control: 'text' },
    meta: { control: 'text' },
  },
  decorators: [
    (Story) => (
      <div style={{ maxWidth: 520 }}>
        <Story />
      </div>
    ),
  ],
} satisfies Meta<typeof ItemCard>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const WithAction: Story = {
  name: 'With action',
  args: {
    icon: <Bot />,
    title: 'Claude Code — refactor settings store',
    description: 'Waiting for your approval to run go test ./...',
    meta: (
      <Badge color="yellow" badgeStyle="light" shape="pill">
        needs input
      </Badge>
    ),
    action: (
      <Button size="sm" variant="plain">
        Open
      </Button>
    ),
  },
}

export const Group: Story = {
  render: () => (
    <ItemCardGroup>
      <ItemCard
        icon={<Bot />}
        title="Claude Code"
        description="Refactor settings store into slices"
        meta={
          <Badge color="green" badgeStyle="light" shape="pill">
            done
          </Badge>
        }
        action={
          <IconButton variant="ghost" aria-label="More">
            <MoreHorizontal />
          </IconButton>
        }
      />
      <ItemCard
        icon={<Sparkles />}
        title="Codex"
        description="Add keyboard tests for FileTree"
        meta={
          <Badge color="yellow" badgeStyle="light" shape="pill">
            running
          </Badge>
        }
        action={
          <IconButton variant="ghost" aria-label="More">
            <MoreHorizontal />
          </IconButton>
        }
      />
      <ItemCard icon={<Database />} title="postgres-0" description="agent_sessions · 48,917 rows" meta="412 MB" />
      <ItemCard icon={<Server />} title="flint-api-7d9f8c6b5-x2kqz" description="namespace flint · node pool default" meta="Running" />
    </ItemCardGroup>
  ),
}
