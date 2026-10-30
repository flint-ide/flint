import { useEffect, useRef, useState, type ReactNode } from 'react'
import { dap } from '../../../wailsjs/go/models'
import {
  debugStep, evaluate, restartDebug, selectFrame, stopDebug, toggleVariable, useDebug, type DebugSession,
} from '../../store/debug'
import { usePersistentNumber } from '../../lib/usePersistentNumber'
import { Splitter } from '../layout/Splitter'
import { RerunIcon, StopIcon } from '../run/RunWidget'

/** The debug tab: stepping toolbar, frames, variables and the console. */
export function DebugView() {
  const session = useDebug((s) => s.session)
  const [width, setWidth] = usePersistentNumber('flint.debugFramesWidth', 300)
  const body = useRef<HTMLDivElement>(null)
  if (!session) return <div className="placeholder muted">No debug session.</div>
  const paused = session.status === 'paused'
  const live = session.status === 'running' || paused

  return (
    <div className="debug-view">
      <div className="run-toolbar">
        <button className="icon" title="Restart" onClick={() => void restartDebug()}>
          <RerunIcon />
        </button>
        <button className="icon run-stop" title="Stop (⌘F2)" disabled={!live && session.status !== 'starting'} onClick={() => void stopDebug()}>
          <StopIcon />
        </button>
        <span className="debug-sep" />
        <StepButton title="Resume (F9)" disabled={!paused} onClick={() => void debugStep('continue')}>▶︎</StepButton>
        <StepButton title="Pause" disabled={session.status !== 'running'} onClick={() => void debugStep('pause')}>❚❚</StepButton>
        <StepButton title="Step Over (F8)" disabled={!paused} onClick={() => void debugStep('next')}>↷</StepButton>
        <StepButton title="Step Into (F7)" disabled={!paused} onClick={() => void debugStep('stepIn')}>↓</StepButton>
        <StepButton title="Step Out (⇧F8)" disabled={!paused} onClick={() => void debugStep('stepOut')}>↑</StepButton>
        <span className={`run-status ${statusClass(session)}`}>{statusText(session)}</span>
      </div>
      <div className="run-body" ref={body}>
        <div className="debug-side" style={{ width }}>
          <Frames session={session} />
          <Variables session={session} />
        </div>
        <Splitter
          direction="horizontal"
          onResize={(ratio) => {
            const total = body.current?.getBoundingClientRect().width ?? 1000
            setWidth(Math.min(total - 200, Math.max(180, ratio * total)))
          }}
        />
        <Console session={session} />
      </div>
    </div>
  )
}

function StepButton({ title, disabled, onClick, children }: { title: string; disabled: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button className="icon debug-step" title={title} disabled={disabled} onClick={onClick}>
      {children}
    </button>
  )
}

function statusClass(s: DebugSession): string {
  return s.status === 'paused' ? 'paused' : s.status === 'running' || s.status === 'starting' ? 'running' : ''
}

function statusText(s: DebugSession): string {
  switch (s.status) {
    case 'starting':
      return 'Starting…'
    case 'running':
      return 'Running'
    case 'paused':
      return `Paused${s.stopReason ? ` (${s.stopReason})` : ''}`
    default:
      return 'Finished'
  }
}

function Frames({ session }: { session: DebugSession }) {
  if (session.status !== 'paused') return <div className="debug-section placeholder muted">{session.status === 'ended' ? 'Not running' : 'Running…'}</div>
  return (
    <div className="debug-section debug-frames">
      <div className="debug-heading">Frames</div>
      {session.frames.map((f) => (
        <div
          key={f.id}
          className={`debug-row${f.id === session.frameId ? ' selected' : ''}${f.source?.path ? '' : ' muted'}`}
          title={f.source?.path ? `${f.source.path}:${f.line}` : f.name}
          onClick={() => void selectFrame(f.id)}
        >
          <span className="truncate">{f.name}</span>
          {f.source?.path && <span className="debug-hint">{f.source.path.split('/').pop()}:{f.line}</span>}
        </div>
      ))}
    </div>
  )
}

function Variables({ session }: { session: DebugSession }) {
  if (session.status !== 'paused') return null
  return (
    <div className="debug-section debug-vars">
      <div className="debug-heading">Variables</div>
      {session.scopes.map((sc) => (
        <VariableRow key={sc.variablesReference} session={session} name={sc.name} value="" refId={sc.variablesReference} depth={0} />
      ))}
    </div>
  )
}

function VariableRow({ session, name, value, type, refId, depth }: { session: DebugSession; name: string; value: string; type?: string; refId: number; depth: number }) {
  const open = !!session.expanded[refId]
  const children: dap.Variable[] | undefined = session.vars[refId]
  return (
    <>
      <div
        className="debug-row debug-var"
        style={{ paddingLeft: 6 + depth * 14 }}
        title={type ? `${name} ${type} = ${value}` : value}
        onClick={() => refId > 0 && void toggleVariable(refId)}
      >
        <span className="test-twisty">{refId > 0 ? (open ? '▾' : '▸') : ''}</span>
        <span className={depth === 0 ? 'debug-scope' : 'debug-name'}>{name}</span>
        {value && <span className="debug-value truncate">{` = ${value}`}</span>}
      </div>
      {open && !children && <div className="debug-row muted" style={{ paddingLeft: 20 + depth * 14 }}>Loading…</div>}
      {open &&
        children?.map((v, i) => (
          <VariableRow
            key={`${v.name}${i}`}
            session={session}
            name={v.name}
            value={v.value}
            type={v.type}
            refId={v.variablesReference}
            depth={depth + 1}
          />
        ))}
    </>
  )
}

function Console({ session }: { session: DebugSession }) {
  const output = useDebug((s) => s.output)
  const [expr, setExpr] = useState('')
  const out = useRef<HTMLPreElement>(null)
  useEffect(() => {
    const el = out.current
    if (el) el.scrollTop = el.scrollHeight
  }, [output])
  return (
    <div className="debug-console">
      <pre ref={out}>{output}</pre>
      <input
        className="debug-eval"
        placeholder={session.status === 'paused' ? 'Evaluate expression (↵)' : 'Pause the program to evaluate in a frame'}
        spellCheck={false}
        value={expr}
        disabled={session.status === 'ended'}
        onChange={(e) => setExpr(e.target.value)}
        onKeyDown={(e) => {
          if (e.key !== 'Enter' || !expr.trim()) return
          void evaluate(expr)
          setExpr('')
        }}
      />
    </div>
  )
}
