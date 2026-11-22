// The chat's minimal, safe markdown: fenced code, inline code, bold, headings and
// lists. No HTML is ever produced from the text, so agent output can't inject markup.

/** Splits on ``` fences: even indexes are prose, odd indexes are code (an unclosed fence runs to the end). */
export function fenceParts(text: string): string[] {
  return text.split(/```[^\n]*\n?/)
}

/** A code part without the newline that precedes its closing fence. */
export function codeText(part: string): string {
  return part.replace(/\n$/, '')
}

export type MarkdownLine =
  | { kind: 'gap' }
  | { kind: 'heading'; text: string }
  | { kind: 'bullet'; marker: string; text: string }
  | { kind: 'text'; text: string }

/**
 * One line of prose. Blank lines become gaps, except at the edges of a part
 * (`edge`), where the fence or the message boundary already separates them.
 */
export function parseLine(line: string, edge: boolean): MarkdownLine | null {
  if (!line.trim()) return edge ? null : { kind: 'gap' }
  // Only the prefix is matched and the rest sliced off: a pattern ending in \s+(.*)$
  // backtracks polynomially on long runs of spaces (agent output is untrusted input).
  const heading = /^#{1,4}[ \t]+/.exec(line)
  if (heading) return { kind: 'heading', text: line.slice(heading[0].length) }
  const bullet = /^[ \t]*([-*]|\d+\.)[ \t]+/.exec(line)
  if (bullet) return { kind: 'bullet', marker: /\d/.test(bullet[1]) ? bullet[1] : '•', text: line.slice(bullet[0].length) }
  return { kind: 'text', text: line }
}

export type InlineToken = { kind: 'code' | 'bold' | 'text'; text: string }

/** `code` and **bold** spans of one line; everything else is plain text. */
export function inlineTokens(s: string): InlineToken[] {
  return s.split(/(`[^`]+`|\*\*[^*]+\*\*)/).map((tok): InlineToken => {
    if (tok.startsWith('`') && tok.endsWith('`') && tok.length > 1) return { kind: 'code', text: tok.slice(1, -1) }
    if (tok.startsWith('**') && tok.endsWith('**') && tok.length > 3) return { kind: 'bold', text: tok.slice(2, -2) }
    return { kind: 'text', text: tok }
  })
}
