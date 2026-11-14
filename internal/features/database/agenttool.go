package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"flint/internal/db"
	"flint/internal/mcpserver"
	"flint/internal/mcptools"
)

// queryTimeout bounds one run_sql_query call.
const queryTimeout = 60 * time.Second

// agentTools offers run_sql_query on Flint's MCP server while the user has
// switched it on (settings.json mcpServer.actions.sql). It acts: the server
// asks the user before every call. Only connections in read-only mode can be
// queried, with one reading statement and a bounded result; the statement is
// recorded in the connection's query history like a console run.
func (d *API) agentTools() []mcpserver.Tool {
	if !d.host.Settings().MCPServer.Actions.SQL {
		return nil
	}
	return []mcpserver.Tool{{
		Name:  mcptools.ToolSQL,
		Title: "Run a SQL query",
		Description: "Runs one reading SQL statement (SELECT, WITH, SHOW, DESCRIBE, EXPLAIN) on one of the user's saved database connections in Flint, by its name, " +
			"and returns the rows as tab-separated text. Only connections the user put in read-only mode can be used; others are refused. " +
			"The statement is recorded in the connection's query history. The user is asked before each query.",
		InputSchema: json.RawMessage(fmt.Sprintf(`{"type":"object","properties":{`+
			`"connection":{"type":"string","description":"Name of a saved connection in Flint's Databases panel (it must be read-only)."},`+
			`"sql":{"type":"string","description":"One reading statement, without a semicolon inside it."},`+
			`"database":{"type":"string","description":"Database on the connection's server. Default: the connection's own."},`+
			`"limit":{"type":"integer","minimum":1,"maximum":%d,"description":"Most rows returned (default %d)."}`+
			`},"required":["connection","sql"],"additionalProperties":false}`, db.MaxReadOnlyRows, db.DefaultReadRows)),
		Acts:    true,
		Timeout: queryTimeout,
		Plan:    d.planQuery,
	}}
}

func (d *API) planQuery(_ context.Context, _ string, args json.RawMessage) (mcpserver.Planned, error) {
	var in struct {
		Connection string `json:"connection"`
		SQL        string `json:"sql"`
		Database   string `json:"database"`
		Limit      int    `json:"limit"`
	}
	dec := json.NewDecoder(strings.NewReader(string(args)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return mcpserver.Planned{}, fmt.Errorf("invalid arguments: %v", err)
	}
	if in.Limit < 0 || in.Limit > db.MaxReadOnlyRows {
		return mcpserver.Planned{}, fmt.Errorf("limit must be between 1 and %d", db.MaxReadOnlyRows)
	}
	m, err := d.open()
	if err != nil {
		return mcpserver.Planned{}, err
	}
	conn, err := m.ReadOnlyConnection(in.Connection)
	if err != nil {
		return mcpserver.Planned{}, err
	}
	if err := db.CheckDatabaseName(in.Database); err != nil {
		return mcpserver.Planned{}, err
	}
	if err := db.CheckReadStatement(in.SQL); err != nil {
		return mcpserver.Planned{}, err
	}
	where := conn.Name
	if in.Database != "" {
		where += ", database " + in.Database
	}
	summary := fmt.Sprintf("Run a query on the read-only connection %s (%s):\n%s", where, conn.Driver, strings.TrimSpace(in.SQL))
	return mcpserver.Planned{Summary: summary, Run: func(ctx context.Context) (string, error) {
		res, err := m.QueryReadOnly(ctx, conn.ID, in.Database, queryID(), in.SQL, in.Limit)
		if err != nil {
			return "", err
		}
		return db.RenderResult(res), nil
	}}, nil
}

// queryID names an agent's query (for the manager's bookkeeping of running
// queries; the call's context cancels it).
func queryID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "agent-" + hex.EncodeToString(b[:])
}
