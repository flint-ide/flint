package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The pool defaults to the simple protocol (no prepared-statement cache for ad-hoc
// queries); EXPLAIN and edits pass QueryExecModeExec instead: the extended protocol
// sends values as real bind parameters and refuses several statements in one string.

// Explain runs EXPLAIN (FORMAT JSON) of sql. With analyze the statement really executes,
// inside a transaction that is always rolled back.
func (s *pgSession) Explain(ctx context.Context, sql string, analyze bool) (*ExplainResult, error) {
	target, err := explainTarget(sql)
	if err != nil {
		return nil, err
	}
	stmt := postgresExplainSQL(target, analyze)
	start := time.Now()

	var raw string
	rolledBack := false
	if analyze {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		err = tx.QueryRow(ctx, stmt, pgx.QueryExecModeExec).Scan(&raw)
		// Rolled back on a fresh context: a cancelled ctx must not leave the work committed
		// (it can't anyway, but the connection should go back to the pool clean).
		rbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		rbErr := tx.Rollback(rbCtx)
		cancel()
		if err != nil {
			return nil, err
		}
		if rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			return nil, fmt.Errorf("roll back EXPLAIN ANALYZE: %w", rbErr)
		}
		rolledBack = true
	} else if err := s.pool.QueryRow(ctx, stmt, pgx.QueryExecModeExec).Scan(&raw); err != nil {
		return nil, err
	}

	res, err := ParsePostgresPlan([]byte(raw))
	if err != nil {
		return nil, err
	}
	res.Statement = stmt
	res.RolledBack = rolledBack
	res.ElapsedMs = time.Since(start).Milliseconds()
	return res, nil
}

// pgQuerier is what catalog lookups need: a pool or a transaction.
type pgQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

const pgEditTableSQL = `
SELECT n.nspname, c.relname, c.relkind::text
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.oid = $1::int8::oid`

// pgEditColumnsSQL lists a table's live columns with their 1-based primary-key position
// (0 = not in it). indkey is an int2vector, whose subscripts start at 0: hence ORDINALITY.
const pgEditColumnsSQL = `
SELECT a.attnum::int4, a.attname::text, format_type(a.atttypid, NULL), t.typname::text, t.typcategory::text,
       a.attnotnull, a.atthasdef, a.attgenerated <> '', a.attidentity = 'a',
       coalesce((SELECT k.ord FROM unnest(i.indkey::int2[]) WITH ORDINALITY AS k(attnum, ord)
                 WHERE k.attnum = a.attnum), 0)::int4
FROM pg_attribute a
JOIN pg_type t ON t.oid = a.atttypid
LEFT JOIN pg_index i ON i.indrelid = a.attrelid AND i.indisprimary
WHERE a.attrelid = $1::int8::oid AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY a.attnum`

// loadEditTarget reads a table's name, columns and primary key from the catalog.
func loadEditTarget(ctx context.Context, q pgQuerier, oid uint32) (*EditTarget, error) {
	t := &EditTarget{TableOID: oid, Key: []string{}, Columns: []EditColumn{}}
	rows, err := q.Query(ctx, pgEditTableSQL, pgx.QueryExecModeExec, int64(oid))
	if err != nil {
		return nil, fmt.Errorf("look up table: %w", err)
	}
	var kind string
	found := false
	for rows.Next() {
		found = true
		if err := rows.Scan(&t.Schema, &t.Table, &kind); err != nil {
			rows.Close()
			return nil, fmt.Errorf("look up table: %w", err)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("look up table: %w", err)
	}
	if !found {
		return nil, errors.New("the source table no longer exists")
	}
	if kind != "r" && kind != "p" {
		return nil, errors.New("only rows of plain tables can be edited")
	}

	rows, err = q.Query(ctx, pgEditColumnsSQL, pgx.QueryExecModeExec, int64(oid))
	if err != nil {
		return nil, fmt.Errorf("read table columns: %w", err)
	}
	defer rows.Close()
	keyPos := map[int32]string{}
	for rows.Next() {
		var (
			c                   EditColumn
			attnum, pkPos       int32
			typname, category   string
			generated, alwaysID bool
		)
		if err := rows.Scan(&attnum, &c.Name, &c.Type, &typname, &category, &c.NotNull, &c.HasDefault, &generated, &alwaysID, &pkPos); err != nil {
			return nil, fmt.Errorf("read table columns: %w", err)
		}
		c.Attnum = int(attnum)
		c.PrimaryKey = pkPos > 0
		c.Kind, c.ReadOnlyReason = editColumnKind(typname, category)
		switch {
		case generated:
			c.ReadOnlyReason = "generated column"
		case alwaysID:
			c.ReadOnlyReason = "identity column (GENERATED ALWAYS)"
		}
		c.Editable = c.ReadOnlyReason == ""
		if c.Kind == "" {
			c.Kind = KindText
		}
		if pkPos > 0 {
			keyPos[pkPos] = c.Name
		}
		t.Columns = append(t.Columns, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read table columns: %w", err)
	}
	for i := int32(1); i <= int32(len(keyPos)); i++ {
		t.Key = append(t.Key, keyPos[i])
	}
	return t, nil
}

func (s *pgSession) EditTarget(ctx context.Context, oid uint32) (*EditTarget, error) {
	return loadEditTarget(ctx, s.pool, oid)
}

func (s *pgSession) PreviewEdits(ctx context.Context, batch EditBatch) (string, error) {
	target, err := loadEditTarget(ctx, s.pool, batch.TableOID)
	if err != nil {
		return "", err
	}
	stmts, err := BuildEditStatements(*target, batch.Edits)
	if err != nil {
		return "", err
	}
	return PreviewSQL(stmts), nil
}

// ApplyEdits runs the batch in one transaction. The table is re-read inside it, and an
// UPDATE or DELETE that doesn't hit exactly one row (changed or deleted meanwhile) rolls
// everything back.
func (s *pgSession) ApplyEdits(ctx context.Context, batch EditBatch) (*EditResult, error) {
	start := time.Now()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }() // no-op after Commit

	target, err := loadEditTarget(ctx, tx, batch.TableOID)
	if err != nil {
		return nil, err
	}
	stmts, err := BuildEditStatements(*target, batch.Edits)
	if err != nil {
		return nil, err
	}
	res := &EditResult{Statements: len(stmts), SQL: PreviewSQL(stmts)}
	for i, st := range stmts {
		args := make([]any, 0, len(st.Args)+1)
		args = append(args, pgx.QueryExecModeExec)
		for _, a := range st.Args {
			if a == nil {
				args = append(args, nil)
			} else {
				args = append(args, *a)
			}
		}
		tag, err := tx.Exec(ctx, st.SQL, args...)
		if err != nil {
			return res, fmt.Errorf("statement %d (%s): %w; nothing was applied", i+1, st.Preview, err)
		}
		if st.ExpectOne && tag.RowsAffected() != 1 {
			return res, fmt.Errorf("statement %d affected %d rows instead of 1 (the row changed or is gone?); nothing was applied: %s",
				i+1, tag.RowsAffected(), st.Preview)
		}
		res.Affected += tag.RowsAffected()
	}
	if err := tx.Commit(ctx); err != nil {
		return res, err
	}
	res.ElapsedMs = time.Since(start).Milliseconds()
	return res, nil
}
