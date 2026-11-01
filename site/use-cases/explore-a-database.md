# Explore a database

You need to understand a PostgreSQL database: what is in it, how a slow query behaves, and maybe fix a few rows. This walkthrough adds a connection safely, browses the schema, runs and explains queries, and edits data with a review step. ClickHouse works the same way for browsing, querying and EXPLAIN. Use its HTTP port (8123, or 8443 with TLS) when you add the connection.

## 1. Add a connection

1. Open the Databases view (`⛁` on the activity bar), or run "New Database Connection…" from the palette.
2. Choose **PostgreSQL**, give the connection a name, and enter the host, port, user and password.
3. Leave **Database** empty to connect to `postgres` by default, or enter the database you use most.
4. Tick **TLS** for anything that isn't on your machine. Flint then verifies the server certificate and host name.
5. For a production database, tick **Read-only**. Flint refuses statements that modify data on this connection.
6. Click **Test**, then **Save**.

The password goes into the macOS Keychain, not into a file.

> **Warning:** If the server uses a self-signed certificate or a private CA, the TLS check fails. **Don't verify certificate (insecure)** keeps encryption but lets anyone in between read the traffic. Use it only when you trust the network path.

## 2. Browse the schema

1. Expand the connection. **Databases (N)** lists every database on the server; ones you can't connect to are marked with a red cross.
2. Expand a database to see its schemas and tables.

Flint opens a separate connection for each database you expand and closes it after a minute of inactivity.

## 3. Open a SQL console

1. Press `⌘N`, or click `+` in the tab bar and choose **SQL console**.
2. Pick the connection and the database at the top of the console.

Consoles are scratchpads: they save automatically and come back after a restart, with the connection and database you picked. Find them later in the Scratchpads view (`✎`).

## 4. Run queries

1. Write a query. Autocompletion suggests tables and columns from the selected database.
2. Put the cursor in the statement (or select it) and press `⌘↵`.

```sql
SELECT o.id, o.status, o.created_at, c.email
FROM orders o
JOIN customers c ON c.id = o.customer_id
WHERE o.created_at > now() - interval '7 days'
ORDER BY o.created_at DESC;
```

Results appear in a table below, up to 200,000 rows. Export them to TSV or JSON when you need them elsewhere.

## 5. Understand a slow query

1. With the cursor in the query, press `⇧⌘E` (Explain). Flint shows the estimated plan as a tree, without running the query.
2. Press `⇧⌘A` (Analyze) to run it and see actual rows, time and buffers.

Look at:

- the bar on each node, which shows its share of the total time (or cost);
- warnings for sequential scans over many rows, row estimates off by 10x or more, and sorts or hashes spilling to disk;
- node details (click a node) for conditions, sort keys and buffers.

Analyze runs inside a transaction that is always rolled back. For statements other than `SELECT`, Flint asks before running them, and refuses on a read-only connection.

> **Tip:** Paste the plan's warnings into the agent chat and ask for an index or a rewrite.

## 6. Find a query you ran before

1. Press `⇧⌘H`, or click **History** in the console.
2. Type to filter. Turn on **All databases** to search across every database of the connection.
3. Press `↵` to insert the statement, `⌘↵` to run it again, or `⌘C` to copy it.

## 7. Edit rows

When a result comes from a single table with a primary key, and the key columns are in the result, you can edit it.

1. Query the rows you want to change, including the primary key:

   ```sql
   SELECT id, status, note FROM orders WHERE id IN (1041, 1042);
   ```

2. Double-click a cell (or press `↵` or `F2`) and change the value. The editor matches the column type and has a NULL toggle.
3. Use **+ Row** to add a row and **− Row** (or `⌘⌫`) to mark one for deletion.
4. Changed cells stay highlighted. Click **Revert** to throw the changes away.
5. Click **Submit…** to see the exact SQL Flint will run.
6. Click **Apply**. All changes run in one transaction, by primary key.

If any `UPDATE` or `DELETE` would affect anything other than exactly one row, the whole transaction is rolled back.

On a read-only connection, results can't be edited. ClickHouse results are always read-only.

## Tips

- **Separate connections for separate risks.** Keep a read-only connection for production and a writable one for local or staging.
- **History is private and local.** It is stored per connection on your Mac, with passwords in role statements masked. "Clear History…" removes it.
- **Notes next to queries.** "New Markdown Note" in the palette creates a note scratchpad for findings.
