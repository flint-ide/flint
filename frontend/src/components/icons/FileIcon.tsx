import { memo } from 'react'

// Small, dependency-free file-type icons: a tinted badge with a short label per
// type (in the spirit of Seti/IntelliJ), plus folder, terminal and chat glyphs.

interface Badge {
  label: string
  color: string
}

const BY_EXT: Record<string, Badge> = {
  go: { label: 'GO', color: '#00add8' },
  mod: { label: 'GO', color: '#00add8' },
  sum: { label: 'GO', color: '#5c7d8a' },
  ts: { label: 'TS', color: '#3b82f6' },
  tsx: { label: 'TS', color: '#38bdf8' },
  mts: { label: 'TS', color: '#3b82f6' },
  cts: { label: 'TS', color: '#3b82f6' },
  js: { label: 'JS', color: '#eab308' },
  jsx: { label: 'JS', color: '#38bdf8' },
  mjs: { label: 'JS', color: '#eab308' },
  cjs: { label: 'JS', color: '#eab308' },
  json: { label: '{}', color: '#ca8a04' },
  jsonc: { label: '{}', color: '#ca8a04' },
  yaml: { label: 'YM', color: '#e06c75' },
  yml: { label: 'YM', color: '#e06c75' },
  toml: { label: 'TL', color: '#b07a52' },
  md: { label: 'M↓', color: '#60a5fa' },
  mdx: { label: 'M↓', color: '#60a5fa' },
  sql: { label: 'SQ', color: '#f59e0b' },
  py: { label: 'PY', color: '#4b8bbe' },
  pyi: { label: 'PY', color: '#4b8bbe' },
  rs: { label: 'RS', color: '#dea584' },
  html: { label: '<>', color: '#e34c26' },
  xml: { label: '<>', color: '#e37933' },
  svg: { label: 'SV', color: '#f59e0b' },
  css: { label: '#', color: '#8b7fd6' },
  scss: { label: '#', color: '#cd6799' },
  sh: { label: '$', color: '#89e051' },
  bash: { label: '$', color: '#89e051' },
  zsh: { label: '$', color: '#89e051' },
  env: { label: '·E', color: '#eab308' },
  proto: { label: 'PB', color: '#7dd3fc' },
  csv: { label: 'CS', color: '#22c55e' },
  txt: { label: '≡', color: '#9ca3af' },
  log: { label: '≡', color: '#9ca3af' },
  lock: { label: '🔒', color: '#9ca3af' },
  png: { label: 'IM', color: '#a78bfa' },
  jpg: { label: 'IM', color: '#a78bfa' },
  jpeg: { label: 'IM', color: '#a78bfa' },
  gif: { label: 'IM', color: '#a78bfa' },
  webp: { label: 'IM', color: '#a78bfa' },
  ico: { label: 'IM', color: '#a78bfa' },
  java: { label: 'J', color: '#f89820' },
  kt: { label: 'K', color: '#a97bff' },
  c: { label: 'C', color: '#6b9bd2' },
  h: { label: 'H', color: '#6b9bd2' },
  cpp: { label: 'C+', color: '#f34b7d' },
  rb: { label: 'RB', color: '#e0115f' },
  php: { label: 'PH', color: '#8892bf' },
  swift: { label: 'SW', color: '#f05138' },
  tf: { label: 'TF', color: '#844fba' },
  hcl: { label: 'TF', color: '#844fba' },
}

const BY_NAME: Record<string, Badge> = {
  Dockerfile: { label: 'DK', color: '#2496ed' },
  'docker-compose.yml': { label: 'DK', color: '#2496ed' },
  'docker-compose.yaml': { label: 'DK', color: '#2496ed' },
  Makefile: { label: 'MK', color: '#9ca3af' },
  '.gitignore': { label: 'GI', color: '#f14e32' },
  '.gitattributes': { label: 'GI', color: '#f14e32' },
  '.gitmodules': { label: 'GI', color: '#f14e32' },
  '.env': { label: '·E', color: '#eab308' },
  'package.json': { label: 'NP', color: '#cb3837' },
  'package-lock.json': { label: 'NP', color: '#7a2a29' },
  'go.mod': { label: 'GO', color: '#00add8' },
  'go.sum': { label: 'GO', color: '#5c7d8a' },
  'Cargo.toml': { label: 'RS', color: '#dea584' },
  LICENSE: { label: '©', color: '#d4a72c' },
  'README.md': { label: 'i', color: '#60a5fa' },
  'CLAUDE.md': { label: '✦', color: '#d97757' },
  'AGENTS.md': { label: '✦', color: '#d97757' },
}

