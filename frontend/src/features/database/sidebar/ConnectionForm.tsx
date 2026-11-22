import { Alert, Button, Checkbox, Field, FieldLabel, Form, FormActions, Input } from '@flint-ide/ui'
import { useState } from 'react'
import { Database, errorMessage, newConnection, type Connection, type Driver } from '../../../api/backend'
import { db } from '../../../wails/models'
import { useDatabase } from '../store/database'
import { Modal } from '../../../components/layout/Modal'
import { Select } from '../../../components/ui/Select'
import { keyringName } from '../../../lib/platform'

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
  // ClickHouse is reached over HTTP(S); a different port (a proxy, a custom setup) goes in the Port field.
  const defaultPort = isClickHouse ? (conn.tls ? 8443 : 8123) : 5432

  return (
    <Modal title={initial ? `Edit ${initial.name}` : 'New connection'} onClose={onClose}>
      <Form
        onSubmit={(e) => {
          e.preventDefault()
          void save()
        }}
      >
        <Field>
          <FieldLabel>Driver</FieldLabel>
          <Select
            block
            value={conn.driver as Driver}
            options={[
              { value: 'postgres', label: 'PostgreSQL', icon: <span className="driver-badge postgres">PG</span> },
              { value: 'clickhouse', label: 'ClickHouse', icon: <span className="driver-badge clickhouse">CH</span> },
            ]}
            onChange={(driver) => update({ driver: driver as Connection['driver'], port: 0 })}
          />
        </Field>
        <Input label="Name" value={conn.name} placeholder="prod-analytics" onChange={(e) => update({ name: e.target.value })} />
        <div className="form-row">
          <div className="grow">
            <Input label="Host" value={conn.host} onChange={(e) => update({ host: e.target.value })} required />
          </div>
          <div className="port">
            <Input
              label="Port"
              type="number"
              value={conn.port || ''}
              placeholder={String(defaultPort)}
              onChange={(e) => update({ port: Number(e.target.value) || 0 })}
            />
          </div>
        </div>
        <Input
          label="Default database"
          helper="Optional: every database on the server is listed"
          value={conn.database}
          placeholder={isClickHouse ? 'default' : 'postgres'}
          onChange={(e) => update({ database: e.target.value })}
        />
        <div className="form-row">
          <div className="grow">
            <Input label="User" value={conn.user} onChange={(e) => update({ user: e.target.value })} required autoComplete="off" />
          </div>
          <div className="grow">
            <Input
              label="Password"
              type="password"
              value={password}
              placeholder={passwordTouched ? '' : `•••••• (stored in ${keyringName()})`}
              autoComplete="new-password"
              onChange={(e) => {
                setPassword(e.target.value)
                setPasswordTouched(true)
              }}
            />
          </div>
        </div>
        <div className="form-row checks">
          <Checkbox label="TLS" checked={conn.tls} onCheckedChange={(checked) => update({ tls: checked, tlsInsecure: false, port: 0 })} />
          {conn.tls && (
            <span title="Encrypts without checking the server's certificate: anyone in between can read the traffic. Only for self-signed or private CAs.">
              <Checkbox label="Don't verify certificate (insecure)" checked={conn.tlsInsecure} onCheckedChange={(checked) => update({ tlsInsecure: checked })} />
            </span>
          )}
          <Checkbox label="Read-only" checked={conn.readOnly} onCheckedChange={(checked) => update({ readOnly: checked })} />
        </div>
        {status.kind !== 'idle' && (
          <Alert size="inline" variant={status.kind === 'error' ? 'negative' : status.kind === 'ok' ? 'positive' : 'plain'}>
            {status.kind === 'busy' ? status.label : status.message}
          </Alert>
        )}
        <FormActions align="between">
          <Button type="button" variant="plain" onClick={() => void test()} disabled={busy}>Test</Button>
          <span>
            <Button type="button" variant="plain" onClick={onClose}>Cancel</Button>
            <Button type="submit" variant="primary" disabled={busy}>Save</Button>
          </span>
        </FormActions>
      </Form>
    </Modal>
  )
}
