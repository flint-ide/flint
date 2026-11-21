import type { Meta, StoryObj } from '@storybook/react-vite'
import { useMemo, useState } from 'react'
import { Input } from '@wunderui/react'
import { icons } from 'lucide-react'

type Args = { size: number; strokeWidth: number }

/** Every Lucide icon (lucide-react). Search by name; import as `import { GitBranch } from 'lucide-react'`. */
const meta = {
  title: 'Foundations/Icons',
  args: { size: 18, strokeWidth: 1.75 },
  argTypes: {
    size: { control: { type: 'range', min: 12, max: 32, step: 1 } },
    strokeWidth: { control: { type: 'range', min: 1, max: 2.5, step: 0.25 } },
  },
  render: function Render({ size, strokeWidth }) {
    const [query, setQuery] = useState('')
    const shown = useMemo(() => {
      const q = query.trim().toLowerCase()
      return Object.entries(icons).filter(([name]) => !q || name.toLowerCase().includes(q))
    }, [query])
    return (
      <div>
        <Input className="sb-search" placeholder={`Search ${Object.keys(icons).length} icons…`} value={query} onChange={(e) => setQuery(e.target.value)} />
        <div className="sb-icons">
          {shown.slice(0, 600).map(([name, LucideIcon]) => (
            <div key={name} className="sb-icon" title={name}>
              <LucideIcon size={size} strokeWidth={strokeWidth} />
              <span>{name}</span>
            </div>
          ))}
        </div>
      </div>
    )
  },
} satisfies Meta<Args>

export default meta
export const AllIcons: StoryObj<typeof meta> = {}
