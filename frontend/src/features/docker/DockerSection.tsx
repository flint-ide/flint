import { useSettings } from '../../store/settings'
import { DEFAULT_DOCKER, DOCKER_LOG_TAIL, validDockerHost, type DockerSettings } from './settings'
import { NumberField, Row, TextField } from '../../components/settings/fields'

/** Settings ▸ Docker: where the engine is, and how much log a log tab starts with. */
export function DockerSection() {
  const docker = useSettings((s) => s.settings.docker)
  const update = useSettings((s) => s.update)
  const set = (change: Partial<DockerSettings>) => update((s) => ({ docker: { ...s.docker, ...change } }))
  const isDefault = docker.host === DEFAULT_DOCKER.host && docker.logTailLines === DEFAULT_DOCKER.logTailLines
  return (
    <section>
      <h2>Docker</h2>
      <p className="muted settings-note">
        The Docker panel talks to the Docker Engine directly, so it works without the docker CLI (only Compose up, down and pull need the CLI).
      </p>
      <Row
        label="Engine address"
        hint="unix:///path/docker.sock, tcp://host:2376 or https://host:2376. Empty finds the engine like the docker CLI: DOCKER_HOST, the current docker context, then the default sockets (Docker Desktop, Colima, OrbStack…). Plain tcp:// without TLS works but is flagged in the panel."
      >
        <TextField value={docker.host} valid={validDockerHost} onCommit={(v) => set({ host: v.trim() })} />
      </Row>
      <Row label="Log lines at the start" hint="How many past lines a container's log tab shows before it follows new output.">
        <NumberField
          value={docker.logTailLines}
          min={DOCKER_LOG_TAIL.min}
          max={DOCKER_LOG_TAIL.max}
          step={50}
          suffix="lines"
          onCommit={(v) => set({ logTailLines: Math.round(v) })}
        />
      </Row>
      <div className="settings-actions">
        <button disabled={isDefault} onClick={() => update({ docker: DEFAULT_DOCKER })}>Reset to Defaults</button>
      </div>
    </section>
  )
}
