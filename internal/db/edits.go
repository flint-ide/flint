package db

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Value kinds of editable columns; the grid picks its cell editor by them.
const (
	KindText   = "text"
	KindNumber = "number"
	KindBool   = "bool"
	KindJSON   = "json"
)

// EditColumn is one column of an editable table.
type EditColumn struct {
	Attnum int    `json:"attnum"`
	Name   string `json:"name"`
	// Type is the column type without modifiers (e.g. "character varying"); values are
	// cast to it from text, so typmod checks happen on assignment like for a literal.
	Type       string `json:"type"`
	Kind       string `json:"kind"`
	NotNull    bool   `json:"notNull"`
	HasDefault bool   `json:"hasDefault"`
	PrimaryKey bool   `json:"primaryKey"`
	// Editable is false for generated, GENERATED ALWAYS identity, array and binary columns;
	// ReadOnlyReason says why.
	Editable       bool   `json:"editable"`
	ReadOnlyReason string `json:"readOnlyReason,omitempty"`
}

// EditTarget is a table whose rows can be edited by primary key.
type EditTarget struct {
	TableOID uint32 `json:"tableOid"`
	Schema   string `json:"schema"`
	Table    string `json:"table"`
	// Key lists the primary-key columns in index order.
	Key     []string     `json:"key"`
	Columns []EditColumn `json:"columns"`
}

// Edit operations.
const (
	EditUpdate = "update"
	EditInsert = "insert"
	EditDelete = "delete"
)

// RowEdit is one staged change. Values are strings in the column type's input syntax
// (as typed in a literal) or nil for SQL NULL; anything else is rejected. Key holds the
// row's original primary key. (map[string]any rather than *string: Wails can't type the latter.)
type RowEdit struct {
	Op     string         `json:"op"`
	Key    map[string]any `json:"key"`
	Values map[string]any `json:"values"`
}

// textValues checks that every value is a string or nil.
func textValues(m map[string]any) (map[string]*string, error) {
	out := make(map[string]*string, len(m))
	for name, v := range m {
		switch x := v.(type) {
		case nil:
			out[name] = nil
		case string:
			out[name] = &x
		default:
			return nil, fmt.Errorf("value of %q must be text or null, got %T", name, v)
		}
	}
	return out, nil
}

// EditBatch is a set of changes to one table, applied in one transaction.
type EditBatch struct {
	TableOID uint32    `json:"tableOid"`
	Edits    []RowEdit `json:"edits"`
}

// EditResult reports an applied batch.
type EditResult struct {
	Statements int    `json:"statements"`
	Affected   int64  `json:"affected"`
	SQL        string `json:"sql"` // the preview of what ran
	ElapsedMs  int64  `json:"elapsedMs"`
}

// EditStatement is one parameterized statement; Args are bound as text ($n::text::type),
// nil meaning NULL. ExpectOne statements must affect exactly one row.
type EditStatement struct {
	SQL       string
	Preview   string // SQL with the values inlined as literals, for display only
	Args      []*string
	ExpectOne bool
}

// ErrNoPrimaryKey is returned for tables without a primary key.
var ErrNoPrimaryKey = errors.New("the table has no primary key, so its rows can't be edited")

// BuildEditStatements turns staged edits into parameterized statements. Column names are
// checked against the target (so the UI can't smuggle identifiers in) and always quoted;
// values only ever travel as bind parameters.
func BuildEditStatements(t EditTarget, edits []RowEdit) ([]EditStatement, error) {
	if len(t.Key) == 0 {
		return nil, ErrNoPrimaryKey
	}
	cols := make(map[string]EditColumn, len(t.Columns))
	for _, c := range t.Columns {
		cols[c.Name] = c
	}
	table := quoteIdent(t.Schema) + "." + quoteIdent(t.Table)
	var out []EditStatement
	for i, e := range edits {
		st, skip, err := buildEdit(t, cols, table, e)
		if err != nil {
			return nil, fmt.Errorf("change %d: %w", i+1, err)
		}
		if !skip {
			out = append(out, st)
		}
	}
	return out, nil
}

func buildEdit(t EditTarget, cols map[string]EditColumn, table string, e RowEdit) (EditStatement, bool, error) {
	key, err := textValues(e.Key)
	if err != nil {
		return EditStatement{}, false, err
	}
	vals, err := textValues(e.Values)
	if err != nil {
		return EditStatement{}, false, err
	}
	var b stmtBuilder
	switch e.Op {
	case EditUpdate:
		values, err := sortedColumns(cols, vals, true)
		if err != nil {
			return EditStatement{}, false, err
		}
		if len(values) == 0 {
			return EditStatement{}, true, nil // nothing changed
		}
		b.raw("UPDATE " + table + " SET ")
		for i, c := range values {
			if i > 0 {
				b.raw(", ")
			}
			b.raw(quoteIdent(c.Name) + " = ")
			b.param(vals[c.Name], c.Type)
		}
		if err := b.where(t, cols, key); err != nil {
			return EditStatement{}, false, err
		}
		return b.statement(true), false, nil
	case EditInsert:
		values, err := sortedColumns(cols, vals, true)
		if err != nil {
			return EditStatement{}, false, err
		}
		if len(values) == 0 {
			b.raw("INSERT INTO " + table + " DEFAULT VALUES")
			return b.statement(false), false, nil
		}
		names := make([]string, len(values))
		for i, c := range values {
			names[i] = quoteIdent(c.Name)
		}
		b.raw("INSERT INTO " + table + " (" + strings.Join(names, ", ") + ") VALUES (")
		for i, c := range values {
			if i > 0 {
				b.raw(", ")
			}
			b.param(vals[c.Name], c.Type)
		}
		b.raw(")")
		return b.statement(false), false, nil
	case EditDelete:
		b.raw("DELETE FROM " + table)
		if err := b.where(t, cols, key); err != nil {
			return EditStatement{}, false, err
		}
		return b.statement(true), false, nil
	}
	return EditStatement{}, false, fmt.Errorf("unknown operation %q", e.Op)
}

