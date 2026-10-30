package db

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// explainer is implemented by sessions that can explain a statement. For ClickHouse
// "analyze" selects EXPLAIN PIPELINE (nothing executes); for Postgres it runs ANALYZE.
type explainer interface {
	Explain(ctx context.Context, sql string, analyze bool) (*ExplainResult, error)
}

// rowEditor is implemented by sessions whose result rows can be edited (Postgres).
type rowEditor interface {
	EditTarget(ctx context.Context, oid uint32) (*EditTarget, error)
	PreviewEdits(ctx context.Context, batch EditBatch) (string, error)
	ApplyEdits(ctx context.Context, batch EditBatch) (*EditResult, error)
}

// ErrEditUnsupported is returned when the driver has no row editing.
var ErrEditUnsupported = errors.New("editing rows is supported for PostgreSQL only")

// analyzeConfirmReason is shown before EXPLAIN ANALYZE runs a statement that may write.
const analyzeConfirmReason = "EXPLAIN ANALYZE executes the statement. Flint runs it in a transaction that is rolled back, " +
	"but effects outside the transaction (sequence values, notifications, external calls) remain and locks are taken."

// Explain explains sql on one database of the connection. For Postgres, analyze executes
// the statement in a rolled-back transaction; when it may modify data and allowWrites is
// false nothing runs and the result asks for confirmation (NeedsConfirm).
func (m *Manager) Explain(ctx context.Context, connID, database, queryID, sql string, analyze, allowWrites bool) (*ExplainResult, error) {
	conn, ok := m.store.Get(connID)
	if !ok {
		return nil, fmt.Errorf("unknown connection %q", connID)
	}
	if conn.Driver == Postgres && analyze && analyzeModifies(sql) {
		if conn.ReadOnly {
			return nil, ErrReadOnly
		}
		if !allowWrites {
			return &ExplainResult{Driver: Postgres, Analyze: true, NeedsConfirm: true, ConfirmReason: analyzeConfirmReason}, nil
		}
	}
	start := time.Now()
	res, err := m.explain(ctx, conn, database, queryID, sql, analyze)
	kind := HistoryExplain
	if analyze {
		kind = HistoryExplainAnalyze
	}
	m.record(conn, database, kind, sql, start, 0, err)
	return res, err
}

func (m *Manager) explain(ctx context.Context, conn Connection, database, queryID, sql string, analyze bool) (*ExplainResult, error) {
	sess, _, release, err := m.acquire(ctx, conn, database)
	if err != nil {
		return nil, err
	}
	defer release()
	ex, ok := sess.(explainer)
	if !ok {
		return nil, fmt.Errorf("EXPLAIN isn't supported for %s", conn.Driver)
	}
	ctx, done := m.track(ctx, queryID)
	defer done()
	res, err := ex.Explain(ctx, sql, analyze)
	if err != nil {
		return nil, cancelled(ctx, err)
	}
	return res, nil
}

// EditTarget describes the table (columns, primary key) behind an editable result.
func (m *Manager) EditTarget(ctx context.Context, connID, database string, tableOID uint32) (*EditTarget, error) {
	var target *EditTarget
	err := m.withEditor(ctx, connID, database, func(ed rowEditor) (err error) {
		target, err = ed.EditTarget(ctx, tableOID)
		return err
	})
	return target, err
}

// PreviewEdits renders the statements a batch would run, values inlined for reading.
func (m *Manager) PreviewEdits(ctx context.Context, connID, database string, batch EditBatch) (string, error) {
	var sql string
	err := m.withEditor(ctx, connID, database, func(ed rowEditor) (err error) {
		sql, err = ed.PreviewEdits(ctx, batch)
		return err
	})
	return sql, err
}

// ApplyEdits applies a batch of row changes in one transaction and records it in the history.
func (m *Manager) ApplyEdits(ctx context.Context, connID, database string, batch EditBatch) (*EditResult, error) {
	conn, ok := m.store.Get(connID)
	if !ok {
		return nil, fmt.Errorf("unknown connection %q", connID)
	}
	if conn.ReadOnly {
		return nil, ErrReadOnly
	}
	start := time.Now()
	var res *EditResult
	err := m.withEditor(ctx, connID, database, func(ed rowEditor) (err error) {
		res, err = ed.ApplyEdits(ctx, batch)
		return err
	})
	sql := fmt.Sprintf("-- %d row change(s) to table OID %d", len(batch.Edits), batch.TableOID)
	var affected int64
	if res != nil {
		sql = res.SQL
		if err == nil {
			affected = res.Affected // on failure everything was rolled back
		}
	}
	m.record(conn, database, HistoryEdit, sql, start, affected, err)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (m *Manager) withEditor(ctx context.Context, connID, database string, fn func(rowEditor) error) error {
	conn, ok := m.store.Get(connID)
	if !ok {
		return fmt.Errorf("unknown connection %q", connID)
	}
	sess, _, release, err := m.acquire(ctx, conn, database)
	if err != nil {
		return err
	}
	defer release()
	ed, ok := sess.(rowEditor)
	if !ok {
		return ErrEditUnsupported
	}
	return fn(ed)
}

// record adds a run to the query history; failures to write it never fail the run.
func (m *Manager) record(conn Connection, database, kind, sql string, start time.Time, rows int64, runErr error) {
	if m.history == nil {
		return
	}
	e := HistoryEntry{
		ID:         start.UnixMicro(),
		Database:   keyFor(conn, database).database,
		SQL:        sql,
		At:         start.UnixMilli(),
		DurationMs: time.Since(start).Milliseconds(),
		Rows:       rows,
		Kind:       kind,
	}
	if runErr != nil {
		e.Error = runErr.Error()
	}
	_, _ = m.history.Record(conn.ID, e)
}

// QueryHistory lists a connection's recorded statements.
func (m *Manager) QueryHistory(connID string, q HistoryFilter) ([]HistoryEntry, error) {
	if m.history == nil {
		return []HistoryEntry{}, nil
	}
	// Entries store resolved names, so "" (the profile's default) is resolved the same way.
	if !q.AllDatabases {
		if conn, ok := m.store.Get(connID); ok {
			q.Database = keyFor(conn, q.Database).database
		}
	}
	return m.history.List(connID, q)
}

// ClearHistory forgets a connection's recorded statements.
func (m *Manager) ClearHistory(connID string) error {
	if m.history == nil {
		return nil
	}
	return m.history.Clear(connID)
}
