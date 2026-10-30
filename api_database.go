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

// DeleteConnection removes a profile and its stored password.
func (d *DatabaseAPI) DeleteConnection(id string) error {
	d.app.db.Invalidate(id)
	return d.app.db.Store().Delete(id)
}

// TestConnection pings a (possibly unsaved) profile.
func (d *DatabaseAPI) TestConnection(c db.Connection, password string, updatePassword bool) error {
	ctx, cancel := context.WithTimeout(d.app.ctx, testTimeout)
	defer cancel()
	return d.app.db.Test(ctx, c, passwordArg(password, updatePassword))
}

// RunQuery executes one statement and returns at most limit rows.
func (d *DatabaseAPI) RunQuery(connID, queryID, sql string, limit int) (*db.QueryResult, error) {
	return d.app.db.Query(d.app.ctx, connID, queryID, sql, limit)
}

// CancelQuery aborts a running query by id.
func (d *DatabaseAPI) CancelQuery(queryID string) {
	d.app.db.Cancel(queryID)
}

// Schema returns tables and columns for autocomplete and the schema browser.
func (d *DatabaseAPI) Schema(connID string, refresh bool) ([]db.Table, error) {
	ctx, cancel := context.WithTimeout(d.app.ctx, schemaTimeout)
	defer cancel()
	return d.app.db.Schema(ctx, connID, refresh)
}

func passwordArg(password string, update bool) *string {
	if !update {
		return nil
	}
	return &password
}
