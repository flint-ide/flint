// @flint-ide/ui: Flint's UI kit. Every WunderUI component (Base UI + Tailwind,
// MIT) plus Flint's own compositions. Styles: import '@flint-ide/ui/styles.css'.
// Icons (Lucide) are a separate entry, '@flint-ide/ui/icons', because some of
// their names (Calendar, Table, Badge…) clash with components. Extension
// webviews: build the page with `go run ./cmd/flint-webview` and use flint(),
// syncFlintTheme() and '@flint-ide/ui/webview.css'.
export * from '@wunderui/react'

export { AgentRow, type AgentRowProps, type AgentState } from './AgentRow'
export { type ContextMenuApi, ContextMenuProvider, type ContextMenuProviderProps, useContextMenu } from './ContextMenu'
export { contextMenuPoint, type ContextMenuEntry, type ContextMenuItems, visibleEntries } from './menuEntries'
export { CommandPalette, type CommandPaletteProps, type PaletteCommand } from './CommandPalette'
export { DiffStat, type DiffStatProps } from './DiffStat'
export { EditorTabs, type EditorTab, type EditorTabsProps } from './EditorTabs'
export { addedBlocks, filterByLabel, moveIndex } from './filter'
export { FlameGraph, type FlameGraphProps } from './FlameGraph'
export {
  fitLabel, formatValue, frameAt, frameColor, frameAlpha, hitTest, layoutFlame, percent, searchFrames, shortName, sortTop,
  type FlameFrame, type FramePath, type FrameRect, type SearchResult, type TopRow, type TopSortKey,
} from './flame'
export { StatTile, StatTiles, type StatTileProps, type StatTileTrend } from './StatTile'
export { JobList, type Job, type JobState } from './JobList'
export { Keys, type KeysProps } from './Keys'
export { PanelHeader, type PanelHeaderProps } from './PanelHeader'
export { TreeRow, type TreeRowProps } from './TreeRow'
export { treeIndent, type TwistyState } from './treeIndent'
export { TreeSection, type TreeSectionProps } from './TreeSection'
export { ViewToolbar, type ViewToolbarProps } from './ViewToolbar'
export { StatusBar, StatusItem, type StatusItemProps, StatusSpacer } from './StatusBar'
export { applyTheme, type FlintTheme } from './theme'
export { flint, type FlintWebview, type FlintWebviewTheme, syncFlintTheme, useFlintMessages } from './webview'
