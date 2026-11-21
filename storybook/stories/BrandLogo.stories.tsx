import type { Meta, StoryObj } from '@storybook/react-vite'
import { BRANDS, BRAND_NAMES, BrandLogo, BrandMark, Button, Switch } from '@wunderui/react'

const SIZES = ['sm', 'md', 'lg'] as const

const card: React.CSSProperties = {
  display: 'flex',
  gap: 10,
  alignItems: 'flex-start',
  padding: 12,
  borderRadius: 8,
  border: '1px solid var(--border)',
  background: 'var(--bg-raised)',
}

/** Logos of products Flint integrates with, at 16, 20 or 24 px. */
const meta = {
  title: 'Data Display/Brand Logo',
  component: BrandLogo,
  args: { name: 'GitHub', size: 'md' },
  argTypes: {
    name: { control: 'select', options: BRAND_NAMES },
    size: { control: 'inline-radio', options: SIZES },
    label: { control: 'text' },
  },
} satisfies Meta<typeof BrandLogo>

export default meta
type Story = StoryObj<typeof meta>

export const Playground: Story = {}

/** Every brand with its category and description from `BRANDS`. */
export const AllBrands: Story = {
  name: 'All brands',
  render: (args) => (
    <div className="sb-grid">
      {BRANDS.map((brand) => (
        <div key={brand.name} style={card}>
          <BrandLogo name={brand.name} size={args.size} label="" />
          <div style={{ minWidth: 0 }}>
            <div style={{ fontSize: 13, fontWeight: 600 }}>{brand.name}</div>
            <div style={{ fontSize: 11, color: 'var(--fg-muted)' }}>{brand.category}</div>
            <div style={{ fontSize: 12, color: 'var(--fg-muted)', marginTop: 4 }}>{brand.description}</div>
          </div>
        </div>
      ))}
    </div>
  ),
}

/** The three sizes next to each other. One-colour marks (GitHub, OpenAI, Linear, Vercel) follow the text colour. */
export const Sizes: Story = {
  render: () => (
    <div className="sb-stack">
      {SIZES.map((size) => (
        <div key={size} className="sb-row" style={{ gap: 12 }}>
          <span className="sb-label">{size}</span>
          {(['GitHub', 'Slack', 'Linear', 'Vercel', 'Claude', 'OpenAI', 'Jira', 'Figma'] as const).map((name) => (
            <BrandLogo key={name} name={name} size={size} />
          ))}
        </div>
      ))}
    </div>
  ),
}

/** `BrandMark` is the bare SVG mark on a 20 × 20 grid: size it with width/height. */
export const Marks: Story = {
  render: () => (
    <div className="sb-row" style={{ gap: 16 }}>
      {BRAND_NAMES.map((name) => (
        <BrandMark key={name} name={name} width={32} height={32} aria-label={name} />
      ))}
    </div>
  ),
}

/** In context: the integrations settings page. */
export const Integrations: Story = {
  render: () => (
    <div className="sb-stack" style={{ maxWidth: 480, gap: 0 }}>
      {(
        [
          ['GitHub', 'Pull requests, reviews and checks for flint-ide/flint', true],
          ['Slack', 'Post CI failures to #flint-dev', true],
          ['Linear', 'Link branches to issues', false],
          ['Jira', 'Show ticket status next to the branch name', false],
        ] as const
      ).map(([name, description, on]) => (
          <div
            key={name}
            className="sb-row"
            style={{ gap: 12, padding: '12px 0', borderBottom: '1px solid var(--border)', flexWrap: 'nowrap' }}
          >
            <BrandLogo name={name} size="lg" label="" />
            <div style={{ flex: 1 }}>
              <div style={{ fontSize: 13, fontWeight: 600 }}>{name}</div>
              <div style={{ fontSize: 12, color: 'var(--fg-muted)' }}>{description}</div>
            </div>
            <Switch defaultChecked={on} aria-label={`Enable ${name}`} />
          </div>
        ))}
      <div className="sb-row" style={{ paddingTop: 12 }}>
        <Button variant="plain" size="sm">
          <BrandLogo name="Claude" size="sm" label="" />
          Connect Claude
        </Button>
      </div>
    </div>
  ),
}
