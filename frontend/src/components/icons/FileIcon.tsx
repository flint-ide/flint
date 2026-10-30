import { memo, type ReactNode } from 'react'

// Dependency-free file icons in the spirit of IntelliJ's new UI: an outlined page
// with a folded corner and a solid language badge, two-tone folders, and a few
// glyph icons (images, lock files, git). Everything is inline SVG.

interface Badge {
  label: string
  bg: string
  fg?: string // label colour; white unless the badge is light
}

const DARK = '#1b1c1f'

const BY_EXT: Record<string, Badge> = {
  go: { label: 'GO', bg: '#00a7d0' },
  ts: { label: 'TS', bg: '#3178c6' },
  tsx: { label: 'TSX', bg: '#2f74c0' },
  mts: { label: 'TS', bg: '#3178c6' },
  cts: { label: 'TS', bg: '#3178c6' },
  js: { label: 'JS', bg: '#e8cd2c', fg: DARK },
  jsx: { label: 'JSX', bg: '#d9bf22', fg: DARK },
  mjs: { label: 'JS', bg: '#e8cd2c', fg: DARK },
  cjs: { label: 'JS', bg: '#e8cd2c', fg: DARK },
  json: { label: '{}', bg: '#c9a227', fg: DARK },
  jsonc: { label: '{}', bg: '#c9a227', fg: DARK },
  yaml: { label: 'YML', bg: '#c4452e' },
  yml: { label: 'YML', bg: '#c4452e' },
  toml: { label: 'TML', bg: '#9c5a32' },
  md: { label: 'MD', bg: '#4a8fc0' },
  mdx: { label: 'MDX', bg: '#4a8fc0' },
  sql: { label: 'SQL', bg: '#d98a12' },
  py: { label: 'PY', bg: '#3572a5' },
  pyi: { label: 'PY', bg: '#3572a5' },
  rs: { label: 'RS', bg: '#c45a2c' },
  html: { label: '<>', bg: '#e1532f' },
  xml: { label: '<>', bg: '#d0742f' },
  css: { label: 'CSS', bg: '#6e4cb8' },
  scss: { label: 'SC', bg: '#c6538c' },
  sh: { label: '$_', bg: '#3f9a3b' },
  bash: { label: '$_', bg: '#3f9a3b' },
  zsh: { label: '$_', bg: '#3f9a3b' },
  mk: { label: 'MK', bg: '#6c7a89' },
  env: { label: 'ENV', bg: '#b8941a', fg: DARK },
  proto: { label: 'PB', bg: '#3d7fc1' },
  csv: { label: 'CSV', bg: '#2a8a52' },
  tsv: { label: 'TSV', bg: '#2a8a52' },
  java: { label: 'J', bg: '#b4651e' },
  kt: { label: 'KT', bg: '#7f52ff' },
  c: { label: 'C', bg: '#5a6c84' },
  h: { label: 'H', bg: '#5a6c84' },
  cpp: { label: 'C++', bg: '#d0436f' },
  rb: { label: 'RB', bg: '#c22f2a' },
  php: { label: 'PHP', bg: '#6f73b0' },
  swift: { label: 'SW', bg: '#e8553a' },
  tf: { label: 'TF', bg: '#7444c4' },
  hcl: { label: 'HCL', bg: '#7444c4' },
  graphql: { label: 'GQL', bg: '#d6339c' },
  http: { label: 'API', bg: '#2a8a7a' },
  rest: { label: 'API', bg: '#2a8a7a' },
  vue: { label: 'VUE', bg: '#3a9b6f' },
  svelte: { label: 'SV', bg: '#e8552f' },
  dart: { label: 'DT', bg: '#2a8ac2' },
  lua: { label: 'LUA', bg: '#33338c' },
}

