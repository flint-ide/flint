import { fileURLToPath } from 'node:url'
import type { StorybookConfig } from '@storybook/react-vite'

const ui = (path: string) => fileURLToPath(new URL(`../../ui/${path}`, import.meta.url))

const config: StorybookConfig = {
  framework: '@storybook/react-vite',
  stories: ['../stories/**/*.mdx', '../stories/**/*.stories.tsx'],
  addons: ['@storybook/addon-docs', '@storybook/addon-a11y'],
  core: { disableTelemetry: true },
  // Stories render @flint-ide/ui (../ui, source without node_modules) and Flint's
  // own components from ../frontend/src. Their imports must resolve to this
  // package's copies: one React (or hooks break), one WunderUI, one Lucide.
  viteFinal: async (cfg) => ({
    ...cfg,
    resolve: {
      ...cfg.resolve,
      alias: [
        { find: /^@flint-ide\/ui$/, replacement: ui('src/index.ts') },
        { find: /^@flint-ide\/ui\/icons$/, replacement: ui('src/icons.ts') },
        { find: /^@flint-ide\/ui\/styles\.css$/, replacement: ui('css/index.css') },
      ],
      dedupe: ['react', 'react-dom', '@wunderui/react', 'lucide-react'],
    },
  }),
}

export default config
