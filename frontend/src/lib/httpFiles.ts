// Pure helpers of the HTTP client (.http / .rest files); no React, no stores, so
// node:test covers them.

/** Whether a path is a request file the HTTP client handles. */
export function isHttpFile(path: string): boolean {
  return /\.(http|rest)$/i.test(path)
}

/** The folder whose http-client.env.json files apply to a request file. */
export function envDir(path: string): string {
  const slash = path.lastIndexOf('/')
  return slash > 0 ? path.slice(0, slash) : '/'
}

/** "1.2 KB" style sizes for response bodies. */
export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

/** Response time: whole milliseconds below a second, else seconds. */
export function formatTime(ms: number): string {
  if (ms < 1) return '<1 ms'
  if (ms < 1000) return `${Math.round(ms)} ms`
  return `${(ms / 1000).toFixed(2)} s`
}

export type StatusTone = 'ok' | 'redirect' | 'client-error' | 'server-error' | 'info'

/** Colour class of an HTTP status code. */
export function statusTone(status: number): StatusTone {
  if (status >= 500) return 'server-error'
  if (status >= 400) return 'client-error'
  if (status >= 300) return 'redirect'
  if (status >= 200) return 'ok'
  return 'info'
}

const EXTENSIONS: Record<string, string> = {
  'application/json': 'json',
  'application/xml': 'xml',
  'text/xml': 'xml',
  'text/html': 'html',
  'text/plain': 'txt',
  'text/csv': 'csv',
  'image/png': 'png',
  'image/jpeg': 'jpg',
  'image/gif': 'gif',
  'image/svg+xml': 'svg',
  'image/webp': 'webp',
  'application/pdf': 'pdf',
  'application/zip': 'zip',
}

/** A file name to suggest for "Save to file": the URL's last segment with a fitting extension. */
export function suggestedFileName(url: string, contentType: string, kind: string): string {
  let last = ''
  try {
    last = decodeURIComponent(new URL(url).pathname.split('/').filter(Boolean).pop() ?? '')
  } catch {
    // An unparsable URL just gets the default name.
  }
  const base = last.replace(/[^\w.-]+/g, '_') || 'response'
  if (/\.\w{1,5}$/.test(base)) return base
  const ext = EXTENSIONS[contentType] ?? (kind === 'json' ? 'json' : kind === 'xml' ? 'xml' : kind === 'binary' || kind === 'image' ? 'bin' : 'txt')
  return `${base}.${ext}`
}

/** Path of the language to highlight a response body with (fed to languageFor). */
export function bodyLanguagePath(kind: string): string {
  switch (kind) {
    case 'json':
      return 'body.json'
    case 'xml':
      return 'body.xml'
    case 'html':
      return 'body.html'
    default:
      return 'body.txt'
  }
}

/** The first free name among requests.http, requests-2.http, … given the taken names. */
export function freeHttpFileName(taken: ReadonlySet<string>): string {
  if (!taken.has('requests.http')) return 'requests.http'
  for (let i = 2; ; i++) {
    const name = `requests-${i}.http`
    if (!taken.has(name)) return name
  }
}

/** Content of a new request file. */
export const HTTP_FILE_TEMPLATE = `# Requests to your API. Put the cursor in a request and press ⌘↵ (or click ▶) to send it.
# Variables: @name = value here, or per environment in http-client.env.json next to this file
# (secrets in http-client.private.env.json, which belongs in .gitignore).
@baseUrl = http://localhost:8080

### Health check
GET {{baseUrl}}/health
Accept: application/json

### Create an item
POST {{baseUrl}}/items
Content-Type: application/json

{
  "id": "{{$uuid}}",
  "createdAt": {{$timestamp}}
}
`