const BY_NAME: Record<string, Badge> = {
  'go.mod': { label: 'MOD', bg: '#00a7d0' },
  'go.sum': { label: 'SUM', bg: '#4e7e8c' },
  'go.work': { label: 'WRK', bg: '#00a7d0' },
  'package.json': { label: 'NPM', bg: '#c4302b' },
  'package-lock.json': { label: 'NPM', bg: '#7b2a27' },
  'tsconfig.json': { label: 'TS', bg: '#3178c6' },
  'Cargo.toml': { label: 'RS', bg: '#c45a2c' },
  Makefile: { label: 'MK', bg: '#6c7a89' },
  makefile: { label: 'MK', bg: '#6c7a89' },
  GNUmakefile: { label: 'MK', bg: '#6c7a89' },
  Dockerfile: { label: 'DOC', bg: '#1f8ce0' },
  'docker-compose.yml': { label: 'DOC', bg: '#1f8ce0' },
  'docker-compose.yaml': { label: 'DOC', bg: '#1f8ce0' },
  'compose.yaml': { label: 'DOC', bg: '#1f8ce0' },
  'README.md': { label: 'i', bg: '#4a8fc0' },
  'CLAUDE.md': { label: 'AI', bg: '#d97757' },
  'AGENTS.md': { label: 'AI', bg: '#d97757' },
  LICENSE: { label: '©', bg: '#a8892a' },
  '.env': { label: 'ENV', bg: '#b8941a', fg: DARK },
  '.npmrc': { label: 'NPM', bg: '#7b2a27' },
  '.editorconfig': { label: 'EC', bg: '#5f6b78' },
}

type Glyph = 'image' | 'lock' | 'git' | 'text'

const GLYPH_EXT: Record<string, Glyph> = {
  png: 'image', jpg: 'image', jpeg: 'image', gif: 'image', webp: 'image', ico: 'image', svg: 'image', bmp: 'image',
  lock: 'lock', txt: 'text', log: 'text',
}

const GLYPH_NAME: Record<string, Glyph> = {
  '.gitignore': 'git', '.gitattributes': 'git', '.gitmodules': 'git', '.gitkeep': 'git',
  'yarn.lock': 'lock', 'pnpm-lock.yaml': 'lock', 'Cargo.lock': 'lock', 'poetry.lock': 'lock', 'bun.lockb': 'lock',
}

function lookup(name: string): { badge?: Badge; glyph?: Glyph } {
  if (GLYPH_NAME[name]) return { glyph: GLYPH_NAME[name] }
  if (BY_NAME[name]) return { badge: BY_NAME[name] }
  if (name.startsWith('.env')) return { badge: BY_NAME['.env'] }
  if (name.startsWith('Dockerfile')) return { badge: BY_NAME.Dockerfile }
  const dot = name.lastIndexOf('.')
  if (dot < 0) return {}
  const ext = name.slice(dot + 1).toLowerCase()
  if (GLYPH_EXT[ext]) return { glyph: GLYPH_EXT[ext] }
  return { badge: BY_EXT[ext] }
}

// Page outline with folded corner; the badge sits over its lower-left part.
const PAGE = 'M4 1.5h5.3l3.2 3.2v8.8a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V2.5a1 1 0 0 1 1-1z'
const FOLD = 'M9.2 1.6v2.6a.6.6 0 0 0 .6.6h2.6'
const PAGE_STROKE = '#868c97'
const PAGE_FILL = '#25272c'

function Svg({ children, className = 'file-icon' }: { children: ReactNode; className?: string }) {
  return (
    <svg className={className} width="16" height="16" viewBox="0 0 16 16" aria-hidden>
      {children}
    </svg>
  )
}

function Page({ withFold = true }: { withFold?: boolean }) {
  return (
    <>
      <path d={PAGE} fill={PAGE_FILL} stroke={PAGE_STROKE} strokeWidth="1" strokeLinejoin="round" />
      {withFold && <path d={FOLD} fill="none" stroke={PAGE_STROKE} strokeWidth="1" strokeLinejoin="round" />}
    </>
  )
}

function BadgeIcon({ badge }: { badge: Badge }) {
  const chars = [...badge.label].length
  const width = chars <= 1 ? 7 : chars === 2 ? 9.5 : 12.5
  const fontSize = chars >= 3 ? 4.6 : 5.6
  return (
    <Svg>
      <Page />
      <rect x="0.5" y="7.5" width={width} height="7" rx="1.6" fill={badge.bg} />
      <text
        x={0.5 + width / 2}
        y="12.9"
        textAnchor="middle"
        fontSize={fontSize}
        fontWeight="800"
        letterSpacing="-0.15"
        fontFamily="-apple-system, 'SF Pro Text', 'Helvetica Neue', sans-serif"
        fill={badge.fg ?? '#ffffff'}
      >
        {badge.label}
      </text>
    </Svg>
  )
}

