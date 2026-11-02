package database

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"flint/internal/db"
	"flint/internal/feature"
)

const (
	testTimeout   = 15 * time.Second
	schemaTimeout = 60 * time.Second
)

// API manages connection profiles and runs queries (the Databases panel's and
// the SQL consoles' backend).
type API struct {
	host feature.Host
	// open reads connections.json and the query history on first use: building
	// the feature does no I/O (feature.Module.New).
	open   func() (*db.Manager, error)
	mu     sync.Mutex
	opened *db.Manager
}

func newAPI(h feature.Host) *API {
	a := &API{host: h}
	a.open = sync.OnceValues(func() (*db.Manager, error) {
		store, err := db.OpenStore(h.ConfigDir(), db.Keychain{})
		if err != nil {
			return nil, err
		}
		m := db.NewManager(store)
		m.SetHistory(db.OpenHistory(filepath.Join(h.ConfigDir(), "history"), db.DefaultHistoryLimit))
		a.mu.Lock()
		a.opened = m
		a.mu.Unlock()
		return m, nil
	})
	return a
}

// close ends every open session on quit (nothing when no query ran).
func (d *API) close() {
	d.mu.Lock()
	m := d.opened
	d.mu.Unlock()
	if m != nil {
		m.CloseAll()
	}
}

func (d *API) ctx() context.Context { return d.host.Context() }

// ListConnections returns all saved profiles (without passwords).
func (d *API) ListConnections() ([]db.Connection, error) {
	m, err := d.open()
	if err != nil {
		return nil, err
	}
	return m.Store().List(), nil
}

// SaveConnection creates or updates a profile. The password is only written when
// updatePassword is true, so editing other fields keeps the stored secret.
func (d *API) SaveConnection(c db.Connection, password string, updatePassword bool) (db.Connection, error) {
	m, err := d.open()
	if err != nil {
		return db.Connection{}, err
	}
	saved, err := m.Store().Save(c, passwordArg(password, updatePassword))
	if err != nil {
		return db.Connection{}, err
	}
	m.Invalidate(saved.ID)
	return saved, nil
}

// DeleteConnection removes a profile, its stored password and its query history.
func (d *API) DeleteConnection(id string) error {
	m, err := d.open()
	if err != nil {
		return err
	}
	m.Invalidate(id)
	if err := m.Store().Delete(id); err != nil {
		return err
	}
	return m.ClearHistory(id)
}

// QueryHistory lists the connection's executed statements, newest (or best match) first.
func (d *API) QueryHistory(connID string, q db.HistoryFilter) ([]db.HistoryEntry, error) {
	m, err := d.open()
	if err != nil {
		return nil, err
	}
	return m.QueryHistory(connID, q)
}

// ClearQueryHistory forgets every recorded statement of the connection.
func (d *API) ClearQueryHistory(connID string) error {
	m, err := d.open()
	if err != nil {
		return err
	}
	return m.ClearHistory(connID)
}

// Explain returns the plan of sql. analyze runs EXPLAIN ANALYZE (Postgres, in a rolled-back
// transaction) or EXPLAIN PIPELINE (ClickHouse). A Postgres statement that may write needs
// allowWrites; without it the result only asks for confirmation.
func (d *API) Explain(connID, database, queryID, sql string, analyze, allowWrites bool) (*db.ExplainResult, error) {
	m, err := d.open()
	if err != nil {
		return nil, err
	}
	return m.Explain(d.ctx(), connID, database, queryID, sql, analyze, allowWrites)
}

// EditTarget describes the table behind an editable result (columns, primary key).
func (d *API) EditTarget(connID, database string, tableOID uint32) (*db.EditTarget, error) {
	m, err := d.open()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(d.ctx(), testTimeout)
	defer cancel()
	return m.EditTarget(ctx, connID, database, tableOID)
}

// PreviewEdits returns the SQL a batch of staged row changes would run.
func (d *API) PreviewEdits(connID, database string, batch db.EditBatch) (string, error) {
	m, err := d.open()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(d.ctx(), testTimeout)
	defer cancel()
	return m.PreviewEdits(ctx, connID, database, batch)
}

// ApplyEdits applies staged row changes in one transaction.
func (d *API) ApplyEdits(connID, database string, batch db.EditBatch) (*db.EditResult, error) {
	m, err := d.open()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(d.ctx(), schemaTimeout)
	defer cancel()
	return m.ApplyEdits(ctx, connID, database, batch)
}

// TestConnection pings a (possibly unsaved) profile.
func (d *API) TestConnection(c db.Connection, password string, updatePassword bool) error {
	m, err := d.open()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(d.ctx(), testTimeout)
	defer cancel()
	return m.Test(ctx, c, passwordArg(password, updatePassword))
}

// RunQuery executes one statement on a database of the connection ("" = the profile's
// default) and returns at most limit rows. consoleID names the SQL console: its statements
// share one ClickHouse session, so SET and temporary tables carry over between them.
func (d *API) RunQuery(connID, database, queryID, consoleID, sql string, limit int) (*db.QueryResult, error) {
	m, err := d.open()
	if err != nil {
		return nil, err
	}
	return m.Query(db.WithConsole(d.ctx(), consoleID), connID, database, queryID, sql, limit)
}

// MigrationNotice returns, once, the message about connections Flint moved from
// ClickHouse's native protocol to HTTP ("" when there is none).
func (d *API) MigrationNotice() (string, error) {
	m, err := d.open()
	if err != nil {
		return "", err
	}
	return m.Store().TakeMigrationNotice(), nil
}

// CancelQuery aborts a running query by id.
func (d *API) CancelQuery(queryID string) {
	if m, err := d.open(); err == nil {
		m.Cancel(queryID)
	}
}

// Schema returns tables and columns of one database ("" = the profile's default)
// for autocomplete and the schema browser.
func (d *API) Schema(connID, database string, refresh bool) ([]db.Table, error) {
	m, err := d.open()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(d.ctx(), schemaTimeout)
	defer cancel()
	return m.Schema(ctx, connID, database, refresh)
}

// Databases lists the databases on the connection's server (the "Databases (N)" node).
func (d *API) Databases(connID string) ([]db.DatabaseInfo, error) {
	m, err := d.open()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(d.ctx(), testTimeout)
	defer cancel()
	return m.Databases(ctx, connID)
}

func passwordArg(password string, update bool) *string {
	if !update {
		return nil
	}
	return &password
}
