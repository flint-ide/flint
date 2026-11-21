import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { RichTextEditor, RichTextEditorSkeleton } from '@wunderui/react'

const PR_DESCRIPTION = `<h2>Summary</h2>
<p>Adds a <strong>Storybook</strong> package that renders WunderUI components with Flint's theme tokens.</p>
<ul>
  <li>New <code>storybook/</code> workspace with React 19 and Vite</li>
  <li>Stories for buttons, forms and feedback components</li>
</ul>
<h2>Test plan</h2>
<ol>
  <li>Run <code>task storybook</code></li>
  <li>Switch between light and dark themes</li>
</ol>`

const meta = {
  title: 'Forms/Rich Text Editor',
  component: RichTextEditor,
  args: { placeholder: 'Describe your changes…', content: PR_DESCRIPTION },
  argTypes: {
    placeholder: { control: 'text' },
    content: { control: 'text' },
  },
} satisfies Meta<typeof RichTextEditor>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {
  render: (args) => (
    <div style={{ maxWidth: 680 }}>
      <RichTextEditor key={args.content} {...args} />
    </div>
  ),
}

export const PullRequestDescription: Story = {
  name: 'Pull request description',
  render: function Render() {
    const [html, setHtml] = useState(PR_DESCRIPTION)
    return (
      <div className="sb-stack" style={{ maxWidth: 680 }}>
        <RichTextEditor content={PR_DESCRIPTION} onChange={setHtml} placeholder="Describe your changes…" />
        <span className="sb-label" style={{ width: 'auto' }}>
          {html.length} characters of HTML
        </span>
      </div>
    )
  },
}

export const Empty: Story = {
  render: () => (
    <div style={{ maxWidth: 680 }}>
      <RichTextEditor placeholder="Write a release note…" />
    </div>
  ),
}

export const Skeleton: Story = {
  name: 'RichTextEditorSkeleton',
  render: () => (
    <div style={{ maxWidth: 680 }}>
      <RichTextEditorSkeleton placeholder="Loading editor…" />
    </div>
  ),
}
