import type { Meta, StoryObj } from '@storybook/react-vite'
import { Button, InputGroup, InputGroupAddon, Kbd } from '@wunderui/react'
import { Eye, GitBranch, Search, Terminal } from 'lucide-react'

const meta = {
  title: 'Forms/Input Group',
  component: InputGroup,
} satisfies Meta<typeof InputGroup>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {
  render: (args) => (
    <div style={{ maxWidth: 420 }}>
      <InputGroup {...args}>
        <InputGroupAddon>
          <Search />
        </InputGroupAddon>
        <input placeholder="Search files…" aria-label="Search files" />
        <InputGroupAddon>
          <Kbd>⌘P</Kbd>
        </InputGroupAddon>
      </InputGroup>
    </div>
  ),
}

export const Examples: Story = {
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 420 }}>
      <div className="sb-row">
        <span className="sb-label">search</span>
        <InputGroup style={{ flex: 1 }}>
          <InputGroupAddon>
            <Search />
          </InputGroupAddon>
          <input placeholder="Search settings…" aria-label="Search settings" />
        </InputGroup>
      </div>
      <div className="sb-row">
        <span className="sb-label">url prefix</span>
        <InputGroup style={{ flex: 1 }}>
          <InputGroupAddon>https://</InputGroupAddon>
          <input defaultValue="github.com/flint-ide/flint" aria-label="Repository URL" />
        </InputGroup>
      </div>
      <div className="sb-row">
        <span className="sb-label">port suffix</span>
        <InputGroup style={{ flex: 1 }}>
          <input defaultValue="localhost" aria-label="Host" />
          <InputGroupAddon>:5432</InputGroupAddon>
        </InputGroup>
      </div>
      <div className="sb-row">
        <span className="sb-label">icon + button</span>
        <InputGroup style={{ flex: 1 }}>
          <InputGroupAddon>
            <GitBranch />
          </InputGroupAddon>
          <input defaultValue="feature/ui-kit" aria-label="Branch name" />
          <InputGroupAddon>
            <Button variant="plain" size="sm">
              Checkout
            </Button>
          </InputGroupAddon>
        </InputGroup>
      </div>
      <div className="sb-row">
        <span className="sb-label">password</span>
        <InputGroup style={{ flex: 1 }}>
          <input type="password" defaultValue="hunter2" aria-label="Database password" />
          <InputGroupAddon>
            <Eye />
          </InputGroupAddon>
        </InputGroup>
      </div>
      <div className="sb-row">
        <span className="sb-label">read-only</span>
        <InputGroup style={{ flex: 1 }}>
          <InputGroupAddon>
            <Terminal />
          </InputGroupAddon>
          <input readOnly defaultValue="/bin/zsh" aria-label="Shell path" />
        </InputGroup>
      </div>
      <div className="sb-row">
        <span className="sb-label">invalid</span>
        <InputGroup style={{ flex: 1 }}>
          <InputGroupAddon>:</InputGroupAddon>
          <input aria-invalid defaultValue="99999" aria-label="Port" />
        </InputGroup>
      </div>
    </div>
  ),
}
