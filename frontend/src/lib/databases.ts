import type { Connection, DatabaseInfo } from '../api/backend'

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
 * The databases offered by a console's picker: the server's list, plus the current choice
 * if the list hasn't loaded (or no longer has it) so the picker never shows a blank value.
 */
export function databaseChoices(list: DatabaseInfo[] | undefined, current: string): DatabaseInfo[] {
  const known = list ?? []
  if (!current || known.some((d) => d.name === current)) return known
  return [{ name: current, canConnect: true, default: false }, ...known]
}
