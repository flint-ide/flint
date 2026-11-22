import { useEffect, useRef, useState } from 'react'
import { createScratchFile, deleteScratchFile, renameScratchFile, useScratch, type ScratchFile } from '../../store/scratch'
import { SCRATCH_FILE_TYPES } from '../../lib/scratchTypes'
import { Dropdown, type DropdownItem } from '../ui/Dropdown'
import { FileIcon, TabKindIcon } from '../icons/FileIcon'
import { useEditor } from '../../store/editor'
import { useDatabase } from '../../features/database/store/database'
import { confirmAction } from '../../store/dialog'
import type { ScratchKind, ScratchPad } from '../../api/backend'
import { formatKeys } from '../../lib/commands'
import { IconButton, Input, PanelHeader, TreeRow } from '@flint-ide/ui'
import { Plus, X } from '@flint-ide/ui/icons'

const KIND_LABEL: Record<ScratchKind, string> = { sql: 'SQL', markdown: 'MD', text: 'TXT' }

/** The "+" picker: a scratch file of any type (searchable), then the SQL console and notes. */
function newScratchItems(create: (kind: ScratchKind) => void): DropdownItem[] {
  return [
    { type: 'section', label: 'Scratch file' },
    ...SCRATCH_FILE_TYPES.map(
      (t): DropdownItem => ({
        key: t.id,
        label: t.label,
        hint: t.fileName.slice(t.fileName.lastIndexOf('.')),
        keywords: t.keywords,
        icon: <FileIcon name={t.fileName} />,
        onSelect: () => void createScratchFile(t.id),
      }),
    ),
    { type: 'section', label: 'Scratchpad' },
    { key: 'sql', label: 'SQL console', kbd: formatKeys('Mod+N'), icon: <TabKindIcon kind="sql" />, onSelect: () => create('sql') },
    { key: 'markdown', label: 'Markdown note', icon: <TabKindIcon kind="markdown" />, onSelect: () => create('markdown') },
    { key: 'text', label: 'Text note', icon: <TabKindIcon kind="text" />, onSelect: () => create('text') },
  ]
}

/** Persistent scratches kept outside any project: files of any type, SQL consoles and notes. */
export function ScratchPanel() {
  const items = useScratch((s) => s.items)
  const files = useScratch((s) => s.files)
  const create = useScratch((s) => s.create)
  const pickerToken = useScratch((s) => s.pickerToken)
  const picker = useRef<HTMLElement | null>(null)

  // Newest first: files saved since the last look move up.
  useEffect(() => void useScratch.getState().load(), [])
  // ＋ ▸ Scratch File… in the tab bar opens the picker here.
  useEffect(() => {
    if (pickerToken > 0) picker.current?.click()
  }, [pickerToken])

  return (
    <div className="panel">
      <PanelHeader
        title="Scratches"
        actions={
          <Dropdown
            align="end"
            searchable
            placeholder="Type: http, json, go…"
            minWidth={240}
            items={() => newScratchItems((kind) => void create(kind))}
            trigger={(t) => (
              <IconButton
                {...t}
                ref={(el) => {
                  t.ref(el)
                  picker.current = el
                }}
                size="sm"
                variant="ghost"
                aria-label="New scratch file"
                title="New scratch file (any type), SQL console or note"
              >
                <Plus />
              </IconButton>
            )}
          />
        }
      />
      <div className="tree scroll">
        {items.length === 0 && files.length === 0 && (
          <div className="placeholder muted">No scratches yet. ＋ creates a file of any type (an HTTP request, JSON, Go…); {formatKeys('Mod+N')} an SQL console.</div>
        )}
        {files.map((f) => (
          <ScratchFileRow key={f.path} file={f} />
        ))}
        {items.map((sc) => (
          <ScratchRow key={sc.id} sc={sc} />
        ))}
      </div>
    </div>
  )
}

/** A scratch file: opens in a file tab (language support, the HTTP client). */
function ScratchFileRow({ file }: { file: ScratchFile }) {
  const [renaming, setRenaming] = useState(false)
  const openFile = useEditor((s) => s.openFile)
  const active = useEditor((s) => s.activeId === `file:${file.path}`)

  const commit = (value: string) => {
    setRenaming(false)
    if (value.trim() && value.trim() !== file.name) void renameScratchFile(file.path, value)
  }

  return (
    <TreeRow
      active={active}
      file
      editing={renaming}
      icon={<FileIcon name={file.name} />}
      onClick={() => openFile(file.path)}
      onDoubleClick={() => setRenaming(true)}
      title={`${file.path}\nDouble-click to rename`}
      label={
        renaming ? (
          <Input
            size="sm"
            className="inline-input"
            defaultValue={file.name}
            autoFocus
            onClick={(e) => e.stopPropagation()}
            onFocus={(e) => {
              const dot = e.target.value.lastIndexOf('.')
              e.target.setSelectionRange(0, dot > 0 ? dot : e.target.value.length)
            }}
            onBlur={(e) => commit(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') commit(e.currentTarget.value)
              if (e.key === 'Escape') setRenaming(false)
            }}
          />
        ) : (
          file.name
        )
      }
      actions={
        <IconButton size="sm" variant="ghost" aria-label="Delete" title="Delete" onClick={() => void deleteScratchFile(file)}>
          <X />
        </IconButton>
      }
    />
  )
}

function ScratchRow({ sc }: { sc: ScratchPad }) {
  const [renaming, setRenaming] = useState(false)
  const openScratch = useEditor((s) => s.openScratch)
  const active = useEditor((s) => s.activeId === `scratch:${sc.id}`)
  const rename = useScratch((s) => s.rename)
  const remove = useScratch((s) => s.remove)
  const connName = useDatabase((s) => s.connections.find((c) => c.id === sc.connId)?.name)

  const commit = (value: string) => {
    setRenaming(false)
    if (value.trim() && value !== sc.name) void rename(sc.id, value)
  }

  return (
    <TreeRow
      active={active}
      editing={renaming}
      icon={<span className={`kind-badge ${sc.kind}`}>{KIND_LABEL[sc.kind as ScratchKind] ?? sc.kind}</span>}
      onClick={() => openScratch(sc)}
      onDoubleClick={() => setRenaming(true)}
      title="Double-click to rename"
      label={
        renaming ? (
          <Input
            size="sm"
            className="inline-input"
            defaultValue={sc.name}
            autoFocus
            onClick={(e) => e.stopPropagation()}
            onBlur={(e) => commit(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') commit(e.currentTarget.value)
              if (e.key === 'Escape') setRenaming(false)
            }}
          />
        ) : (
          sc.name
        )
      }
      hint={connName && (sc.database ? `${connName} · ${sc.database}` : connName)}
      actions={
        <IconButton
          size="sm"
          variant="ghost"
          aria-label="Delete"
          title="Delete"
          onClick={async () => (await confirmAction(`Delete scratchpad "${sc.name}"?`, 'Delete')) && remove(sc.id)}
        >
          <X />
        </IconButton>
      }
    />
  )
}
