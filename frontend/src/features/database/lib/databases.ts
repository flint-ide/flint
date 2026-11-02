import type { Connection, DatabaseInfo, Table } from '../../../api/backend'

/** The parts of a connection that pick its default database. */
type ConnectionRef = { driver: string; database: string }

/**
 * Display name of a connection's default database until the server says otherwise: the
 * profile's, else "postgres" (what Go opens) or "default" (ClickHouse leaves it to the server).
 */
export function defaultDatabase(conn: ConnectionRef): string {
  if (conn.database) return conn.database
  return conn.driver === 'clickhouse' ? 'default' : 'postgres'
}

/**
 * The database argument sent to Go and used as cache key for a remembered choice
 * ("" = the profile's default). It's "" when the profile has no database: Go then opens
 * "postgres", or ClickHouse's server-side default.
 */
export function resolveDatabase(conn: Pick<Connection, 'database'>, database: string | undefined): string {
  return database || conn.database
}

/** Name of the default database: flagged by the server's list, else the display fallback. */
export function defaultDatabaseName(list: DatabaseInfo[] | undefined, conn: ConnectionRef): string {
  return list?.find((d) => d.default)?.name ?? defaultDatabase(conn)
}

/** Cache key of one database's schema; database names may contain anything but NUL. */
export function schemaKey(connId: string, database: string): string {
  return `${connId}\u0000${database}`
}

/** True when the schema cache key belongs to the connection (any of its databases). */
export function isSchemaKeyOf(key: string, connId: string): boolean {
  return key.startsWith(`${connId}\u0000`)
}

/**
 * Words that can't (or, like user and current_date, mustn't) stand bare as a column
 * name: Postgres' reserved key words plus the ClickHouse clauses that break a column
 * list. Quoting a few extra words is harmless; missing one silently reads current_user.
 */
const RESERVED = new Set(
  (
    'all analyse analyze and any array as asc asymmetric authorization between binary both case cast check collate collation column ' +
    'concurrently constraint create cross current_catalog current_date current_role current_schema current_time current_timestamp ' +
    'current_user default deferrable desc distinct do else end except false fetch final for foreign format freeze from full grant group ' +
    'having ilike in initially inner intersect into is isnull join lateral leading left like limit localtime localtimestamp natural not ' +
    'notnull null offset on only or order outer overlaps placing prewhere primary references returning right sample select session_user ' +
    'settings similar some symmetric system_user table tablesample then to trailing true union unique user using variadic verbose when where window with'
  ).split(' '),
)

/**
 * An identifier ready to paste into SQL: bare when it's plain lowercase, else quoted with the
 * dialect's quote (backtick for ClickHouse, double quote for Postgres), the quote doubled inside.
 */
export function quoteIdent(driver: string, name: string): string {
  if (/^[a-z_][a-z0-9_]*$/.test(name) && !RESERVED.has(name)) return name
  const q = driver === 'clickhouse' ? '`' : '"'
  return `${q}${name.replaceAll(q, q + q)}${q}`
}

/** "schema.table", each part quoted as needed (ClickHouse's schema is the database). */
export function qualifiedTable(driver: string, t: Pick<Table, 'schema' | 'name'>): string {
  return `${quoteIdent(driver, t.schema)}.${quoteIdent(driver, t.name)}`
}

/** The statement a table preview opens with. */
export function previewQuery(driver: string, t: Pick<Table, 'schema' | 'name'>): string {
  return `SELECT *\nFROM ${qualifiedTable(driver, t)}\nLIMIT 100;\n`
}

/** The statement that counts a table's rows. */
export function countQuery(driver: string, t: Pick<Table, 'schema' | 'name'>): string {
  return `SELECT count(*)\nFROM ${qualifiedTable(driver, t)};\n`
}

/** A password-less URL of the connection, e.g. postgresql://app@db:5432/shop, for psql or clickhouse-client. */
export function connectionUrl(conn: ConnectionRef & Pick<Connection, 'host' | 'port' | 'user'>): string {
  const scheme = conn.driver === 'clickhouse' ? 'clickhouse' : 'postgresql'
  const user = conn.user ? `${encodeURIComponent(conn.user)}@` : ''
  return `${scheme}://${user}${conn.host}:${conn.port}/${encodeURIComponent(defaultDatabase(conn))}`
}

/**
 * The databases offered by a console's picker: the server's list, plus the current choice
 * if the list hasn't loaded (or no longer has it) so the picker never shows a blank value.
 */
export function databaseChoices(list: DatabaseInfo[] | undefined, current: string): DatabaseInfo[] {
  const known = list ?? []
  if (!current || known.some((d) => d.name === current)) return known
  return [{ name: current, canConnect: true, default: false }, ...known]
}
