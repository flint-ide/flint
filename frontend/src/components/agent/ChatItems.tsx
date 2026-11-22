import { memo, useMemo } from 'react'
import { ChatContextUsage, ChatFileLink, ChatNotice, ChatPermission, DiffStat, ToolCall, ToolCallOutput, ToolDiff, toolStatus } from '@flint-ide/ui'
import type { Diff, PermissionRequest, ToolCall as ToolCallData, Usage, UsageTotals } from '../../lib/agentReducer'
import { contextPercent, formatCost, formatTokens, hasUsage, usageTooltip } from '../../lib/usage'
import { countLines, previewLines, useHunks } from '../../lib/editDiff'
import { useEditor } from '../../store/editor'

// The transcript's items bound to Flint's data and stores; the look is the kit's (ui/src/chat).
// memo: streaming appends to one item; the rest must not re-render.

/** One tool call as a compact row; click to see input, output and diffs. */
export const ToolCallRow = memo(function ToolCallRow({ call, root, live }: { call: ToolCallData; root: string; live: boolean }) {
  const hasDiffs = (call.diffs?.length ?? 0) > 0
  const hasDetails = !!(call.input || call.output || hasDiffs)
  return (
    <ToolCall
      kind={call.kind}
      title={call.title || call.kind || 'tool'}
      status={toolStatus(call.status)}
      live={live}
      meta={hasDiffs && <ChatDiffStat diffs={call.diffs!} />}
      paths={(call.paths?.length ?? 0) > 0 && call.paths!.slice(0, 4).map((p) => <FileLink key={p} path={p} root={root} />)}
    >
      {hasDetails && (
        <>
          {call.diffs?.map((d) => <DiffPreview key={d.path} diff={d} root={root} />)}
          {call.input && !hasDiffs && <ToolCallOutput>{call.input}</ToolCallOutput>}
          {call.output && <ToolCallOutput muted>{call.output}</ToolCallOutput>}
        </>
      )}
    </ToolCall>
  )
})

function ChatDiffStat({ diffs }: { diffs: Diff[] }) {
  // Diffed in Go; the stat appears once the backend answers.
  const hunks = useHunks(diffs)
  if (!hunks) return null
  const { add, del } = countLines(hunks)
  return <DiffStat added={add} removed={del} />
}

const MAX_DIFF_LINES = 80

/** Changed lines only (red/green), like a compact unified diff. */
function DiffPreview({ diff, root }: { diff: Diff; root: string }) {
  const diffs = useMemo(() => [diff], [diff]) // stable array: useHunks keys on identity
  const hunks = useHunks(diffs)
  const lines = useMemo(() => (hunks ? previewLines(diff, hunks[0], MAX_DIFF_LINES) : []), [diff, hunks])
  return (
    <ToolDiff header={<FileLink path={diff.path} root={root} />} lines={lines.slice(0, MAX_DIFF_LINES)} truncated={lines.length > MAX_DIFF_LINES} />
  )
}

export function FileLink({ path, root }: { path: string; root: string }) {
  const openFile = useEditor((s) => s.openFile)
  const rel = root && path.startsWith(root + '/') ? path.slice(root.length + 1) : path
  return <ChatFileLink label={rel} path={path} onOpen={() => openFile(path)} />
}

/** A question from the agent with its options; settled requests show the answer. */
export const PermissionCard = memo(function PermissionCard(props: {
  request: PermissionRequest
  answer: string | null
  settled: boolean
  onAnswer: (optionId: string, optionName: string) => void
}) {
  const { request, answer, settled, onAnswer } = props
  const call = request.toolCall
  return (
    <ChatPermission
      kind={call.kind}
      title={call.title || 'The agent wants to continue'}
      input={call.input}
      options={request.options}
      settled={settled}
      answer={answer}
      onAnswer={onAnswer}
    />
  )
})

/** Compact running usage of a chat: context fill gauge, tokens and cost; details in the tooltip. */
export const ChatUsage = memo(function ChatUsage({ totals }: { totals: UsageTotals }) {
  if (!hasUsage(totals)) return null
  return (
    <ChatContextUsage percent={contextPercent(totals)} title={usageTooltip(totals)}>
      <span>
        {formatTokens(totals.inputTokens)} in / {formatTokens(totals.outputTokens)} out
      </span>
      {totals.costUsd > 0 && <span>{formatCost(totals.costUsd)}</span>}
    </ChatContextUsage>
  )
})

export function UsageLine({ usage, stopReason }: { usage?: Usage; stopReason: string }) {
  const parts: string[] = []
  if (stopReason !== 'end_turn') parts.push(stopReason.replace('_', ' '))
  if (usage && (usage.inputTokens || usage.outputTokens)) parts.push(`${usage.inputTokens + usage.outputTokens} tokens`)
  if (usage?.costUsd) parts.push(`$${usage.costUsd.toFixed(3)}`)
  if (parts.length === 0) return null
  return <ChatNotice>{parts.join(' · ')}</ChatNotice>
}
