import { useState } from 'react'
import { runconfig } from '../../../wailsjs/go/models'
import { errorMessage } from '../../api/backend'
import { confirmAction } from '../../store/dialog'
import {
  closeRunConfigDialog, deleteRunConfig, runConfiguration, saveRunConfig, useRun, type RunConfig, type RunType,
} from '../../store/run'
import { formatEnv, joinArgs, parseEnv, splitArgs } from '../../lib/shellArgs'
import { Modal } from '../layout/Modal'
import { Select } from '../ui/Select'

interface Props {
  editing: { config: RunConfig; replaces: string | null }
}

type Status = { kind: 'idle' } | { kind: 'busy' } | { kind: 'error'; message: string }

const TYPES: { value: RunType; label: string; hint: string }[] = [
  { value: 'command', label: 'Command', hint: 'program or shell line' },
  { value: 'go', label: 'Go', hint: 'go build + run' },
  { value: 'npm', label: 'npm script', hint: 'package.json' },
  { value: 'make', label: 'Make', hint: 'Makefile target' },
]

/** Create / edit a run configuration; saved to .flint/run.json (validated in Go). */
export function RunConfigDialog({ editing }: Props) {
  const [config, setConfig] = useState<RunConfig>(editing.config)
  const [args, setArgs] = useState(joinArgs(editing.config.args))
  const [buildFlags, setBuildFlags] = useState(joinArgs(editing.config.buildFlags))
  const [env, setEnv] = useState(formatEnv(editing.config.env))
  const [status, setStatus] = useState<Status>({ kind: 'idle' })
  const debuggable = useRun((s) => s.capabilities[editing.config.id]?.debug ?? editing.config.type === 'go')
  const type = config.type as RunType
  const update = (patch: Partial<RunConfig>) => setConfig((c) => runconfig.Config.createFrom({ ...c, ...patch }))

  /** The configuration as saved: only the fields its type uses. */
  const build = (): RunConfig => {
    const list = (s: string) => (s.trim() ? splitArgs(s) : undefined)
    const envMap = parseEnv(env)
    return runconfig.Config.createFrom({
      id: config.id,
      name: config.name.trim(),
      type,
      cwd: config.cwd?.trim() || undefined,
      env: Object.keys(envMap).length ? envMap : undefined,
      before: config.before?.trim() || undefined,
      command: type === 'command' ? config.command : undefined,
      args: type === 'make' ? undefined : list(args),
      package: type === 'go' ? config.package : undefined,
      buildFlags: type === 'go' ? list(buildFlags) : undefined,
      script: type === 'npm' ? config.script : undefined,
      target: type === 'make' ? config.target || undefined : undefined,
      debug: config.debug,
      profileURL: config.profileURL?.trim() || undefined,
    })
  }

  const save = async (andRun: boolean) => {
    setStatus({ kind: 'busy' })
    try {
      const stored = await saveRunConfig(build(), editing.replaces)
      closeRunConfigDialog()
      if (andRun && stored) void runConfiguration(stored)
    } catch (err) {
      setStatus({ kind: 'error', message: errorMessage(err) })
    }
  }

  const remove = async () => {
    if (!editing.replaces || !(await confirmAction(`Delete the run configuration ${config.name}?`, 'Delete'))) return
    try {
      await deleteRunConfig(editing.replaces)
      closeRunConfigDialog()
    } catch (err) {
      setStatus({ kind: 'error', message: errorMessage(err) })
    }
  }

  const title = editing.replaces ? `Edit ${editing.config.name}` : editing.config.name ? `Save ${editing.config.name}` : 'New run configuration'
  return (
    <Modal title={title} onClose={closeRunConfigDialog} className="run-config-dialog">
      <form
        className="form"
        onSubmit={(e) => {
          e.preventDefault()
          void save(false)
        }}
      >
        <div className="form-row">
          <label className="grow">
            Name
            <input value={config.name} placeholder="Server" autoFocus required onChange={(e) => update({ name: e.target.value })} />
          </label>
          <label className="run-type">
            Type
            <Select block value={type} options={TYPES} onChange={(t) => update({ type: t })} />
          </label>
        </div>

        {type === 'command' && (
          <>
            <label>
              Command
              <input value={config.command ?? ''} placeholder="./bin/server --dev   (shell line, or a program when arguments are set)" required onChange={(e) => update({ command: e.target.value })} />
            </label>
            <label>
              Arguments
              <input value={args} placeholder="optional; quoted like in a shell" onChange={(e) => setArgs(e.target.value)} />
            </label>
          </>
        )}
        {type === 'go' && (
          <>
            <label>
              Package or file
              <input value={config.package ?? ''} placeholder="./cmd/server, . or main.go" required onChange={(e) => update({ package: e.target.value })} />
            </label>
            <div className="form-row">
              <label className="grow">
                Build flags
                <input value={buildFlags} placeholder="-race -tags dev" onChange={(e) => setBuildFlags(e.target.value)} />
              </label>
              <label className="grow">
                Program arguments
                <input value={args} placeholder="-port 8080" onChange={(e) => setArgs(e.target.value)} />
              </label>
            </div>
          </>
        )}
        {type === 'npm' && (
          <div className="form-row">
            <label className="grow">
              Script
              <input value={config.script ?? ''} placeholder="dev" required onChange={(e) => update({ script: e.target.value })} />
            </label>
            <label className="grow">
              Arguments
              <input value={args} placeholder="--port 3000" onChange={(e) => setArgs(e.target.value)} />
            </label>
          </div>
        )}
        {type === 'make' && (
          <label>
            Target
            <input value={config.target ?? ''} placeholder="default target" onChange={(e) => update({ target: e.target.value })} />
          </label>
        )}

        <div className="form-row">
          <label className="grow">
            Working directory
            <input value={config.cwd ?? ''} placeholder="project root (relative path)" onChange={(e) => update({ cwd: e.target.value })} />
          </label>
          <label className="grow">
            Before launch
            <input value={config.before ?? ''} placeholder="e.g. make generate" onChange={(e) => update({ before: e.target.value })} />
          </label>
        </div>
        <label>
          Environment
          <textarea className="run-env" rows={3} value={env} placeholder={'KEY=value\nDEBUG=1'} spellCheck={false} onChange={(e) => setEnv(e.target.value)} />
        </label>
        <label>
          Profile URL
          <input
            value={config.profileURL ?? ''}
            placeholder="http://localhost:6060/debug/pprof/profile?seconds=10   (optional, net/http/pprof)"
            onChange={(e) => update({ profileURL: e.target.value })}
          />
        </label>
        {debuggable && type === 'go' && <div className="muted run-hint">Debugging (dlv dap) will hook into this configuration.</div>}

        {status.kind === 'error' && <div className="form-status error">{status.message}</div>}
        <div className="form-actions">
          {editing.replaces && (
            <button type="button" className="danger" onClick={() => void remove()} disabled={status.kind === 'busy'}>
              Delete
            </button>
          )}
          <span className="muted run-hint">.flint/run.json</span>
          <span className="spacer" />
          <button type="button" onClick={closeRunConfigDialog}>Cancel</button>
          <button type="button" onClick={() => void save(true)} disabled={status.kind === 'busy'}>Save &amp; Run</button>
          <button type="submit" className="primary" disabled={status.kind === 'busy'}>Save</button>
        </div>
      </form>
    </Modal>
  )
}
