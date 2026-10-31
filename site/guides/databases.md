# Databases

Flint connects to PostgreSQL and ClickHouse. You can browse schemas, run queries in persistent SQL consoles, read visual query plans, look up past statements, and edit Postgres rows by primary key.

## Connections

Open the Databases view (`⛁` on the activity bar) and add a connection, or run "New Database Connection…" from the command palette.

| Field | Meaning |
|---|---|
| Driver | PostgreSQL or ClickHouse |
| Protocol | ClickHouse only: Native TCP (port 9000, or 9440 with TLS) or HTTP (port 8123, or 8443 with TLS) |
| Name | A label for the connection |
| Host, Port | The server; the port defaults to the driver's usual port |
| Database | Optional default database. Without it, Flint uses `postgres` (ClickHouse: `default`) |
| User, Password | The password is stored in the macOS Keychain, never in a file |
| TLS | Encrypts the connection and verifies the server certificate and host name |
| Don't verify certificate (insecure) | Keeps encryption but skips verification |
| Read-only | Flint refuses statements that modify data |

**Test** checks the connection before you save it.

> **Warning:** "Don't verify certificate (insecure)" means anyone between you and the server can read the traffic. Use it only for self-signed certificates or private certificate authorities.

## Browsing

Under each connection, a **Databases (N)** node lists every database on the server, as in pgAdmin. Postgres leaves out templates and marks databases you can't connect to with a red cross. ClickHouse lists what `SHOW DATABASES` returns.

Expanding a database opens a separate connection to it (same host, user and password) and shows its schemas and tables. Flint closes connections to non-default databases after a minute of inactivity, and right before a statement that modifies the server (such as `DROP DATABASE`), so its own idle connections don't block it.

## SQL consoles

A SQL console is a scratchpad: it saves automatically and is still there after a restart. Open one with `⌘N`, from the `+` menu in the tab bar, or from the Scratchpads view (`✎`).

- Pick the connection and the database at the top. The console remembers your choice.
- `⌘↵` runs the statement under the cursor, or the selection.
- Autocompletion uses the selected database's schema.
- Results appear in a table that handles up to 200,000 rows, with export to TSV or JSON.

## Query history

Every statement you run is recorded per connection and database, including failed ones, EXPLAINs and edits made from the results table, with its start time, duration, row count and error.

Open it with **History** in the console or `⇧⌘H`. The list is newest first and has a fuzzy filter and an **All databases** toggle.

| Key | Action |
|---|---|
| `↵` | Insert the statement into the editor |
| `⌘↵` | Run it again |
| `⌘C` | Copy it |

Flint keeps the last 5000 statements per connection. Passwords in `CREATE/ALTER ROLE … PASSWORD '…'` and ClickHouse `IDENTIFIED … BY '…'` are masked; everything else is stored as written. Use "Clear History…" to remove the history of a connection.

## EXPLAIN

Run **Explain** (`⇧⌘E`) or **Analyze** (`⇧⌘A`) from the console toolbar or the palette.

### PostgreSQL

The plan is shown as an expandable tree. Each node shows its type, relation, index, cost, estimated and actual rows, loops, time, buffers and filter, with a bar for its share of the total (self time with Analyze, self cost otherwise). Click a node for details such as conditions, sort keys and buffers.

Flint warns you about:

- a sequential scan reading at least 10,000 rows (without Analyze: at least 10,000 estimated rows or a cost of at least 2500);
- a row estimate that is off by 10x or more;
- a sort or hash that spills to disk.

Analyze always runs the statement inside a transaction that is rolled back afterwards. For anything other than a plain `SELECT` (for example `DELETE`, `WITH … UPDATE` or `SELECT … INTO`), Flint asks you to confirm first, and on a read-only connection it refuses. EXPLAIN accepts one statement at a time.

### ClickHouse

Explain runs `EXPLAIN PLAN indexes = 1`, and Analyze runs `EXPLAIN PIPELINE`. Neither executes the query. The tree is built from the indentation of ClickHouse's output, on a best-effort basis.

## Editing results (PostgreSQL)

When every table-backed column of a result comes from a single table that has a primary key, and all key columns are in the result, the cells become editable.

1. Double-click a cell, or press `↵` or `F2`. The editor matches the column type: text, numbers with validation, booleans as a list, JSON in a multi-line field with validation, and a NULL toggle.
2. **+ Row** adds a row (columns you leave empty get their DEFAULT). **− Row** or `⌘⌫` marks a row for deletion.
3. Your changes stay local and highlighted. **Revert** discards them.
4. **Submit…** shows the generated SQL. **Apply** runs the `UPDATE`, `INSERT` and `DELETE` statements by primary key in a single transaction.

If an `UPDATE` or `DELETE` doesn't hit exactly one row, everything is rolled back. Values are always sent as parameters.

These stay read-only: computed and `GENERATED ALWAYS AS IDENTITY` columns, arrays, `bytea`, join results, views without a key, every result on a read-only connection, and all ClickHouse results.

## Where data is kept

Connection profiles are stored without passwords; passwords are in the Keychain and are deleted together with the connection. See [Files and data](../reference/files-and-data.md).

## Related

- [Explore a database](../use-cases/explore-a-database.md)
