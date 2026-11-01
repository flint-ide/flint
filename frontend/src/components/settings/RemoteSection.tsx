import { useEffect, useRef, useState } from 'react'
import { useSettings } from '../../store/settings'
import { confirmAction, showToast } from '../../store/dialog'
import { errorMessage } from '../../api/backend'
import { EventsOn } from '../../wails/runtime'
import * as RemoteAPI from '../../../bindings/flint/remoteapi'
import type { PairingView, RemoteStatus } from '../../../bindings/flint/models'
import { DEFAULT_REMOTE, formatCountdown, parseHosts, REMOTE_LIMITS, type RemoteSettings } from '../../lib/remoteSettings'
import { Select } from '../ui/Select'
import { NumberField, Row } from './fields'

/** Settings ▸ Remote: Flint Remote, followed from a paired phone (early preview). */
export function RemoteSection() {
  const remote = useSettings((s) => s.settings.remote)
  const update = useSettings((s) => s.update)
  const set = (change: Partial<RemoteSettings>) => update((s) => ({ remote: { ...s.remote, ...change } }))
  const [status, setStatus] = useState<RemoteStatus | null>(null)
  useEffect(() => {
    let live = true
    const load = () => RemoteAPI.Status().then((s) => live && setStatus(s), () => {})
    void load()
    const off = EventsOn('remote:changed', () => void load())
    return () => {
      live = false
      off()
    }
  }, [])
  return (
    <section>
      <h2>Remote <span className="muted">(early preview)</span></h2>
      <p className="muted settings-note">
        Follow your agent chats and tasks from your phone while Flint keeps running here. This first version is read-only; the iPhone and Android
        app that uses it comes next.
      </p>
      <Row label="Allow paired devices" hint="Serves a read-only API on 127.0.0.1 for paired devices only. Off by default.">
        <input type="checkbox" checked={remote.enabled} onChange={(e) => set({ enabled: e.target.checked })} />
      </Row>
      <Row label="Port" hint="On 127.0.0.1 only; fixed so a tunnel can target it.">
        <NumberField value={remote.port} {...REMOTE_LIMITS.port} step={1} onCommit={(v) => set({ port: Math.round(v) })} />
      </Row>
      <HostsRow hosts={remote.allowedHosts} onCommit={(allowedHosts) => set({ allowedHosts })} />
      <StatusLine status={status} enabled={remote.enabled} />
      <Devices status={status} hosts={remote.allowedHosts} />
      <h3>How to reach it</h3>
      <p className="muted settings-note">
        Flint never opens a public port. Use your own tunnel: <code>tailscale serve --bg {remote.port}</code> (HTTPS on your tailnet; add the name it
        gives the laptop above), or <code>ssh -L {remote.port}:127.0.0.1:{remote.port} you@laptop</code> from another machine. The laptop must be
        awake (on power, or with <code>caffeinate</code>). Pairing gives a device read access to your chats, with credentials redacted; revoke a
        device here at any time.
      </p>
      <h3>Limits</h3>
      <Row label="Events kept per chat" hint="Replayed to a device that connects mid-conversation.">
        <NumberField value={remote.replayEvents} {...REMOTE_LIMITS.replayEvents} step={100} onCommit={(v) => set({ replayEvents: Math.round(v) })} />
      </Row>
      <Row label="Memory per chat">
        <NumberField value={remote.replayKB} {...REMOTE_LIMITS.replayKB} step={256} suffix="KB" onCommit={(v) => set({ replayKB: Math.round(v) })} />
      </Row>
      <Row label="Requests per minute" hint="Per paired device.">
        <NumberField value={remote.requestsPerMinute} {...REMOTE_LIMITS.requestsPerMinute} step={10} onCommit={(v) => set({ requestsPerMinute: Math.round(v) })} />
      </Row>
      <div className="settings-actions">
        <button
          disabled={JSON.stringify({ ...remote, enabled: false }) === JSON.stringify(DEFAULT_REMOTE)}
          onClick={() => set({ ...DEFAULT_REMOTE, enabled: remote.enabled })}
        >
          Reset to Defaults
        </button>
      </div>
    </section>
  )
}

function HostsRow({ hosts, onCommit }: { hosts: string[]; onCommit: (hosts: string[]) => void }) {
  const saved = hosts.join('\n')
  const [draft, setDraft] = useState(saved)
  useEffect(() => setDraft(saved), [saved])
  const { hosts: parsed, invalid } = parseHosts(draft)
  return (
    <Row label="Extra allowed hosts" hint="One per line: the name a tunnel gives the laptop, e.g. laptop.tail1234.ts.net. Loopback names are always allowed.">
      <div className="settings-control settings-control-stack">
        <textarea
          className={`settings-textarea${invalid.length ? ' invalid' : ''}`}
          value={draft}
          spellCheck={false}
          placeholder="laptop.tail1234.ts.net"
          onChange={(e) => setDraft(e.target.value)}
          onBlur={() => invalid.length === 0 && parsed.join('\n') !== saved && onCommit(parsed)}
        />
        {invalid.length > 0 && <span className="error">Not a host name: {invalid.join(', ')}</span>}
      </div>
    </Row>
  )
}

