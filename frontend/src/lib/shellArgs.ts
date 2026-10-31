// Argument lists edited as one line in the run configuration dialog.

/** Splits a line like a POSIX shell would: quotes group, backslash escapes. */
export function splitArgs(line: string): string[] {
  const out: string[] = []
  let cur = ''
  let has = false
  let quote: '"' | "'" | null = null
  for (let i = 0; i < line.length; i++) {
    const c = line[i]
    if (quote) {
      if (c === quote) quote = null
      else if (c === '\\' && quote === '"' && i + 1 < line.length) cur += line[++i]
      else cur += c
    } else if (c === '"' || c === "'") {
      quote = c
      has = true
    } else if (c === '\\' && i + 1 < line.length) {
      cur += line[++i]
      has = true
    } else if (/\s/.test(c)) {
      if (has || cur) out.push(cur)
      cur = ''
      has = false
    } else {
      cur += c
    }
  }
  if (has || cur) out.push(cur)
  return out
}

/** Joins arguments back into a line splitArgs reads the same way. */
export function joinArgs(args: string[] | undefined): string {
  return (args ?? []).map((a) => (a !== '' && /^[\w@%+=:,./-]+$/.test(a) ? a : `'${a.replace(/'/g, `'\\''`)}'`)).join(' ')
}

/** KEY=VALUE lines → map; blank lines and # comments are skipped. */
export function parseEnv(text: string): Record<string, string> {
  const env: Record<string, string> = {}
  for (const raw of text.split('\n')) {
    const line = raw.trim()
    if (!line || line.startsWith('#')) continue
    const eq = line.indexOf('=')
    if (eq <= 0) continue
    env[line.slice(0, eq).trim()] = line.slice(eq + 1)
  }
  return env
}

export function formatEnv(env: Record<string, string | undefined> | undefined): string {
  return Object.entries(env ?? {})
    .map(([k, v]) => `${k}=${v}`)
    .join('\n')
}
