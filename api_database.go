package main

import (
	"context"
	"time"

	"flint/internal/db"
)

const (
	testTimeout   = 15 * time.Second
	schemaTimeout = 60 * time.Second
)

// DatabaseAPI manages connection profiles and runs queries.
type DatabaseAPI struct {
	app *App
}

// ListConnections returns all saved profiles (without passwords).
func (d *DatabaseAPI) ListConnections() []db.Connection {
	return d.app.db.Store().List()
}

// SaveConnection creates or updates a profile. The password is only written when
// updatePassword is true, so editing other fields keeps the stored secret.
func (d *DatabaseAPI) SaveConnection(c db.Connection, password string, updatePassword bool) (db.Connection, error) {
	saved, err := d.app.db.Store().Save(c, passwordArg(password, updatePassword))
	if err != nil {
		return db.Connection{}, err
	}
	d.app.db.Invalidate(saved.ID)
	return saved, nil
}

// DeleteConnection removes a profile, its stored password and its query history.
func (d *DatabaseAPI) DeleteConnection(id string) error {
	d.app.db.Invalidate(id)
	if err := d.app.db.Store().Delete(id); err != nil {
		return err
	}
	return d.app.db.ClearHistory(id)
}

// QueryHistory lists the connection's executed statements, newest (or best match) first.
func (d *DatabaseAPI) QueryHistory(connID string, q db.HistoryFilter) ([]db.HistoryEntry, error) {
	return d.app.db.QueryHistory(connID, q)
}

// ClearQueryHistory forgets every recorded statement of the connection.
func (d *DatabaseAPI) ClearQueryHistory(connID string) error {
	return d.app.db.ClearHistory(connID)
}

// Explain returns the plan of sql. analyze runs EXPLAIN ANALYZE (Postgres, in a rolled-back
// transaction) or EXPLAIN PIPELINE (ClickHouse). A Postgres statement that may write needs
// allowWrites; without it the result only asks for confirmation.
func (d *DatabaseAPI) Explain(connID, database, queryID, sql string, analyze, allowWrites bool) (*db.ExplainResult, error) {
	return d.app.db.Explain(d.app.ctx, connID, database, queryID, sql, analyze, allowWrites)
}

// EditTarget describes the table behind an editable result (columns, primary key).
func (d *DatabaseAPI) EditTarget(connID, database string, tableOID uint32) (*db.EditTarget, error) {
	ctx, cancel := context.WithTimeout(d.app.ctx, testTimeout)
	defer cancel()
	return d.app.db.EditTarget(ctx, connID, database, tableOID)
}

// PreviewEdits returns the SQL a batch of staged row changes would run.
func (d *DatabaseAPI) PreviewEdits(connID, database string, batch db.EditBatch) (string, error) {
	ctx, cancel := context.WithTimeout(d.app.ctx, testTimeout)
	defer cancel()
	return d.app.db.PreviewEdits(ctx, connID, database, batch)
}

// ApplyEdits applies staged row changes in one transaction.
func (d *DatabaseAPI) ApplyEdits(connID, database string, batch db.EditBatch) (*db.EditResult, error) {
	ctx, cancel := context.WithTimeout(d.app.ctx, schemaTimeout)
	defer cancel()
	return d.app.db.ApplyEdits(ctx, connID, database, batch)
}

// TestConnection pings a (possibly unsaved) profile.
func (d *DatabaseAPI) TestConnection(c db.Connection, password string, updatePassword bool) error {
	ctx, cancel := context.WithTimeout(d.app.ctx, testTimeout)
	defer cancel()
	return d.app.db.Test(ctx, c, passwordArg(password, updatePassword))
}

// RunQuery executes one statement on a database of the connection ("" = the profile's
// default) and returns at most limit rows.
func (d *DatabaseAPI) RunQuery(connID, database, queryID, sql string, limit int) (*db.QueryResult, error) {
	return d.app.db.Query(d.app.ctx, connID, database, queryID, sql, limit)
}

// CancelQuery aborts a running query by id.
func (d *DatabaseAPI) CancelQuery(queryID string) {
	d.app.db.Cancel(queryID)
}

// Schema returns tables and columns of one database ("" = the profile's default)
// for autocomplete and the schema browser.
func (d *DatabaseAPI) Schema(connID, database string, refresh bool) ([]db.Table, error) {
	ctx, cancel := context.WithTimeout(d.app.ctx, schemaTimeout)
	defer cancel()
	return d.app.db.Schema(ctx, connID, database, refresh)
}

// Databases lists the databases on the connection's server (the "Databases (N)" node).
func (d *DatabaseAPI) Databases(connID string) ([]db.DatabaseInfo, error) {
	ctx, cancel := context.WithTimeout(d.app.ctx, testTimeout)
	defer cancel()
	return d.app.db.Databases(ctx, connID)
}

func passwordArg(password string, update bool) *string {
	if !update {
		return nil
	}
	return &password
}
