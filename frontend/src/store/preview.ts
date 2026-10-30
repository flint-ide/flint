import { createStore } from '../lib/store'

/** How a Markdown file is shown: source only, side by side, or the rendering only. */
export type MarkdownMode = 'editor' | 'split' | 'preview'

interface PreviewState {
  /** Markdown files not in plain editor mode, by path. */
  markdown: Record<string, MarkdownMode>
  /** SVG files shown as source text instead of the image. */
  svgSource: Record<string, boolean>
}

export const usePreview = createStore<PreviewState>(() => ({ markdown: {}, svgSource: {} }))

export const markdownMode = (path: string, s: PreviewState = usePreview.getState()): MarkdownMode => s.markdown[path] ?? 'editor'

export function setMarkdownMode(path: string, mode: MarkdownMode): void {
  usePreview.setState((s) => {
    const { [path]: _old, ...rest } = s.markdown
    return { markdown: mode === 'editor' ? rest : { ...rest, [path]: mode } }
  })
}

export function toggleSvgSource(path: string): void {
  usePreview.setState((s) => {
    const { [path]: on, ...rest } = s.svgSource
    return { svgSource: on ? rest : { ...rest, [path]: true } }
  })
}