function badgeFor(name: string): Badge | null {
  if (BY_NAME[name]) return BY_NAME[name]
  if (name.startsWith('.env')) return BY_NAME['.env']
  if (name.startsWith('Dockerfile')) return BY_NAME.Dockerfile
  const dot = name.lastIndexOf('.')
  return dot > 0 || (dot === 0 && name.length > 1) ? (BY_EXT[name.slice(dot + 1).toLowerCase()] ?? null) : null
}

interface Props {
  name: string
  isDir?: boolean
  open?: boolean
}

/** 16px icon for a file or folder name. */
export const FileIcon = memo(function FileIcon({ name, isDir, open }: Props) {
  if (isDir) return <FolderIcon open={open} special={name.startsWith('.')} />
  const badge = badgeFor(name)
  if (!badge) return <GenericFile />
  const small = badge.label.length > 1
  return (
    <svg className="file-icon" width="16" height="16" viewBox="0 0 16 16" aria-hidden>
      <rect x="1" y="2" width="14" height="12" rx="3" fill={badge.color} fillOpacity="0.16" />
      <text
        x="8"
        y="11"
        textAnchor="middle"
        fontSize={small ? 6.6 : 8.5}
        fontWeight="700"
        fontFamily="-apple-system, 'SF Pro Text', sans-serif"
        fill={badge.color}
      >
        {badge.label}
      </text>
    </svg>
  )
})

function FolderIcon({ open, special }: { open?: boolean; special?: boolean }) {
  const color = special ? '#6b7280' : '#8fa4c2'
  return (
    <svg className="file-icon" width="16" height="16" viewBox="0 0 16 16" aria-hidden>
      {open ? (
        <path d="M1.5 4.5a1 1 0 0 1 1-1h3.2l1.3 1.4h5.5a1 1 0 0 1 1 1V6H4.3a1 1 0 0 0-.95.68L1.5 12.2z M3.4 7h11.3l-1.9 5.6a.6.6 0 0 1-.57.4H1.9z" fill={color} />
      ) : (
        <path d="M1.5 4.5a1 1 0 0 1 1-1h3.2l1.3 1.4h6.5a1 1 0 0 1 1 1v6.6a1 1 0 0 1-1 1H2.5a1 1 0 0 1-1-1z" fill={color} />
      )}
    </svg>
  )
}

function GenericFile() {
  return (
    <svg className="file-icon" width="16" height="16" viewBox="0 0 16 16" aria-hidden>
      <path d="M4 1.5h5.2L12.5 4.8V14a.5.5 0 0 1-.5.5H4a.5.5 0 0 1-.5-.5V2a.5.5 0 0 1 .5-.5z" fill="none" stroke="#7a808c" />
      <path d="M9 1.8V5h3.2" fill="none" stroke="#7a808c" />
    </svg>
  )
}

/** Icons for non-file tabs. */
export function TabKindIcon({ kind, busy }: { kind: 'terminal' | 'chat' | 'diff' | 'sql' | 'markdown' | 'text'; busy?: boolean }) {
  switch (kind) {
    case 'terminal':
      return (
        <svg className="file-icon" width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <rect x="1" y="2" width="14" height="12" rx="2.5" fill="#86efac" fillOpacity="0.14" />
          <path d="M4 6l2.5 2L4 10" fill="none" stroke="#86efac" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M8 10.5h4" stroke="#86efac" strokeWidth="1.4" strokeLinecap="round" />
        </svg>
      )
    case 'chat':
      return (
        <svg className={`file-icon${busy ? ' pulse' : ''}`} width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <path d="M8 1.5l1.6 4.9L14.5 8l-4.9 1.6L8 14.5l-1.6-4.9L1.5 8l4.9-1.6z" fill="#f59e0b" />
        </svg>
      )
    case 'diff':
      return (
        <svg className="file-icon" width="16" height="16" viewBox="0 0 16 16" aria-hidden>
          <rect x="1" y="2" width="14" height="12" rx="3" fill="#82aaff" fillOpacity="0.16" />
          <path d="M5 6h3M6.5 4.5v3M5 11h3" stroke="#82aaff" strokeWidth="1.3" strokeLinecap="round" />
          <path d="M10 5v6" stroke="#82aaff" strokeWidth="1.3" strokeLinecap="round" opacity="0.6" />
        </svg>
      )
    case 'sql':
      return <FileIcon name="x.sql" />
    case 'markdown':
      return <FileIcon name="x.md" />
    default:
      return <FileIcon name="x.txt" />
  }
}
