// The Database feature (Go: internal/features/database): the Databases panel
// (PostgreSQL and ClickHouse connections, schema tree and its menus), SQL consoles'
// query panel (results grid with local sort and filters, row editing, EXPLAIN,
// history) and their commands. SQL consoles are scratchpads (core): with this
// feature off they open as plain SQL text.
import { lazyPreload } from '../../lib/lazyPreload'
import { defineFeature } from '../../lib/features'
import { databaseCommands } from './commands'
import { useDatabase } from './store/database'

// Not parsed at startup; fetched in the background once the app is idle (or when first opened).
const DatabasePanel = lazyPreload(() => import('./sidebar/DatabasePanel').then((m) => ({ default: m.DatabasePanel })))

export default defineFeature({
  id: 'database',
  optional: true,
  name: 'Database',
  description: 'PostgreSQL and ClickHouse: connections, schema browser, SQL consoles with autocomplete, result grid with local filters, row editing and EXPLAIN.',
  panels: [
    {
      id: 'database', title: 'Databases', icon: 'database', order: 50, component: DatabasePanel,
      command: { id: 'view.databases', keywords: 'sql postgres clickhouse' },
    },
  ],
  commands: databaseCommands,
  activate: () => void useDatabase.getState().loadConnections(),
})
