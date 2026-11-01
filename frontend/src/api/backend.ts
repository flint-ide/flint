// Single entry point to the Go backend: re-exports generated bindings under readable names
// and adds types for event payloads the generator can't see.
import * as Workspace from '../../bindings/flint/workspaceapi'
import * as Search from '../../bindings/flint/searchapi'
import * as Database from '../../bindings/flint/databaseapi'
import * as Scratch from '../../bindings/flint/scratchapi'
import * as Diff from '../../bindings/flint/diffapi'
import * as GitHub from '../../bindings/flint/githubapi'
import { EventsOn } from '../wails/runtime'
import { db, github, scratch, search, textdiff, workspace } from '../wails/models'

export { Workspace, Search, Database, Scratch, Diff, GitHub }

export type Entry = workspace.Entry
export type FileContent = workspace.FileContent
export type Connection = db.Connection
export type QueryResult = db.QueryResult
export type Table = db.Table
export type DatabaseInfo = db.DatabaseInfo
export type HistoryEntry = db.HistoryEntry
export type ExplainResult = db.ExplainResult
export type PlanNode = db.PlanNode
export type EditTarget = db.EditTarget
export type EditResult = db.EditResult
export type ScratchPad = scratch.Scratch
export type Hunk = textdiff.Hunk
export type LineMark = textdiff.LineMark
export type SearchOptions = search.Options
export type GitHubStatus = github.Status
export type PullRequest = github.PullRequest
export type PullRequestDetails = github.Details
export type ReviewThread = github.Thread
export type ReviewComment = github.Comment
export type PullRequestFile = github.File
export type PullRequestFileDiff = github.FileDiff
export type PullRequestCheck = github.Check
export type Driver = 'postgres' | 'clickhouse'
export type ScratchKind = 'sql' | 'markdown' | 'text'

// Event payloads aren't part of any bound method signature, so Wails doesn't generate them.
export interface SearchMatch {
  /** Workspace folder the match was found in; path is relative to it. */
  root: string
  path: string
  line: number
  text: string
  ranges: { start: number; end: number }[]
  /** When replacing: what each range becomes. */
  replacements?: string[]
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
    tls: false,
    readOnly: false,
  })
}

/** A batch of staged row edits in the shape of Go's db.EditBatch. */
export function editBatch(batch: { tableOid: number; edits: { op: string; key: Record<string, string | null>; values: Record<string, string | null> }[] }): db.EditBatch {
  return db.EditBatch.createFrom(batch)
}

/** Filter for DatabaseAPI.QueryHistory ("" database = the profile's default). */
export function historyFilter(f: { database: string; allDatabases: boolean; filter: string; limit: number }): db.HistoryFilter {
  return db.HistoryFilter.createFrom(f)
}

/** Pull request list filter in the shape of Go's github.ListFilter. */
export function pullRequestFilter(f: { who: string; state: string }): github.ListFilter {
  return github.ListFilter.createFrom(f)
}

/** A new review comment in the shape of Go's github.NewComment. */
export function newReviewComment(c: { path: string; line: number; side: 'LEFT' | 'RIGHT'; startLine: number; body: string; review: boolean }): github.NewComment {
  return github.NewComment.createFrom(c)
}

/** Wails rejects bound-method promises with a plain string; normalize to a message. */
export function errorMessage(err: unknown): string {
  if (typeof err === 'string') return err
  if (err instanceof Error) return err.message
  return String(err)
}
