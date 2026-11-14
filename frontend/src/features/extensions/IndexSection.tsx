import { useEffect } from 'react'
import { useSettings } from '../../store/settings'
import { Row, TextField } from '../../components/settings/fields'
import { uninstallExtension, useExtensions } from './store'
import { fetchIndex, installFromIndex, loadIndex, useIndex, type IndexEntry, type IndexStatus } from './indexStore'
import { formatSize, indexRows, kindLabel } from './indexModel'
import { sentence, type CatalogRow, type CatalogStatus } from './catalogModel'
import { DEFAULT_INDEX_URL, indexSettings, validIndexKey, validIndexUrl, withIndexSetting } from './settings'

const STATUS_LABELS: Record<CatalogStatus, string> = {
  available: 'Not installed',
  installed: 'Installed',
  update: 'Update available',
  newer: 'Newer version installed',
  broken: 'Can’t be installed here',
}

function when(time: unknown): string {
  const d = new Date(typeof time === 'string' ? time : '')
  return Number.isNaN(d.getTime()) || d.getFullYear() < 2000 ? '' : d.toLocaleString()
}

/**
 * Settings ▸ Extensions ▸ Extension index: prebuilt extensions from one
 * signed index. Flint downloads it only when a button here (or its palette
 * command) is used, and Go shows nothing of it before its signature
 * verified against the public key built into Flint or set below.
 */
export function IndexSection() {
  const status = useIndex((s) => s.status)
  const loading = useIndex((s) => s.loading)
  const error = useIndex((s) => s.error)
  const installed = useExtensions((s) => s.list)
  const extensions = useSettings((s) => s.settings.extensions)
  const update = useSettings((s) => s.update)
  const { url, publicKey } = indexSettings(extensions)
  // Reads what Go already has (no request); again when the address or key changed.
  useEffect(() => void loadIndex(), [url, publicKey])
  const rows = indexRows(status?.entries ?? [], installed)
  return (
    <>
      <h3>Extension index</h3>
      <p className="muted settings-note">
        Prebuilt extensions from one signed index, <code>{status?.url ?? (url || DEFAULT_INDEX_URL)}</code>. Flint downloads it only when you click a
        button here, checks its signature before showing anything, checks each download against it, and asks you to confirm every install and update.
      </p>
      {status && !status.configured && <NotConfigured status={status} />}
      <div className="settings-actions">
        <button disabled={loading || !status?.configured} onClick={() => void fetchIndex()}>
          {loading ? 'Downloading…' : status?.fetched ? 'Check for Updates' : 'Browse Extensions'}
        </button>
        {status?.fetched && <span className="muted">{[when(status.fetchedAt) && `Downloaded ${when(status.fetchedAt)}`, when(status.generatedAt) && `built ${when(status.generatedAt)}`].filter(Boolean).join(', ')}</span>}
      </div>
      {error && <p className="error">{sentence(error)}</p>}
      {status?.fetched && rows.length === 0 && <p className="muted settings-note">The index lists no extensions.</p>}
      {rows.map((row) => (
        <IndexCard key={row.entry.id} row={row} />
      ))}
      <Row label="Index address" hint={`The index.json Flint downloads; its signature is at the same address plus .sig. Empty: ${DEFAULT_INDEX_URL}`}>
        <TextField value={url} valid={(v) => validIndexUrl(v.trim())} onCommit={(v) => update((s) => ({ extensions: withIndexSetting(s.extensions, 'indexUrl', v.trim()) }))} />
      </Row>
      <Row label="Index public key" hint="The Ed25519 public key (base64) the index must be signed with, for an index of your own. Empty: the key built into Flint.">
        <TextField value={publicKey} valid={(v) => validIndexKey(v.trim())} onCommit={(v) => update((s) => ({ extensions: withIndexSetting(s.extensions, 'indexPublicKey', v.trim()) }))} />
      </Row>
    </>
  )
}

/** No key to verify with: say why, and how it gets configured. */
function NotConfigured({ status }: { status: IndexStatus }) {
  return (
    <p className="muted extension-warning">
      {status.official ? (
        <>
          The official index is not configured yet: this build of Flint has no public key for it, so nothing is downloaded. A maintainer sets it up by
          generating a key pair with <code>flint-extindex keygen</code>, storing the secret key as the signing secret of flint-ide/extensions and
          building the public key into Flint. Until then, use the repository below or <strong>Install from Folder…</strong>.
        </>
      ) : (
        <>No public key is configured for this index, so nothing is downloaded.</>
      )}{' '}
      To use an index of your own, set its address and public key below.
    </p>
  )
}

function IndexCard({ row }: { row: CatalogRow<IndexEntry> }) {
  const { entry, status, installedVersion } = row
  const busy = useIndex((s) => s.busy.includes(entry.id))
  const size = formatSize(entry.size)
  return (
    <div className="extension-card">
      <div className="extension-head">
        <strong>{entry.name}</strong>
        <span className="muted">{entry.version}</span>
        {entry.publisher && <span className="muted">by {entry.publisher}</span>}
        <span className="muted">{entry.id}</span>
        <span className="muted">{kindLabel(entry.kind)}</span>
        {size && <span className="muted">{size}</span>}
        <span className={`extension-state ${status === 'installed' ? 'running' : status === 'broken' ? 'invalid' : ''}`}>
          {status === 'update' || status === 'newer' ? `${STATUS_LABELS[status]} (installed ${installedVersion})` : STATUS_LABELS[status]}
        </span>
      </div>
      {entry.description && <p className="extension-description">{entry.description}</p>}
      {entry.error && <p className="error">{sentence(entry.error)}</p>}
      {entry.kind === 'program' && (
        <p className="muted extension-warning">
          Permissions: {entry.permissionTexts.length ? entry.permissionTexts.join('; ') : 'none'}
          {entry.agentTools.length > 0 && <>. Agent tools: {entry.agentTools.map((t) => (t.acts ? `${t.name} (acts)` : t.name)).join(', ')}</>}
        </p>
      )}
      <div className="settings-actions">
        {(status === 'available' || status === 'update') && (
          <button disabled={busy} onClick={() => void installFromIndex(entry.id)}>
            {busy ? 'Downloading…' : status === 'update' ? `Update to ${entry.version}…` : 'Install…'}
          </button>
        )}
        {(status === 'installed' || status === 'update' || status === 'newer') && (
          <button className="danger" disabled={busy} onClick={() => void uninstallExtension(entry.id)}>
            Uninstall…
          </button>
        )}
      </div>
    </div>
  )
}
