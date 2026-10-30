import type { Extension } from '@codemirror/state'
import { StreamLanguage, type StreamParser } from '@codemirror/language'

type Loader = () => Promise<Extension>

// Languages load lazily so startup only pays for what is opened.
const byExtension: Record<string, Loader> = {
  go: () => import('@codemirror/lang-go').then((m) => m.go()),
  py: () => import('@codemirror/lang-python').then((m) => m.python()),
  js: () => import('@codemirror/lang-javascript').then((m) => m.javascript({ jsx: true })),
  jsx: () => import('@codemirror/lang-javascript').then((m) => m.javascript({ jsx: true })),
  mjs: () => import('@codemirror/lang-javascript').then((m) => m.javascript()),
  ts: () => import('@codemirror/lang-javascript').then((m) => m.javascript({ typescript: true })),
  tsx: () => import('@codemirror/lang-javascript').then((m) => m.javascript({ typescript: true, jsx: true })),
  json: () => import('@codemirror/lang-json').then((m) => m.json()),
  yaml: () => import('@codemirror/lang-yaml').then((m) => m.yaml()),
  yml: () => import('@codemirror/lang-yaml').then((m) => m.yaml()),
  md: () => import('@codemirror/lang-markdown').then((m) => m.markdown()),
  html: () => import('@codemirror/lang-html').then((m) => m.html()),
  css: () => import('@codemirror/lang-css').then((m) => m.css()),
  sql: () => import('@codemirror/lang-sql').then((m) => m.sql({ dialect: m.PostgreSQL })),
  sh: () => legacy(import('@codemirror/legacy-modes/mode/shell').then((m) => m.shell)),
  bash: () => legacy(import('@codemirror/legacy-modes/mode/shell').then((m) => m.shell)),
  zsh: () => legacy(import('@codemirror/legacy-modes/mode/shell').then((m) => m.shell)),
  toml: () => legacy(import('@codemirror/legacy-modes/mode/toml').then((m) => m.toml)),
  proto: () => legacy(import('@codemirror/legacy-modes/mode/protobuf').then((m) => m.protobuf)),
  env: () => legacy(import('@codemirror/legacy-modes/mode/properties').then((m) => m.properties)),
  ini: () => legacy(import('@codemirror/legacy-modes/mode/properties').then((m) => m.properties)),
}

const byName: Record<string, Loader> = {
  Dockerfile: () => legacy(import('@codemirror/legacy-modes/mode/dockerfile').then((m) => m.dockerFile)),
  '.env': () => legacy(import('@codemirror/legacy-modes/mode/properties').then((m) => m.properties)),
}

function legacy(parser: Promise<StreamParser<unknown>>): Promise<Extension> {
  return parser.then((p) => StreamLanguage.define(p))
}

/** Returns the lowercase extension of a path ("" if none). */
export function extensionOf(path: string): string {
  const base = path.split('/').pop() ?? ''
  const dot = base.lastIndexOf('.')
  return dot > 0 ? base.slice(dot + 1).toLowerCase() : ''
}

/** Loads the CodeMirror language support for a file path (empty extension if unknown). */
export async function languageFor(path: string): Promise<Extension> {
  const base = path.split('/').pop() ?? ''
  const loader = byName[base] ?? byExtension[extensionOf(path)]
  return loader ? loader() : []
}
