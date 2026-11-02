// The database feature's commands: SQL consoles (scratchpads of kind "sql"), running
// and explaining their statements, query history and new connections. Ids predate the
// feature, so keymap overrides keep working.
import type { Command } from '../../lib/commands'
import { pressInEditor } from '../../commands/pressInEditor'
import { useEditor, viewFor } from '../../store/editor'
import { useScratch } from '../../store/scratch'
import { useWorkspace } from '../../store/workspace'
import { useDatabase } from './store/database'
import { activeSqlConsole } from './store/sqlConsole'

const inSqlConsole = (): boolean => {
  const { tabs, activeId } = useEditor.getState()
  const tab = tabs.find((t) => t.id === activeId)
  return tab?.kind === 'scratch' && tab.scratchKind === 'sql' && !!activeId && !!viewFor(activeId)
}

export const databaseCommands: readonly Command[] = [
  { id: 'scratch.newSql', title: 'New SQL Console', category: 'Database', keys: 'Mod+N', keywords: 'scratch query', run: () => void useScratch.getState().create('sql') },
  {
    id: 'sql.runQuery', title: 'Run Query', category: 'Database', hintKeys: 'Mod+Enter', keywords: 'execute statement',
    when: inSqlConsole,
    run: () => pressInEditor('Mod+Enter'),
  },
  {
    id: 'sql.explain', title: 'Explain Plan', category: 'Database', keys: 'Mod+Shift+E', keywords: 'query plan explain',
    when: () => !!activeSqlConsole(),
    run: () => activeSqlConsole()?.explain(false),
  },
  {
    id: 'sql.explainAnalyze', title: 'Explain Analyze', category: 'Database', keys: 'Mod+Shift+A',
    keywords: 'query plan explain execute timing buffers pipeline',
    when: () => !!activeSqlConsole(),
    run: () => activeSqlConsole()?.explain(true),
  },
  {
    id: 'sql.history', title: 'Query History…', category: 'Database', keys: 'Mod+Shift+H', keywords: 'recent statements log rerun',
    when: () => !!activeSqlConsole(),
    run: () => activeSqlConsole()?.showHistory(),
  },
  {
    id: 'db.newConnection', title: 'New Database Connection…', category: 'Database', keywords: 'postgres clickhouse add',
    run: () => {
      useWorkspace.getState().setSidebar('database')
      useDatabase.getState().setEditing('new')
    },
  },
]
