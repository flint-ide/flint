import { useState } from 'react'
import { Database, errorMessage, newConnection, type ClickHouseProtocol, type Connection, type Driver } from '../../api/backend'
import { db } from '../../../wailsjs/go/models'
import { useDatabase } from '../../store/database'
import { Modal } from '../layout/Modal'
import { Select } from '../ui/Select'

interface Props {
  initial: Connection | null
  onClose: () => void
}

type Status = { kind: 'idle' } | { kind: 'busy'; label: string } | { kind: 'ok'; message: string } | { kind: 'error'; message: string }

/** Create / edit a connection profile. Passwords go straight to the OS keychain. */
export function ConnectionForm({ initial, onClose }: Props) {
  const [conn, setConn] = useState<Connection>(() => initial ?? newConnection())
  const [password, setPassword] = useState('')
  const [passwordTouched, setPasswordTouched] = useState(initial === null)
  const [status, setStatus] = useState<Status>({ kind: 'idle' })
  const upsert = useDatabase((s) => s.upsertConnection)

  const update = (patch: Partial<Connection>) => setConn((c) => db.Connection.createFrom({ ...c, ...patch }))
  const busy = status.kind === 'busy'

  const test = async () => {
    setStatus({ kind: 'busy', label: 'Testing…' })
    try {
      await Database.TestConnection(conn, password, passwordTouched)
      setStatus({ kind: 'ok', message: 'Connection OK' })
    } catch (err) {
      setStatus({ kind: 'error', message: errorMessage(err) })
    }
  }

  const save = async () => {
    setStatus({ kind: 'busy', label: 'Saving…' })
    try {
      upsert(await Database.SaveConnection(conn, password, passwordTouched))
      onClose()
    } catch (err) {
      setStatus({ kind: 'error', message: errorMessage(err) })
    }
  }

  const isClickHouse = conn.driver === 'clickhouse'
  const http = isClickHouse && conn.protocol === 'http'
  const defaultPort = isClickHouse ? (http ? (conn.tls ? 8443 : 8123) : conn.tls ? 9440 : 9000) : 5432

  return (
    <Modal title={initial ? `Edit ${initial.name}` : 'New connection'} onClose={onClose}>
      <form
        className="form"
        onSubmit={(e) => {
          e.preventDefault()
          void save()
        }}
      >
        <label>
          Driver
          <Select
            block
            value={conn.driver as Driver}
            options={[
              { value: 'postgres', label: 'PostgreSQL', icon: <span className="driver-badge postgres">PG</span> },
              { value: 'clickhouse', label: 'ClickHouse', icon: <span className="driver-badge clickhouse">CH</span> },
            ]}
            onChange={(driver) => update({ driver, port: 0, protocol: driver === 'clickhouse' ? 'native' : '' })}
          />
        </label>
        {isClickHouse && (
          <label>
            Protocol
            <Select
              block
              value={(conn.protocol || 'native') as ClickHouseProtocol}
              options={[
                { value: 'native', label: 'Native TCP', hint: conn.tls ? 'port 9440' : 'port 9000' },
                { value: 'http', label: 'HTTP', hint: conn.tls ? 'HTTPS, port 8443' : 'port 8123' },
              ]}
              onChange={(protocol) => update({ protocol, port: 0 })}
            />
          </label>
        )}
        <label>
          Name
          <input value={conn.name} placeholder="prod-analytics" onChange={(e) => update({ name: e.target.value })} />
        </label>
        <div className="form-row">
          <label className="grow">
            Host
            <input value={conn.host} onChange={(e) => update({ host: e.target.value })} required />
          </label>
          <label className="port">
            Port
            <input
              type="number"
              value={conn.port || ''}
              placeholder={String(defaultPort)}
              onChange={(e) => update({ port: Number(e.target.value) || 0 })}
            />
          </label>
        </div>
        <label>
          Default database <span className="muted">(optional — every database on the server is listed)</span>
          <input value={conn.database} placeholder={isClickHouse ? 'default' : 'postgres'} onChange={(e) => update({ database: e.target.value })} />
        </label>
        <div className="form-row">
          <label className="grow">
            User
            <input value={conn.user} onChange={(e) => update({ user: e.target.value })} required autoComplete="off" />
          </label>
          <label className="grow">
            Password
            <input
              type="password"
              value={password}
              placeholder={passwordTouched ? '' : '•••••• (stored in Keychain)'}
              autoComplete="new-password"
              onChange={(e) => {
                setPassword(e.target.value)
                setPasswordTouched(true)
              }}
            />
          </label>
        </div>
        <div className="form-row checks">
          <label className="check">
            <input type="checkbox" checked={conn.tls} onChange={(e) => update({ tls: e.target.checked, port: 0 })} />
            TLS
          </label>
          <label className="check">
            <input type="checkbox" checked={conn.readOnly} onChange={(e) => update({ readOnly: e.target.checked })} />
            Read-only
          </label>
        </div>
        {status.kind !== 'idle' && (
          <div className={`form-status ${status.kind}`}>{status.kind === 'busy' ? status.label : status.message}</div>
        )}
        <div className="form-actions">
          <button type="button" onClick={() => void test()} disabled={busy}>Test</button>
          <span className="spacer" />
          <button type="button" onClick={onClose}>Cancel</button>
          <button type="submit" className="primary" disabled={busy}>Save</button>
        </div>
      </form>
    </Modal>
  )
}
