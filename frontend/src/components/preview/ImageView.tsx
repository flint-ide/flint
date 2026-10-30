import { useEffect, useRef, useState } from 'react'
import * as PreviewAPI from '../../../wailsjs/go/main/PreviewAPI'
import { errorMessage } from '../../api/backend'
import { toggleSvgSource } from '../../store/preview'
import { onFileChanged } from '../../store/fsEvents'

const ZOOMS = [0.1, 0.25, 0.5, 0.75, 1, 1.5, 2, 3, 4, 6, 8]

const formatSize = (bytes: number) =>
  bytes < 1024 ? `${bytes} B` : bytes < 1024 * 1024 ? `${(bytes / 1024).toFixed(1)} KB` : `${(bytes / 1024 / 1024).toFixed(1)} MB`

/** An image file: fit to the window or zoomed (⌘+ / ⌘- / ⌘0, ⌘+wheel), on a checkerboard. */
export function ImageView({ path, active }: { path: string; active: boolean }) {
  const [img, setImg] = useState<{ url: string; size: number; tooLarge: boolean } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [natural, setNatural] = useState<{ w: number; h: number } | null>(null)
  const [zoom, setZoom] = useState<number | null>(null) // null = fit
  const [version, setVersion] = useState(0)
  const box = useRef<HTMLDivElement>(null)

  useEffect(() => onFileChanged(path, () => setVersion((v) => v + 1)), [path])
  useEffect(() => {
    let cancelled = false
    PreviewAPI.Image(path)
      .then((res) => !cancelled && (setImg({ url: res.dataUrl, size: res.size, tooLarge: res.tooLarge }), setError(null)))
      .catch((err) => !cancelled && setError(errorMessage(err)))
    return () => {
      cancelled = true
    }
  }, [path, version])

  const fitScale = () => {
    const el = box.current
    if (!el || !natural) return 1
    return Math.min(1, (el.clientWidth - 32) / natural.w, (el.clientHeight - 32) / natural.h)
  }
  const step = (dir: 1 | -1) => {
    const cur = zoom ?? fitScale()
    const next = dir > 0 ? ZOOMS.find((z) => z > cur + 1e-6) : [...ZOOMS].reverse().find((z) => z < cur - 1e-6)
    setZoom(next ?? cur)
  }

  useEffect(() => {
    if (!active) return
    const onKey = (e: KeyboardEvent) => {
      if (!e.metaKey || e.altKey || e.ctrlKey) return
      if (e.key === '=' || e.key === '+') step(1)
      else if (e.key === '-') step(-1)
      else if (e.key === '0') setZoom(null)
      else return
      e.preventDefault()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  if (error) return <div className="placeholder error">{error}</div>
  if (!img) return <div className="placeholder">Loading…</div>
  if (img.tooLarge) return <div className="placeholder">Image too large to preview ({formatSize(img.size)}).</div>

  const scale = zoom ?? fitScale()
  const svg = /\.svg$/i.test(path)
  return (
    <div className="image-view">
      <div className="run-toolbar">
        <button className="icon" title="Zoom out (⌘-)" onClick={() => step(-1)}>−</button>
        <button className="icon" title="Fit (⌘0)" onClick={() => setZoom(null)}>{zoom === null ? 'Fit' : `${Math.round(scale * 100)}%`}</button>
        <button className="icon" title="Zoom in (⌘+)" onClick={() => step(1)}>+</button>
        <button className="icon" title="Actual size" onClick={() => setZoom(1)}>1:1</button>
        {svg && (
          <button className="icon" title="Edit the SVG source" onClick={() => toggleSvgSource(path)}>Source</button>
        )}
        <span className="run-status">
          {natural ? `${natural.w} × ${natural.h} · ` : ''}
          {formatSize(img.size)}
        </span>
      </div>
      <div
        ref={box}
        className="image-stage"
        onWheel={(e) => {
          if (!e.metaKey) return
          e.preventDefault()
          step(e.deltaY < 0 ? 1 : -1)
        }}
      >
        <img
          className="image-checker"
          src={img.url}
          alt={path.split('/').pop()}
          draggable={false}
          onLoad={(e) => setNatural({ w: e.currentTarget.naturalWidth, h: e.currentTarget.naturalHeight })}
          style={natural ? { width: natural.w * scale, height: natural.h * scale } : { maxWidth: '100%', maxHeight: '100%' }}
        />
      </div>
    </div>
  )
}
