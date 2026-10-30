import { useEffect, useRef, useState } from 'react'
import * as PreviewAPI from '../../../wailsjs/go/main/PreviewAPI'
import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime'
import { errorMessage } from '../../api/backend'
import { useEditor } from '../../store/editor'
import { useWorkspace } from '../../store/workspace'
import { headingAt, headingLines, resolveRelative } from '../../lib/previewFiles'
import { inside } from '../../lib/sessionSnapshot'

interface Props {
  path: string
  source: string
  /** First source line visible in the editor (split mode), to keep the preview in step. */
  topLine?: number
}

/** Rendered Markdown (HTML made safe in Go); links and images resolve against the file. */
export function MarkdownPreview({ path, source, topLine }: Props) {
  const [html, setHtml] = useState('')
  const [error, setError] = useState<string | null>(null)
  const box = useRef<HTMLDivElement>(null)

  useEffect(() => {
    let cancelled = false
    PreviewAPI.Markdown(source)
      .then((h) => !cancelled && (setHtml(h), setError(null)))
      .catch((err) => !cancelled && setError(errorMessage(err)))
    return () => {
      cancelled = true
    }
  }, [source])

  // Relative images load through Go (only from the open folders), as data URLs.
  useEffect(() => {
    const el = box.current
    if (!el) return
    let cancelled = false
    for (const img of el.querySelectorAll<HTMLImageElement>('img[src]')) {
      const abs = resolveRelative(path, img.getAttribute('src') ?? '')
      if (!abs) continue
      img.removeAttribute('src')
      PreviewAPI.Image(abs)
        .then((res) => {
          if (cancelled) return
          if (res.dataUrl) img.src = res.dataUrl
          else img.alt = `${img.alt || abs} (too large to preview)`
        })
        .catch(() => !cancelled && (img.alt = `${img.alt || abs} (not found)`))
    }
    return () => {
      cancelled = true
    }
  }, [html, path])

  // Split mode: show the section the editor is at.
  useEffect(() => {
    const el = box.current
    if (!el || topLine === undefined) return
    const k = headingAt(headingLines(source), topLine)
    if (k < 0) {
      el.scrollTop = 0
      return
    }
    const heading = el.querySelectorAll('h1, h2, h3, h4, h5, h6')[k] as HTMLElement | undefined
    if (heading) el.scrollTop = heading.offsetTop - 8
  }, [topLine, source, html])

  const onClick = (e: React.MouseEvent) => {
    const a = (e.target as HTMLElement).closest('a')
    if (!a) return
    e.preventDefault()
    const href = a.getAttribute('href') ?? ''
    if (href.startsWith('#')) {
      box.current?.querySelector(`[id="${CSS.escape(decodeURIComponent(href.slice(1)))}"]`)?.scrollIntoView({ block: 'start' })
      return
    }
    if (/^https?:\/\//i.test(href) || href.startsWith('mailto:')) {
      BrowserOpenURL(href)
      return
    }
    const abs = resolveRelative(path, href)
    if (abs && useWorkspace.getState().roots.some((r) => inside(abs, r))) useEditor.getState().openFile(abs)
  }

  if (error) return <div className="placeholder error">{error}</div>
  return <div ref={box} className="markdown-preview" onClick={onClick} dangerouslySetInnerHTML={{ __html: html }} />
}
