import { useEffect, useRef, useState } from 'react'
import * as PreviewAPI from '../../../bindings/flint/previewapi'
import { BrowserOpenURL } from '../../wails/runtime'
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
  const [remoteLoaded, setRemoteLoaded] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  const blocked = !remoteLoaded && html.includes('data-remote-src=')

  // Remote images arrive without src (Go moved it aside): a file opened in
  // Flint must not tell a tracker who opened it. One click loads them.
  const loadRemote = () => setRemoteLoaded(true)
  useEffect(() => setRemoteLoaded(false), [path])
  useEffect(() => {
    if (!remoteLoaded) return
    for (const img of box.current?.querySelectorAll<HTMLImageElement>('img[data-remote-src]') ?? []) {
      const src = img.dataset.remoteSrc ?? ''
      if (/^https?:\/\//i.test(src)) img.src = src
    }
  }, [html, remoteLoaded])

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
  return (
    <>
      {blocked && (
        <div className="preview-remote-bar">
          Remote images are blocked so the file can't track you.
          <button className="link" onClick={loadRemote}>Load remote images</button>
        </div>
      )}
      <div ref={box} className="markdown-preview" onClick={onClick} dangerouslySetInnerHTML={{ __html: html }} />
    </>
  )
}
