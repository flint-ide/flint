import type { Meta, StoryObj } from '@storybook/react-vite'
import { useState } from 'react'
import {
  Breadcrumb,
  BreadcrumbEllipsis,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbLoading,
  BreadcrumbPage,
  BreadcrumbRestricted,
  BreadcrumbSeparator,
  BreadcrumbSiblingTrigger,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  type BreadcrumbDensity,
  type BreadcrumbVariant,
} from '@wunderui/react'

const VARIANTS: BreadcrumbVariant[] = ['plain', 'contained', 'border', 'underline']
const DENSITIES: BreadcrumbDensity[] = ['default', 'compact']

type Args = { variant: BreadcrumbVariant; density: BreadcrumbDensity; separator: 'chevron' | 'slash' }

/** The newer Breadcrumb parts: ellipsis, sibling switcher, restricted and loading segments, density and separator type. */
const meta = {
  title: 'Navigation/Breadcrumb Extras',
  args: { variant: 'plain', density: 'default', separator: 'chevron' },
  argTypes: {
    variant: { control: 'inline-radio', options: VARIANTS },
    density: { control: 'inline-radio', options: DENSITIES },
    separator: { control: 'inline-radio', options: ['chevron', 'slash'] },
  },
  decorators: [(Story) => <div style={{ minHeight: 200 }}><Story /></div>],
  render: ({ variant, density, separator }) => (
    <Breadcrumb variant={variant} density={density}>
      <BreadcrumbList>
        <BreadcrumbItem>
          <BreadcrumbLink href="#">flint</BreadcrumbLink>
        </BreadcrumbItem>
        <BreadcrumbSeparator type={separator} />
        <BreadcrumbItem>
          <BreadcrumbEllipsis aria-label="Show 3 hidden folders" />
        </BreadcrumbItem>
        <BreadcrumbSeparator type={separator} />
        <BreadcrumbItem>
          <BreadcrumbLink href="#">components</BreadcrumbLink>
        </BreadcrumbItem>
        <BreadcrumbSeparator type={separator} />
        <BreadcrumbItem>
          <BreadcrumbPage>Editor.tsx</BreadcrumbPage>
        </BreadcrumbItem>
      </BreadcrumbList>
    </Breadcrumb>
  ),
} satisfies Meta<Args>

export default meta
type Story = StoryObj<typeof meta>

/** BreadcrumbEllipsis collapses the middle of a deep path. */
export const Collapsed: Story = {}

export const VariantsAndDensity: Story = {
  render: ({ separator }) => (
    <div className="sb-stack">
      {DENSITIES.flatMap((density) =>
        VARIANTS.map((variant) => (
          <div key={`${density}-${variant}`} className="sb-row">
            <span className="sb-label" style={{ width: 140 }}>
              {variant} · {density}
            </span>
            <Breadcrumb variant={variant} density={density}>
              <BreadcrumbList>
                <BreadcrumbItem>
                  <BreadcrumbLink href="#">flint-ide</BreadcrumbLink>
                </BreadcrumbItem>
                <BreadcrumbSeparator type={separator} />
                <BreadcrumbItem>
                  <BreadcrumbLink href="#">flint</BreadcrumbLink>
                </BreadcrumbItem>
                <BreadcrumbSeparator type={separator} />
                <BreadcrumbItem>
                  <BreadcrumbPage>Pull request #76</BreadcrumbPage>
                </BreadcrumbItem>
              </BreadcrumbList>
            </Breadcrumb>
          </div>
        )),
      )}
    </div>
  ),
}

/** The ellipsis as a menu trigger listing the hidden folders. */
export const EllipsisMenu: Story = {
  render: ({ variant, density, separator }) => (
    <Breadcrumb variant={variant} density={density}>
      <BreadcrumbList>
        <BreadcrumbItem>
          <BreadcrumbLink href="#">flint</BreadcrumbLink>
        </BreadcrumbItem>
        <BreadcrumbSeparator type={separator} />
        <BreadcrumbItem>
          <DropdownMenu>
            <DropdownMenuTrigger render={<BreadcrumbEllipsis aria-label="Show hidden folders" />} />
            <DropdownMenuContent align="start">
              {['frontend', 'src', 'components'].map((f) => (
                <DropdownMenuItem key={f}>{f}</DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        </BreadcrumbItem>
        <BreadcrumbSeparator type={separator} />
        <BreadcrumbItem>
          <BreadcrumbPage>Editor.tsx</BreadcrumbPage>
        </BreadcrumbItem>
      </BreadcrumbList>
    </Breadcrumb>
  ),
}

/** BreadcrumbSiblingTrigger: a chevron next to the current segment that switches to a sibling (another branch). */
export const SiblingSwitcher: Story = {
  render: function Render({ variant, density, separator }) {
    const branches = ['main', 'feat/ui-kit', 'fix/symlink-drafts']
    const [branch, setBranch] = useState('feat/ui-kit')
    return (
      <Breadcrumb variant={variant} density={density}>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink href="#">flint</BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator type={separator} />
          <BreadcrumbItem>
            <BreadcrumbPage>{branch}</BreadcrumbPage>
            <DropdownMenu>
              <DropdownMenuTrigger render={<BreadcrumbSiblingTrigger aria-label="Switch branch" />} />
              <DropdownMenuContent align="start">
                {branches.map((b) => (
                  <DropdownMenuItem key={b} onClick={() => setBranch(b)}>
                    {b}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>
    )
  },
}

/** BreadcrumbRestricted for a segment the user cannot open; BreadcrumbLoading while a segment resolves. */
export const RestrictedAndLoading: Story = {
  render: ({ variant, density, separator }) => (
    <div className="sb-stack">
      <Breadcrumb variant={variant} density={density}>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink href="#">flint-ide</BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator type={separator} />
          <BreadcrumbItem>
            <BreadcrumbRestricted title="You don't have access to this repository">infra-secrets</BreadcrumbRestricted>
          </BreadcrumbItem>
          <BreadcrumbSeparator type={separator} />
          <BreadcrumbItem>
            <BreadcrumbPage>deploy.yml</BreadcrumbPage>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>
      <Breadcrumb variant={variant} density={density}>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink href="#">flint</BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator type={separator} />
          <BreadcrumbItem>
            <BreadcrumbLoading label="Loading folder" />
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>
    </div>
  ),
}

/** Long names truncate with `truncated` (the full name stays in the title). */
export const Truncated: Story = {
  render: ({ variant, density, separator }) => (
    <div style={{ maxWidth: 360 }}>
      <Breadcrumb variant={variant} density={density}>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink href="#" truncated>
              claude/project-feature-proposals-a5fvh9
            </BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator type={separator} />
          <BreadcrumbItem>
            <BreadcrumbPage truncated>review-drafts-read-the-working-tree.md</BreadcrumbPage>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>
    </div>
  ),
}