function GlyphIcon({ glyph }: { glyph: Glyph }) {
  switch (glyph) {
    case 'image':
      return (
        <Svg>
          <rect x="1.5" y="2.5" width="13" height="11" rx="2" fill="#2a2440" stroke="#a78bfa" />
          <circle cx="5.3" cy="6" r="1.3" fill="#c4b5fd" />
          <path d="M2.5 12.2l3.4-3.6 2.3 2.3 2.1-2.4 3.2 3.7" fill="none" stroke="#a78bfa" strokeLinejoin="round" />
        </Svg>
      )
    case 'lock':
      return (
        <Svg>
          <Page />
          <rect x="1" y="8.5" width="8" height="6" rx="1.3" fill="#8a8f99" />
          <path d="M3 8.5V7.2a2 2 0 0 1 4 0v1.3" fill="none" stroke="#8a8f99" strokeWidth="1.2" />
        </Svg>
      )
    case 'git':
      return (
        <Svg>
          <path d="M8 1.2l6.8 6.8L8 14.8 1.2 8z" fill="#e8553a" />
          <circle cx="6.4" cy="5.6" r="1.1" fill="#fff" />
          <circle cx="6.4" cy="10.4" r="1.1" fill="#fff" />
          <circle cx="10" cy="8" r="1.1" fill="#fff" />
          <path d="M6.4 5.6v4.8M6.4 6.2l3.2 1.6" stroke="#fff" strokeWidth="0.9" />
        </Svg>
      )
    case 'text':
      return (
        <Svg>
          <Page />
          <path d="M5 7.5h5M5 9.5h5M5 11.5h3.5" stroke={PAGE_STROKE} strokeWidth="1" strokeLinecap="round" />
        </Svg>
      )
  }
}

// Folder tints by name, like IntelliJ's source/test/resource roots.
const FOLDER_TINTS: Record<string, [string, string]> = {
  src: ['#3b6fb0', '#5b90d6'],
  test: ['#2f7d4f', '#4caf72'],
  tests: ['#2f7d4f', '#4caf72'],
  __tests__: ['#2f7d4f', '#4caf72'],
  docs: ['#6b4fa8', '#9479d0'],
  '.github': ['#4b5563', '#6b7280'],
  '.git': ['#a8452f', '#d06247'],
  node_modules: ['#4a5a3c', '#6b7f57'],
  dist: ['#5b5f66', '#7b8089'],
  build: ['#5b5f66', '#7b8089'],
  vendor: ['#5b5f66', '#7b8089'],
  internal: ['#2c7a85', '#45a2ae'],
  cmd: ['#2c7a85', '#45a2ae'],
}
const DEFAULT_FOLDER: [string, string] = ['#5f7493', '#8299bb']

function FolderIcon({ name, open }: { name: string; open?: boolean }) {
  const [back, front] = FOLDER_TINTS[name] ?? (name.startsWith('.') ? ['#4b5563', '#6b7280'] : DEFAULT_FOLDER)
  return (
    <Svg>
      <path d="M1.5 3.6a1.1 1.1 0 0 1 1.1-1.1h3.3c.3 0 .6.1.8.4l1 1.1h5.7a1.1 1.1 0 0 1 1.1 1.1v7.3a1.1 1.1 0 0 1-1.1 1.1H2.6a1.1 1.1 0 0 1-1.1-1.1z" fill={back} />
      {open ? (
        <path d="M3.4 6.6h11.3c.5 0 .8.5.6.9l-2 4.9a1 1 0 0 1-.9.6H2.2c-.5 0-.8-.5-.6-.9l1.9-4.9a1 1 0 0 1 .9-.6z" fill={front} />
      ) : (
        <path d="M1.5 6.1h13v6.3a1.1 1.1 0 0 1-1.1 1.1H2.6a1.1 1.1 0 0 1-1.1-1.1z" fill={front} />
      )}
    </Svg>
  )
}

interface Props {
  name: string
  isDir?: boolean
  open?: boolean
}

/** 16px icon for a file or folder name. */
export const FileIcon = memo(function FileIcon({ name, isDir, open }: Props) {
  if (isDir) return <FolderIcon name={name} open={open} />
  const { badge, glyph } = lookup(name)
  if (badge) return <BadgeIcon badge={badge} />
  if (glyph) return <GlyphIcon glyph={glyph} />
  return (
    <Svg>
      <Page />
    </Svg>
  )
})

