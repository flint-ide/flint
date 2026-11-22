import { memo, useMemo, type ReactNode } from 'react'
import { codeText, fenceParts, inlineTokens, parseLine } from './markdown'

/**
 * A chat message's text as minimal, safe markdown (see markdown.ts). memo throughout:
 * while a reply streams only its tail changes, so parts and lines are memoized by
 * their text and a new token re-parses the last line instead of the whole message.
 */
export const ChatMarkdown = memo(function ChatMarkdown({ text }: { text: string }) {
  const parts = useMemo(() => fenceParts(text), [text])
  return (
    <div className="fui-chat-md">
      {parts.map((part, i) => (i % 2 === 1 ? <ChatCodeBlock key={i} code={codeText(part)} /> : <Paragraphs key={i} text={part} />))}
    </div>
  )
})

/** A fenced code block: monospace, scrolls sideways instead of wrapping. */
export const ChatCodeBlock = memo(function ChatCodeBlock({ code }: { code: string }) {
  return <pre className="fui-chat-code">{code}</pre>
})

const Paragraphs = memo(function Paragraphs({ text }: { text: string }) {
  const lines = text.split('\n')
  return (
    <>
      {lines.map((line, i) => (
        <Line key={i} line={line} edge={i === 0 || i === lines.length - 1} />
      ))}
    </>
  )
})

const Line = memo(function Line({ line, edge }: { line: string; edge: boolean }) {
  const parsed = parseLine(line, edge)
  if (!parsed) return null
  switch (parsed.kind) {
    case 'gap':
      return <div className="fui-chat-gap" />
    case 'heading':
      return <div className="fui-chat-heading">{inline(parsed.text)}</div>
    case 'bullet':
      return (
        <div className="fui-chat-bullet">
          <span>{parsed.marker}</span>
          <span>{inline(parsed.text)}</span>
        </div>
      )
    default:
      return <div>{inline(parsed.text)}</div>
  }
})

function inline(s: string): ReactNode[] {
  return inlineTokens(s).map((tok, i) => {
    if (tok.kind === 'code') return <code key={i}>{tok.text}</code>
    if (tok.kind === 'bold') return <b key={i}>{tok.text}</b>
    return tok.text
  })
}
