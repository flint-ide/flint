// Package database is the Database feature: the Databases panel's and the SQL
// consoles' backend (PostgreSQL and ClickHouse connection profiles, queries,
// schema, EXPLAIN, row editing and query history through internal/db). The
// frontend half is frontend/src/features/database.
//
// SQL consoles are scratchpads (internal/scratch, core): a console only stores
// the connection id it runs on, so switching this feature off leaves consoles
// readable as text.
package database

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"flint/internal/feature"
)

// ID is the feature's id, also its key under settings.json's features.enabled.
const ID = "database"

// Module declares the feature (listed in package main's features.go).
var Module = feature.Module{
	ID:       ID,
	Optional: true,
	New: func(h feature.Host) feature.Instance {
		api := newAPI(h)
		return feature.Instance{
			Services:   []application.Service{application.NewService(api)},
			AgentTools: api.agentTools,
			Close:      api.close,
		}
	},
}
