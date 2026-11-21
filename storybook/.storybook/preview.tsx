import type { Preview } from '@storybook/react-vite'
import { applyTheme } from '@flint-ide/ui'
import { useEffect } from 'react'
// The same CSS the app loads, in the same order (frontend/src/main.tsx).
import '@flint-ide/ui/styles.css'
import '../../frontend/src/style.css'
import './preview.css'

type Theme = 'dark' | 'light'

const preview: Preview = {
  globalTypes: {
    theme: {
      description: 'Flint theme',
      toolbar: {
        title: 'Theme',
        icon: 'mirror',
        items: [
          { value: 'dark', title: 'Dark', icon: 'moon' },
          { value: 'light', title: 'Light', icon: 'sun' },
        ],
        dynamicTitle: true,
      },
    },
  },
  initialGlobals: { theme: 'dark' },
  decorators: [
    (Story, context) => {
      const theme = (context.globals.theme ?? 'dark') as Theme
      useEffect(() => applyTheme(theme), [theme])
      applyTheme(theme)
      return <Story />
    },
  ],
  parameters: {
    layout: 'padded',
    options: {
      storySort: {
        order: ['Introduction', 'Foundations', 'Actions', 'Navigation', 'Forms', 'Data Display', 'Feedback', 'Overlays', 'Layout', 'Flint'],
      },
    },
    controls: { expanded: true, sort: 'requiredFirst' },
    backgrounds: { disable: true },
    a11y: { test: 'todo' },
  },
}

export default preview
