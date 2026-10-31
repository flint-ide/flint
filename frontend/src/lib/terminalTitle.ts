// Naming terminal tabs after what they run, pure so it is testable.

/** Longest tab name; longer program titles are cut. */
const MAX_TITLE = 40

/** What the backend reports (term:title) plus the title a program set (OSC 0/2). */
export interface TitleSource {
  /** The running command ("npm run dev"), '' at the prompt. */
  command: string
  /** The shell's directory. */
  cwd: string
  /** Title set by the running program, e.g. Claude Code's "✳ Fix the login form". */
  programTitle?: string
  /** What the terminal works as, shown first ("aws:prod"). */
  tag?: string
}

/**
 * The tab name: a program's own title while it runs, else its command line;
 * at the prompt, the folder the shell is in.
 */
export function terminalTitle({ command, cwd, programTitle, tag }: TitleSource): string {
  const name = command ? programTitle?.trim() || command : cwd.split('/').filter(Boolean).pop() || '~'
  const cut = name.length > MAX_TITLE ? `${name.slice(0, MAX_TITLE - 1)}…` : name
  return tag ? `${tag} · ${cut}` : cut
}
