// Single entry point to the Go backend: re-exports generated bindings under readable names
// and adds types for event payloads the generator can't see.
import * as Workspace from '../../wailsjs/go/main/WorkspaceAPI'
import * as Search from '../../wailsjs/go/main/SearchAPI'
import * as Database from '../../wailsjs/go/main/DatabaseAPI'
import * as Scratch from '../../wailsjs/go/main/ScratchAPI'
import * as Diff from '../../wailsjs/go/main/DiffAPI'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { db, scratch, search, textdiff, workspace } from '../../wailsjs/go/models'

export { Workspace, Search, Database, Scratch, Diff }

export type Entry = workspace.Entry
export type FileContent = workspace.FileContent
export type Connection = db.Connection
export type QueryResult = db.QueryResult
export type Table = db.Table
export type DatabaseInfo = db.DatabaseInfo
export type ScratchPad = scratch.Scratch
export type Hunk = textdiff.Hunk
export type LineMark = textdiff.LineMark
export type SearchOptions = search.Options
export type Driver = 'postgres' | 'clickhouse'
export type ScratchKind = 'sql' | 'markdown' | 'text'
export type ClickHouseProtocol = 'native' | 'http'

// Event payloads aren't part of any bound method signature, so Wails doesn't generate them.
export interface SearchMatch {
  path: string
  line: number
  text: string
  ranges: { start: number; end: number }[]
}

export interface SearchSummary {
  matches: number
  files: number
  truncated: boolean
  engine: string
  elapsedMs: number
}

export interface SearchBatch {
  id: string
  matches: SearchMatch[]
}

export interface SearchDone {
  id: string
  summary: SearchSummary
  error: string
}

export function onSearchBatch(cb: (b: SearchBatch) => void): () => void {
  return EventsOn('search:batch', cb)
}

export function onSearchDone(cb: (d: SearchDone) => void): () => void {
  return EventsOn('search:done', cb)
}

export function newConnection(driver: Driver = 'postgres'): Connection {
  return db.Connection.createFrom({
    id: '',
    name: '',
    driver,
    host: 'localhost',
    port: 0,
    database: '',
    user: '',
    protocol: driver === 'clickhouse' ? 'native' : '',
    tls: false,
    readOnly: false,
  })
}

/** Wails rejects bound-method promises with a plain string; normalize to a message. */
export function errorMessage(err: unknown): string {
  if (typeof err === 'string') return err
  if (err instanceof Error) return err.message
  return String(err)
}
