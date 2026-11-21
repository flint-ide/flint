import type { Meta, StoryObj } from '@storybook/react-vite'
import { Fragment } from 'react'
import { Breadcrumb, BreadcrumbItem, BreadcrumbLink, BreadcrumbList, BreadcrumbPage, BreadcrumbSeparator } from '@wunderui/react'
import { FileCode, Folder } from 'lucide-react'

const VARIANTS = ['plain', 'border', 'underline'] as const
const PATH = ['flint', 'internal', 'git', 'diff.go']

type Args = { variant: (typeof VARIANTS)[number]; path: string }

function FilePath({ segments, variant, icons = false }: { segments: string[]; variant?: Args['variant']; icons?: boolean }) {
  const parents = segments.slice(0, -1)
  const file = segments[segments.length - 1]
  return (
    <Breadcrumb variant={variant}>
      <BreadcrumbList>
        {parents.map((segment) => (
          <Fragment key={segment}>
            <BreadcrumbItem>
              <BreadcrumbLink href="#">
                {icons && <Folder size={14} style={{ marginRight: 4, verticalAlign: -2 }} />}
                {segment}
              </BreadcrumbLink>
            </BreadcrumbItem>
            <BreadcrumbSeparator />
          </Fragment>
        ))}
        <BreadcrumbItem>
          <BreadcrumbPage>
            {icons && <FileCode size={14} style={{ marginRight: 4, verticalAlign: -2 }} />}
            {file}
          </BreadcrumbPage>
        </BreadcrumbItem>
      </BreadcrumbList>
    </Breadcrumb>
  )
}

/** The path of the open file above the editor; every folder is a link, the file is the current page. */
const meta = {
  title: 'Navigation/Breadcrumb',
  args: { variant: 'plain', path: PATH.join('/') },
  argTypes: {
    variant: { control: 'inline-radio', options: VARIANTS },
    path: { control: 'text', description: 'Slash-separated path' },
  },
  render: ({ variant, path }) => <FilePath variant={variant} segments={path.split('/').filter(Boolean)} />,
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

export const Variants: Story = {
  render: () => (
    <div className="sb-stack">
      {VARIANTS.map((variant) => (
        <div key={variant} className="sb-row">
          <span className="sb-label">{variant}</span>
          <FilePath variant={variant} segments={PATH} />
        </div>
      ))}
    </div>
  ),
}

export const WithIcons: Story = {
  render: () => <FilePath segments={['flint', 'frontend', 'src', 'components', 'Editor.tsx']} icons />,
}

/** A custom separator: pass children to BreadcrumbSeparator. */
export const SlashSeparator: Story = {
  render: () => (
    <Breadcrumb>
      <BreadcrumbList>
        <BreadcrumbItem>
          <BreadcrumbLink href="#">flint-ide</BreadcrumbLink>
        </BreadcrumbItem>
        <BreadcrumbSeparator>/</BreadcrumbSeparator>
        <BreadcrumbItem>
          <BreadcrumbLink href="#">flint</BreadcrumbLink>
        </BreadcrumbItem>
        <BreadcrumbSeparator>/</BreadcrumbSeparator>
        <BreadcrumbItem>
          <BreadcrumbPage>Pull request #76</BreadcrumbPage>
        </BreadcrumbItem>
      </BreadcrumbList>
    </Breadcrumb>
  ),
}