/** Icons for non-file tabs. */
export function TabKindIcon({ kind, busy }: { kind: 'terminal' | 'chat' | 'diff' | 'history' | 'sql' | 'markdown' | 'text' | 'run' | 'debug' | 'profile' | 'http'; busy?: boolean }) {
  switch (kind) {
    case 'profile':
      return (
        <Svg className={`file-icon${busy ? ' pulse' : ''}`}>
          <rect x="1.5" y="2.5" width="13" height="3" rx="0.8" fill="#9a4a1c" />
          <rect x="1.5" y="6.5" width="9" height="3" rx="0.8" fill="#c2671c" />
          <rect x="11" y="6.5" width="3.5" height="3" rx="0.8" fill="#58657d" />
          <rect x="1.5" y="10.5" width="5.5" height="3" rx="0.8" fill="#f59e0b" />
        </Svg>
      )
    case 'http':
      return (
        <Svg className={`file-icon${busy ? ' pulse' : ''}`}>
          <rect x="1.5" y="2.5" width="13" height="11" rx="2.2" fill="#1d2a2a" stroke="#4fb8a8" />
          <path d="M4 6.2h7.2M9.4 4.6l1.8 1.6-1.8 1.6M12 9.8H4.8M6.6 8.2L4.8 9.8l1.8 1.6" fill="none" stroke="#7fe0cf" strokeWidth="1.1" strokeLinecap="round" strokeLinejoin="round" />
        </Svg>
      )
    case 'debug':
      return (
        <Svg className={`file-icon${busy ? ' pulse' : ''}`}>
          <ellipse cx="8" cy="9.2" rx="3.6" ry="4.3" fill="#3b2a1d" stroke="#f0a35e" />
          <path d="M8 5v8.4M4.4 9.2H2.3M13.7 9.2h-2.1M5 6.4L3.2 4.9M11 6.4l1.8-1.5M5 12l-1.8 1.4M11 12l1.8 1.4M6.3 4.9a1.7 1.7 0 0 1 3.4 0" fill="none" stroke="#f0a35e" strokeWidth="1.1" strokeLinecap="round" />
        </Svg>
      )
    case 'run':
      return (
        <Svg className={`file-icon${busy ? ' pulse' : ''}`}>
          <path d="M4.5 2.8v10.4l8.6-5.2z" fill="#5fbf7f" stroke="#86efac" strokeWidth="0.9" strokeLinejoin="round" />
        </Svg>
      )
    case 'terminal':
      return (
        <Svg>
          <rect x="1.5" y="2.5" width="13" height="11" rx="2.2" fill="#1d2a22" stroke="#5fbf7f" />
          <path d="M4.3 6.2l2.3 1.8-2.3 1.8" fill="none" stroke="#86efac" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M8 10.4h3.6" stroke="#86efac" strokeWidth="1.3" strokeLinecap="round" />
        </Svg>
      )
    case 'chat':
      return (
        <Svg className={`file-icon${busy ? ' pulse' : ''}`}>
          <path d="M8 1.3l1.7 4.9 5 1.8-5 1.8L8 14.7l-1.7-4.9-5-1.8 5-1.8z" fill="#f59e0b" />
        </Svg>
      )
    case 'diff':
      return (
        <Svg>
          <Page />
          <rect x="0.5" y="7.5" width="9.5" height="7" rx="1.6" fill="#4a7fd4" />
          <path d="M3 10.9h3.4M4.7 9.2v3.4" stroke="#fff" strokeWidth="1.1" strokeLinecap="round" />
          <path d="M7.2 12.6h1.6" stroke="#fff" strokeWidth="1.1" strokeLinecap="round" />
        </Svg>
      )
    case 'history':
      return (
        <Svg>
          <circle cx="8" cy="8" r="6.2" fill="#2a3446" stroke="#82aaff" />
          <path d="M8 4.6V8l2.4 1.6" fill="none" stroke="#c7d7ff" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
        </Svg>
      )
    case 'sql':
      return <FileIcon name="x.sql" />
    case 'markdown':
      return <FileIcon name="x.md" />
    default:
      return <FileIcon name="x.txt" />
  }
}
