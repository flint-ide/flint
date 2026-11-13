package database

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"flint/internal/db"
	"flint/internal/feature/featuretest"
	"flint/internal/mcptools"
)

// noSecrets is a keychain without entries (no password is ever needed here:
// every refusal comes before a connection is opened).
type noSecrets struct{}

func (noSecrets) Get(string) (string, error) { return "", nil }
func (noSecrets) Set(string, string) error   { return nil }
func (noSecrets) Delete(string) error        { return nil }

func newToolAPI(t *testing.T) (*API, *featuretest.Host, *db.Manager) {
	t.Helper()
	h := featuretest.New(context.Background())
	h.Dir = t.TempDir()
	store, err := db.OpenStore(h.Dir, noSecrets{})
	if err != nil {
		t.Fatal(err)
	}
	m := db.NewManager(store)
	t.Cleanup(m.CloseAll)
	return &API{host: h, open: func() (*db.Manager, error) { return m, nil }}, h, m
}

// run_sql_query is offered only while its switch is on; it acts, so the
// server asks before every call.
func TestSQLToolIsOptIn(t *testing.T) {
	api, h, _ := newToolAPI(t)
	if tools := api.agentTools(); len(tools) != 0 {
		t.Fatalf("offered by default: %+v", tools)
	}
	s := h.Settings()
	s.MCPServer.Actions.SQL = true
	h.SetSettings(s)
	tools := api.agentTools()
	if len(tools) != 1 || tools[0].Name != mcptools.ToolSQL || !tools[0].Acts || tools[0].Plan == nil || tools[0].Call != nil {
		t.Fatalf("tools = %+v", tools)
	}
	var schema map[string]any
	if err := json.Unmarshal(tools[0].InputSchema, &schema); err != nil || schema["additionalProperties"] != false {
		t.Fatalf("schema %s: %v", tools[0].InputSchema, err)
	}
}

func TestSQLToolPlan(t *testing.T) {
	api, _, m := newToolAPI(t)
	for _, c := range []db.Connection{
		{Name: "replica", Driver: db.Postgres, Host: "192.0.2.1", User: "u", ReadOnly: true},
		{Name: "primary", Driver: db.Postgres, Host: "192.0.2.2", User: "u"},
	} {
		if _, err := m.Store().Save(c, nil); err != nil {
			t.Fatal(err)
		}
	}
	plan := func(args string) (string, error) {
		p, err := api.planQuery(context.Background(), "/p", json.RawMessage(args))
		return p.Summary, err
	}
	summary, err := plan(`{"connection":"replica","sql":" SELECT * FROM users LIMIT 5 ","database":"app","limit":5}`)
	if err != nil {
		t.Fatal(err)
	}
	if summary != "Run a query on the read-only connection replica, database app (postgres):\nSELECT * FROM users LIMIT 5" {
		t.Errorf("summary = %q", summary)
	}
	for _, tc := range []struct{ args, want string }{
		{`{"sql":"SELECT 1"}`, "connection is required"},
		{`{"connection":"replica"}`, "sql is required"},
		{`{"connection":"primary","sql":"SELECT 1"}`, `"primary" is not read-only`},
		{`{"connection":"other","sql":"SELECT 1"}`, "no saved database connection named"},
		{`{"connection":"replica","sql":"DELETE FROM users"}`, "only reading statements"},
		{`{"connection":"replica","sql":"SELECT 1; DROP TABLE users"}`, "one statement"},
		{`{"connection":"replica","sql":"SELECT 1","database":"a b"}`, "plain name"},
		{`{"connection":"replica","sql":"SELECT 1","limit":100000}`, "limit must be between"},
		{`{"connection":"replica","sql":"SELECT 1","readOnly":false}`, "invalid arguments"},
		{`[]`, "invalid arguments"},
	} {
		if _, err := plan(tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want %q", tc.args, err, tc.want)
		}
	}
}
