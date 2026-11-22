import { Button, Card, CardContent } from '@flint-ide/ui'
import { useEffect } from 'react'
import { ExtensionStateBadge } from './ExtensionStateBadge'
import { uninstallExtension, useExtensions } from './store'
import { installFromCatalog, loadCatalog, useCatalog, type Entry } from './catalogStore'
import { catalogRows, sentence, type CatalogRow, type CatalogStatus } from './catalogModel'

const STATUS_LABELS: Record<CatalogStatus, string> = {
  available: 'Not installed',
  installed: 'Installed',
  update: 'Update available',
  newer: 'Newer version installed',
  broken: 'Can’t be installed',
}

/**
 * Settings ▸ Extensions ▸ Repository: every extension of flint-ide/extensions,
 * shown by default, with Install / Update / Uninstall. Local folders are
 * installed from the top of Settings ▸ Extensions (ExtensionsSection).
 */
export function CatalogSection() {
  const listing = useCatalog((s) => s.listing)
  const loading = useCatalog((s) => s.loading)
  const error = useCatalog((s) => s.error)
  const installed = useExtensions((s) => s.list)
  useEffect(() => void loadCatalog(), [])
  const rows = catalogRows(listing?.entries ?? [], installed)
  return (
    <>
      <h3>Repository</h3>
      <p className="muted settings-note">
        Extensions from <code>{listing?.source ?? 'flint-ide/extensions'}</code>
        {listing?.revision ? ` (${listing.revision})` : ''}, read with git and your own credentials. Programs are built with Go on this machine before
        Flint asks you to confirm the install.
      </p>
      <div className="settings-actions">
        <Button variant="plain" disabled={loading} onClick={() => void loadCatalog(true)}>
          {loading ? 'Fetching…' : 'Refresh Repository'}
        </Button>
      </div>
      {error && <p className="error">{sentence(error)}</p>}
      {loading && !listing && <p className="muted settings-note">Fetching the extensions repository…</p>}
      {listing && rows.length === 0 && <p className="muted settings-note">The repository has no extensions.</p>}
      {rows.map((row) => (
        <CatalogCard key={row.entry.folder} row={row} />
      ))}
    </>
  )
}

function CatalogCard({ row }: { row: CatalogRow<Entry> }) {
  const { entry, status, installedVersion } = row
  const busy = useCatalog((s) => s.busy.includes(entry.folder))
  const kind = entry.program ? 'Program' : entry.themes > 0 ? `${entry.themes} color theme${entry.themes === 1 ? '' : 's'}` : 'Data only'
  return (
    <Card className="extension-card"><CardContent>
      <div className="extension-head">
        <strong>{entry.name}</strong>
        {entry.version && <span className="muted">{entry.version}</span>}
        {entry.publisher && <span className="muted">by {entry.publisher}</span>}
        <span className="muted">{kind}</span>
        <ExtensionStateBadge tone={status === 'installed' ? 'ok' : status === 'broken' ? 'bad' : 'none'}>
          {status === 'update' || status === 'newer' ? `${STATUS_LABELS[status]} (installed ${installedVersion})` : STATUS_LABELS[status]}
        </ExtensionStateBadge>
      </div>
      {entry.description && <p className="extension-description">{entry.description}</p>}
      {entry.error && <p className="error">{entry.error}</p>}
      {entry.permissions.length > 0 && <p className="muted extension-warning">Permissions: {entry.permissions.join(', ')}</p>}
      <div className="settings-actions">
        {(status === 'available' || status === 'update') && (
          <Button variant="plain" disabled={busy} onClick={() => void installFromCatalog(entry.folder)}>
            {busy ? (entry.program ? 'Building…' : 'Installing…') : status === 'update' ? 'Update…' : 'Install…'}
          </Button>
        )}
        {(status === 'installed' || status === 'update' || status === 'newer') && (
          <Button variant="destructive" disabled={busy} onClick={() => void uninstallExtension(entry.id)}>
            Uninstall…
          </Button>
        )}
      </div>
    </CardContent></Card>
  )
}