function StatusLine({ status, enabled }: { status: RemoteStatus | null; enabled: boolean }) {
  if (!enabled) return <p className="muted settings-note">Off: nothing listens.</p>
  if (!status) return null
  if (status.error) return <p className="error settings-note">Not running: {status.error}</p>
  if (!status.listening) return <p className="muted settings-note">Starting…</p>
  const streams = status.streams === 1 ? '1 device connected' : `${status.streams} devices connected`
  return <p className="muted settings-note">Listening on {status.address} · {streams}</p>
}

const when = (ms: number) => (ms ? new Date(ms).toLocaleString() : 'never')

function Devices({ status, hosts }: { status: RemoteStatus | null; hosts: string[] }) {
  const [pairing, setPairing] = useState<PairingView | null>(null)
  const [host, setHost] = useState('')
  const devices = status?.devices ?? []
  const pairedBefore = useRef(new Set<string>())
  useEffect(() => {
    // A device paired while the code was shown: close it.
    const added = devices.find((d) => !pairedBefore.current.has(d.id))
    if (pairing && added) {
      showToast(`Paired ${added.name}`)
      setPairing(null)
    }
  }, [devices, pairing])
  const pair = async () => {
    try {
      pairedBefore.current = new Set(devices.map((d) => d.id))
      setPairing(await RemoteAPI.Pair(host))
    } catch (err) {
      showToast(`Can't pair: ${errorMessage(err)}`, 'error')
    }
  }
  const revoke = async (id: string, name: string) => {
    if (!(await confirmAction(`Revoke ${name}? It loses access at once.`, 'Revoke'))) return
    RemoteAPI.Revoke(id).catch((err) => showToast(`Not revoked: ${errorMessage(err)}`, 'error'))
  }
  const revokeAll = async () => {
    if (!(await confirmAction('Revoke every paired device?', 'Revoke All'))) return
    RemoteAPI.RevokeAll().catch((err) => showToast(`Not revoked: ${errorMessage(err)}`, 'error'))
  }
  return (
    <>
      <h3>Paired devices</h3>
      {status?.revokeError && (
        <p className="error settings-note">
          A revoked device is cut off, but its revocation couldn't be saved yet ({status.revokeError}). Flint retries each time this page
          updates; until then it could come back after a restart.
        </p>
      )}
      {devices.length === 0 && <p className="muted settings-note">None yet.</p>}
      {devices.map((d) => (
        <Row key={d.id} label={d.name} hint={`Paired ${when(d.created)} · last seen ${when(d.lastSeen)} · ${d.scopes.join(', ')}`}>
          <button onClick={() => void revoke(d.id, d.name)}>Revoke</button>
        </Row>
      ))}
      <div className="settings-actions">
        {hosts.length > 0 && (
          <Select value={host} options={[{ value: '', label: 'Loopback (ssh -L)' }, ...hosts.map((h) => ({ value: h, label: h }))]} onChange={setHost} />
        )}
        <button disabled={!status?.listening} onClick={() => void pair()}>Pair a Device…</button>
        {devices.length > 1 && <button onClick={() => void revokeAll()}>Revoke All</button>}
      </div>
      {pairing && <PairingPanel pairing={pairing} onClose={() => {
        void RemoteAPI.CancelPairing()
        setPairing(null)
      }} />}
    </>
  )
}

/** The QR code (drawn from Go's path) with the link and secret for manual entry, and a countdown. */
function PairingPanel({ pairing, onClose }: { pairing: PairingView; onClose: () => void }) {
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [])
  const left = pairing.expires - now
  useEffect(() => {
    if (left <= 0) onClose()
  }, [left <= 0]) // eslint-disable-line react-hooks/exhaustive-deps
  const size = pairing.qrSize + 8
  // No "copy link": the secret would linger in the clipboard (and in
  // clipboard managers) after the pairing; it is shown, typed or scanned.
  return (
    <div className="remote-pairing">
      <svg className="remote-qr" viewBox={`-4 -4 ${size} ${size}`} role="img" aria-label="Pairing QR code" shapeRendering="crispEdges">
        <rect x={-4} y={-4} width={size} height={size} fill="#fff" />
        <path d={pairing.qr} fill="#000" />
      </svg>
      <div className="remote-pairing-text">
        <p>Scan with the Flint app on your phone, or enter the address and code. Valid for one device, {formatCountdown(left)} left.</p>
        <p>Address: <code>{pairing.url}</code></p>
        <p>Code: <code className="remote-secret">{pairing.secret}</code></p>
        <div className="settings-actions">
          <button onClick={onClose}>Cancel</button>
        </div>
      </div>
    </div>
  )
}
