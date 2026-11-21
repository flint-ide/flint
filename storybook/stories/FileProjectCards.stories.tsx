import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import { BrandLogo, FileCard, ITEM_CARD_TINT, ItemCardCover, ItemCardFile, ProjectCard, type ItemCardTint } from '@wunderui/react'
import { Braces, FileCode, FileText, Folder, Image, Package, Terminal } from 'lucide-react'

const TINTS = Object.keys(ITEM_CARD_TINT) as ItemCardTint[]

/** A thumbnail rendered inline so the story needs no network. */
const THUMB =
  'data:image/svg+xml;utf8,' +
  encodeURIComponent(
    '<svg xmlns="http://www.w3.org/2000/svg" width="320" height="200"><defs><linearGradient id="g" x1="0" x2="1" y1="0" y2="1"><stop offset="0" stop-color="#555CF3"/><stop offset="1" stop-color="#A584F3"/></linearGradient></defs><rect width="320" height="200" fill="url(#g)"/><text x="24" y="110" font-family="monospace" font-size="22" fill="#fff">flint.png</text></svg>',
  )

/** Cover cards for projects and file cards for workspace files. */
const meta = {
  title: 'Data Display/File & Project Cards',
  component: ItemCardFile,
  args: { name: 'main.go', meta: 'cmd/flint · 4.2 KB · edited 5m ago', color: 'blue', selected: false },
  argTypes: {
    color: { control: 'select', options: TINTS },
    icon: { control: false },
  },
  render: function Render(args) {
    const [selected, setSelected] = useState(args.selected ?? false)
    return (
      <div style={{ maxWidth: 260 }}>
        <ItemCardFile {...args} icon={<FileCode />} selected={selected} onSelectedChange={setSelected} onMenu={() => {}} />
      </div>
    )
  },
} satisfies Meta<typeof ItemCardFile>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** Workspace files in every tint, plus an image thumbnail. */
export const Files: Story = {
  render: function Render() {
    const [selected, setSelected] = useState<string[]>(['Taskfile.yml'])
    const files = [
      { name: 'main.go', meta: 'cmd/flint · 4.2 KB', icon: <FileCode />, color: 'blue' },
      { name: 'package.json', meta: 'frontend · 2.1 KB', icon: <Braces />, color: 'yellow' },
      { name: 'Taskfile.yml', meta: 'root · 1.3 KB', icon: <Terminal />, color: 'green' },
      { name: 'DEVELOPMENT.md', meta: 'docs · 8.7 KB', icon: <FileText />, color: 'alternative' },
      { name: 'wuicss', meta: 'cmd · 6 files', icon: <Folder />, color: 'purple' },
    ] as const
    return (
      <div className="sb-grid">
        {files.map((f) => (
          <ItemCardFile
            key={f.name}
            {...f}
            selected={selected.includes(f.name)}
            onSelectedChange={(on) => setSelected((xs) => (on ? [...xs, f.name] : xs.filter((x) => x !== f.name)))}
            onMenu={() => {}}
            onOpen={() => {}}
          />
        ))}
        <ItemCardFile name="flint.png" meta="docs/assets · 84 KB" thumbnail={THUMB} onMenu={() => {}} />
      </div>
    )
  },
}

/** `deletedAt` turns the card into its trash variant: a red deletion line, no menu or checkbox. */
export const Trash: Story = {
  render: () => (
    <div className="sb-grid">
      <ItemCardFile name="old-config.json" icon={<Braces />} color="yellow" deletedAt="Deleted 2 days ago" />
      <ItemCardFile name="legacy-ui" icon={<Folder />} color="purple" deletedAt="Deleted by Ada · 29 days left" />
    </div>
  ),
}

/** `ItemCardCover`: a tinted cover band with a logo, the project name and one meta line. */
export const ProjectCovers: Story = {
  name: 'Project covers',
  render: () => (
    <div className="sb-grid">
      <ItemCardCover name="flint" meta="Go · updated 2h ago" color="indigo" cover={<BrandLogo name="GitHub" size="lg" />} onClick={() => {}} />
      <ItemCardCover name="flint-docs" meta="Markdown · updated yesterday" color="green" cover={<FileText />} onClick={() => {}} />
      <ItemCardCover name="wunderui" meta="TypeScript · 3 open PRs" color="pink" cover={<Package />} onClick={() => {}} />
      <ItemCardCover name="design-assets" meta="Figma · synced" color="orange" cover={<BrandLogo name="Figma" size="lg" />} />
    </div>
  ),
}

/** Every `ITEM_CARD_TINT` on a cover. */
export const Tints: Story = {
  render: () => (
    <div className="sb-grid">
      {TINTS.map((tint) => (
        <ItemCardCover key={tint} name={tint} meta="ITEM_CARD_TINT" color={tint} cover={<Image />} />
      ))}
    </div>
  ),
}

/** `ProjectCard` and `FileCard` are deprecated aliases of `ItemCardCover` / `ItemCardFile` and render the same. */
export const DeprecatedAliases: Story = {
  name: 'Deprecated aliases',
  render: () => (
    <div className="sb-grid">
      <ProjectCard name="flint (ProjectCard)" meta="alias of ItemCardCover" color="blue" cover={<Package />} />
      <FileCard name="go.mod (FileCard)" meta="alias of ItemCardFile" icon={<FileCode />} color="green" onMenu={() => {}} />
    </div>
  ),
}
