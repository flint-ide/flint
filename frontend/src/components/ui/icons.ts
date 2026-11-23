// The app's named icons, each mapped to the Lucide icon with the same meaning.
// Names are a contract (panel specs and extensions pass them as strings), so the
// set stays fixed; only the drawing is Lucide's. Pure data: Icon.tsx renders them.
import {
  ArrowDownUp,
  Cloud,
  Database,
  Files,
  GitBranch,
  GitFork,
  Pencil,
  Puzzle,
  Search,
  Ship,
  ShipWheel,
  Sigma,
  Sparkles,
  SquareCheck,
  SquareKanban,
  SquareTerminal,
  Workflow,
  type LucideIcon,
} from '@flint-ide/ui/icons'

export const ICONS = {
  files: Files, // ▤
  search: Search, // ⌕
  branch: GitBranch, // ⎇
  fork: GitFork, // ⑂
  updown: ArrowDownUp, // ⇅ push / pull
  database: Database, // ⛁
  pencil: Pencil, // ✎
  checkbox: SquareCheck, // ☑
  ship: Ship, // ⛴
  cloud: Cloud, // ☁
  helm: ShipWheel, // ☸
  terminal: SquareTerminal, // ❯_
  sum: Sigma, // ∑
  board: SquareKanban, // the Projects panel and board tab
  workflow: Workflow, // the Workflows panel and run tab
  agents: Sparkles, // ✦ the Agents panel
  puzzle: Puzzle, // extensions, and panels whose icon Flint doesn't know
} as const satisfies Record<string, LucideIcon>

export type IconName = keyof typeof ICONS

export const isIconName = (name: string): name is IconName => Object.prototype.hasOwnProperty.call(ICONS, name)
