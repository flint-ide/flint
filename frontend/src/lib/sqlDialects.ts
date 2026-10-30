import { PostgreSQL, SQLDialect, sql, type SQLNamespace } from '@codemirror/lang-sql'
import type { Extension } from '@codemirror/state'
import type { Driver, Table } from '../api/backend'

const clickhouseKeywords = [
  'select from where and or not in is null like ilike between as on join inner left right full outer cross',
  'array any all asof semi anti using group by order having limit offset with totals rollup cube union',
  'except intersect distinct case when then else end insert into values create table view materialized',
  'database dictionary if exists drop alter add modify column rename truncate optimize final settings',
  'format engine partition primary key sample prewhere global local interval desc asc nulls first last',
  'show tables databases describe explain system kill query mutation delete update ttl codec default',
  'alias materialized ephemeral comment to attach detach replace temporary live window over rows range',
  'unbounded preceding following current row qualify grouping sets',
].join(' ')

const clickhouseTypes = [
  'uint8 uint16 uint32 uint64 uint128 uint256 int8 int16 int32 int64 int128 int256 float32 float64',
  'decimal decimal32 decimal64 decimal128 decimal256 bool boolean string fixedstring uuid date date32',
  'datetime datetime64 enum enum8 enum16 lowcardinality array tuple map nullable ipv4 ipv6 json object',
  'nested aggregatefunction simpleaggregatefunction nothing',
].join(' ')

const clickhouseBuiltins = [
  'count sum avg min max uniq uniqexact uniqcombined quantile quantiles quantileexact argmin argmax',
  'grouparray groupuniqarray topk any anylast if multiif coalesce ifnull nullif tostring touint64 toint64',
  'tofloat64 todate todatetime todatetime64 tostartofday tostartofhour tostartofminute tostartofweek',
  'tostartofmonth tostartofinterval now today yesterday formatdatetime parsedatetimebesteffort',
  'arrayjoin arraymap arrayfilter arraycount length has hasany hasall empty notempty splitbychar',
  'concat substring lower upper trim replaceall match extract jsonextract jsonextractstring',
  'jsonextractint visitparamextractstring cityhash64 siphash64 rand round floor ceil abs',
].join(' ')

export const ClickHouseSQL = SQLDialect.define({
  keywords: clickhouseKeywords,
  types: clickhouseTypes,
  builtin: clickhouseBuiltins,
  backslashEscapes: true,
  doubleQuotedStrings: false,
  hashComments: true,
  identifierQuotes: '`"',
  caseInsensitiveIdentifiers: false,
})

/** Builds the autocomplete namespace: `schema.table` and bare `table` → column names. */
export function schemaNamespace(tables: Table[]): SQLNamespace {
  const ns: Record<string, Record<string, readonly string[]> | readonly string[]> = {}
  for (const t of tables) {
    const cols = t.columns.map((c) => c.name)
    const bySchema = (ns[t.schema] as Record<string, readonly string[]> | undefined) ?? {}
    bySchema[t.name] = cols
    ns[t.schema] = bySchema
  }
  return ns as SQLNamespace
}

/** Default schema used to resolve unqualified table names in completions. */
export function defaultSchema(driver: Driver, database: string): string | undefined {
  if (driver === 'postgres') return 'public'
  return database || 'default'
}

export function sqlLanguage(driver: Driver | undefined, database: string, tables: Table[]): Extension {
  const dialect = driver === 'clickhouse' ? ClickHouseSQL : PostgreSQL
  return sql({
    dialect,
    upperCaseKeywords: false,
    schema: schemaNamespace(tables),
    defaultSchema: driver ? defaultSchema(driver, database) : undefined,
  })
}