// sortedColumns resolves value column names, in table order; editable demands writable columns.
func sortedColumns(cols map[string]EditColumn, values map[string]*string, editable bool) ([]EditColumn, error) {
	out := make([]EditColumn, 0, len(values))
	for name := range values {
		c, ok := cols[name]
		if !ok {
			return nil, fmt.Errorf("unknown column %q", name)
		}
		if editable && !c.Editable {
			return nil, fmt.Errorf("column %q can't be edited (%s)", name, c.ReadOnlyReason)
		}
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b EditColumn) int { return a.Attnum - b.Attnum })
	return out, nil
}

// stmtBuilder writes the parameterized SQL and its literal preview side by side.
type stmtBuilder struct {
	sql, preview strings.Builder
	args         []*string
}

func (b *stmtBuilder) raw(s string) {
	b.sql.WriteString(s)
	b.preview.WriteString(s)
}

// param binds v as text and casts it to the column type, so any type with a text input
// (numbers, json, enums, timestamps, uuid…) is parsed by the server exactly like a literal.
func (b *stmtBuilder) param(v *string, typ string) {
	b.args = append(b.args, v)
	b.sql.WriteString("$" + strconv.Itoa(len(b.args)) + "::text::" + typ)
	if v == nil {
		b.preview.WriteString("NULL::" + typ)
	} else {
		b.preview.WriteString(QuoteLiteral(*v) + "::" + typ)
	}
}

// where appends the primary-key condition; key must name exactly the key columns, non-NULL.
func (b *stmtBuilder) where(t EditTarget, cols map[string]EditColumn, key map[string]*string) error {
	if len(key) != len(t.Key) {
		return fmt.Errorf("the row key must have exactly the primary key columns (%s)", strings.Join(t.Key, ", "))
	}
	b.raw(" WHERE ")
	for i, name := range t.Key {
		v, ok := key[name]
		if !ok {
			return fmt.Errorf("the row key is missing primary key column %q", name)
		}
		if v == nil {
			return fmt.Errorf("primary key column %q is NULL", name)
		}
		if i > 0 {
			b.raw(" AND ")
		}
		b.raw(quoteIdent(name) + " = ")
		b.param(v, cols[name].Type)
	}
	return nil
}

func (b *stmtBuilder) statement(expectOne bool) EditStatement {
	return EditStatement{SQL: b.sql.String(), Preview: b.preview.String(), Args: b.args, ExpectOne: expectOne}
}

// PreviewSQL joins the statements' previews into a script.
func PreviewSQL(stmts []EditStatement) string {
	var b strings.Builder
	for _, st := range stmts {
		b.WriteString(st.Preview)
		b.WriteString(";\n")
	}
	return b.String()
}

// quoteIdent double-quotes an identifier, doubling embedded quotes.
func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// QuoteLiteral single-quotes a string for display (standard_conforming_strings).
// Never used to build executed SQL: values are always bind parameters.
func QuoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// editColumnKind maps a pg_type name and category to an editor kind and whether the
// column can be edited as text.
func editColumnKind(typname, category string) (kind string, reason string) {
	switch {
	case typname == "json" || typname == "jsonb":
		return KindJSON, ""
	case typname == "bytea":
		return "", "binary"
	case category == "A":
		return "", "array"
	case category == "N":
		return KindNumber, ""
	case category == "B":
		return KindBool, ""
	}
	return KindText, ""
}

// Source table detection for the grid: Postgres reports, per result column, the table OID
// and attribute number it was read from (0 for expressions).

// resultSource returns the single table every table-backed column comes from, the
// per-column attribute numbers (0 = not a column of it, or a repeated one), and a reason
// when the result can't be edited.
func resultSource(oids []uint32, attnums []uint16) (uint32, []int, string) {
	var table uint32
	for _, oid := range oids {
		switch {
		case oid == 0:
		case table == 0:
			table = oid
		case oid != table:
			return 0, nil, "the result combines several tables"
		}
	}
	if table == 0 {
		return 0, nil, "the result doesn't come from a table"
	}
	out := make([]int, len(oids))
	seen := map[uint16]bool{}
	for i, oid := range oids {
		if oid == table && attnums[i] > 0 && !seen[attnums[i]] {
			out[i] = int(attnums[i])
			seen[attnums[i]] = true
		}
	}
	return table, out, ""
}
