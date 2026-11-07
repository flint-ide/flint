// Types a scratch file can have (IntelliJ's "New Scratch File" list): each is a
// real file with that extension, so the editor's language support and tools such
// as the HTTP client work in it. Pure, so it is testable.
import { HTTP_FILE_TEMPLATE } from './httpFiles'

export interface ScratchFileType {
  id: string
  label: string
  /** File name a new scratch gets (numbered by Go when taken). */
  fileName: string
  /** Starting content, and the 1-based line the cursor lands on. */
  template?: string
  line?: number
  /** Search words beyond the label (extension, aliases). */
  keywords?: string
}

export const SCRATCH_FILE_TYPES: readonly ScratchFileType[] = [
  { id: 'http', label: 'HTTP Request', fileName: 'requests.http', template: HTTP_FILE_TEMPLATE, line: 7, keywords: 'rest api curl client' },
  { id: 'json', label: 'JSON', fileName: 'scratch.json', template: '{\n  \n}\n', line: 2 },
  { id: 'yaml', label: 'YAML', fileName: 'scratch.yaml', keywords: 'yml k8s' },
  { id: 'go', label: 'Go', fileName: 'scratch.go', template: 'package main\n\nfunc main() {\n\t\n}\n', line: 4, keywords: 'golang' },
  { id: 'ts', label: 'TypeScript', fileName: 'scratch.ts' },
  { id: 'js', label: 'JavaScript', fileName: 'scratch.js', keywords: 'node' },
  { id: 'py', label: 'Python', fileName: 'scratch.py' },
  { id: 'sh', label: 'Shell Script', fileName: 'scratch.sh', template: '#!/bin/sh\n\n', line: 3, keywords: 'bash zsh' },
  { id: 'sql', label: 'SQL File', fileName: 'scratch.sql', keywords: 'query (not a console)' },
  { id: 'md', label: 'Markdown File', fileName: 'scratch.md' },
  { id: 'html', label: 'HTML', fileName: 'scratch.html' },
  { id: 'css', label: 'CSS', fileName: 'scratch.css' },
  { id: 'xml', label: 'XML', fileName: 'scratch.xml' },
  { id: 'toml', label: 'TOML', fileName: 'scratch.toml' },
  { id: 'rs', label: 'Rust', fileName: 'scratch.rs' },
  { id: 'java', label: 'Java', fileName: 'Scratch.java' },
  { id: 'txt', label: 'Plain Text File', fileName: 'scratch.txt' },
]

export const scratchFileType = (id: string): ScratchFileType | undefined => SCRATCH_FILE_TYPES.find((t) => t.id === id)

/** The extension of a scratch file's name, upper-cased for its badge ("HTTP"); "" without one. */
export function scratchBadge(name: string): string {
  const dot = name.lastIndexOf('.')
  return dot > 0 ? name.slice(dot + 1).toUpperCase().slice(0, 4) : ''
}
