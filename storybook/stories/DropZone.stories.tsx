import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { DropZone } from '@wunderui/react'

const meta = {
  title: 'Forms/Drop Zone',
  component: DropZone,
  args: { accept: '', hint: 'Any file type, up to 10 MB' },
  argTypes: {
    accept: { control: 'text' },
    hint: { control: 'text' },
  },
} satisfies Meta<typeof DropZone>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {
  render: (args) => (
    <div style={{ maxWidth: 480 }}>
      <DropZone {...args} accept={args.accept || undefined} />
    </div>
  ),
}

export const ImportSettings: Story = {
  name: 'Import settings',
  render: function Render() {
    const [files, setFiles] = useState<string[]>([])
    return (
      <div className="sb-stack" style={{ maxWidth: 480 }}>
        <DropZone
          accept=".json,.jsonc"
          hint="settings.json or keybindings.json from VS Code, up to 1 MB"
          onFiles={(list) => setFiles(Array.from(list, (f) => `${f.name} (${Math.ceil(f.size / 1024)} KB)`))}
        />
        {files.length > 0 && (
          <ul className="sb-label" style={{ width: 'auto', margin: 0, paddingLeft: 16 }}>
            {files.map((f) => (
              <li key={f}>{f}</li>
            ))}
          </ul>
        )}
      </div>
    )
  },
}

export const SshKey: Story = {
  name: 'SSH key',
  render: () => (
    <div style={{ maxWidth: 480 }}>
      <DropZone accept=".pub" hint="Public key (.pub) used to sign commits" />
    </div>
  ),
}
