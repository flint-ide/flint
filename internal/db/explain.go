package db

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Thresholds of the plan warnings.
const (
	// bigScanRows is how many rows a Seq Scan may read before it is flagged.
	bigScanRows = 10_000
	// bigScanCost flags a Seq Scan by estimate alone (≈ 100k narrow rows at default costs).
	bigScanCost = 2_500
	// misestimateFactor flags nodes whose actual rows differ from the estimate this much.
	misestimateFactor = 10
)

// ExplainBuffers are a node's block counts (EXPLAIN ANALYZE … BUFFERS).
type ExplainBuffers struct {
	SharedHit     int64 `json:"sharedHit"`
	SharedRead    int64 `json:"sharedRead"`
	SharedDirtied int64 `json:"sharedDirtied"`
	SharedWritten int64 `json:"sharedWritten"`
	LocalHit      int64 `json:"localHit"`
	LocalRead     int64 `json:"localRead"`
	TempRead      int64 `json:"tempRead"`
	TempWritten   int64 `json:"tempWritten"`
}

// PlanDetail is a secondary property of a plan node shown when it is expanded.
type PlanDetail struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// PlanNode is one node of a Postgres plan. Actual* fields are set only by ANALYZE;
// rows and times are per loop, as Postgres reports them.
type PlanNode struct {
	NodeType     string `json:"nodeType"`
	Relationship string `json:"relationship,omitempty"` // "Outer", "Inner", "InitPlan", …
	Relation     string `json:"relation,omitempty"`     // schema-qualified when known
	Alias        string `json:"alias,omitempty"`
	Index        string `json:"index,omitempty"`
	JoinType     string `json:"joinType,omitempty"`

	StartupCost float64 `json:"startupCost"`
	TotalCost   float64 `json:"totalCost"`
	// SelfCost is TotalCost minus the children's total cost (≥ 0).
	SelfCost  float64 `json:"selfCost"`
	PlanRows  float64 `json:"planRows"`
	PlanWidth int64   `json:"planWidth"`

	Analyzed        bool    `json:"analyzed"`
	NeverExecuted   bool    `json:"neverExecuted,omitempty"`
	ActualRows      float64 `json:"actualRows"`
	ActualLoops     float64 `json:"actualLoops"`
	ActualStartupMs float64 `json:"actualStartupMs"`
	ActualTotalMs   float64 `json:"actualTotalMs"`
	// TotalMs is the node's inclusive time over all loops; SelfMs excludes its children.
	TotalMs float64 `json:"totalMs"`
	SelfMs  float64 `json:"selfMs"`

	Filter              string          `json:"filter,omitempty"`
	RowsRemovedByFilter float64         `json:"rowsRemovedByFilter,omitempty"`
	Buffers             *ExplainBuffers `json:"buffers,omitempty"`
	Details             []PlanDetail    `json:"details"`
	Warnings            []string        `json:"warnings"`
	Children            []PlanNode      `json:"children"`
}

// ExplainLine is one line of a text plan (ClickHouse), with its nesting depth.
type ExplainLine struct {
	Depth int    `json:"depth"`
	Text  string `json:"text"`
}

// ExplainResult is the outcome of Explain: a node tree (Postgres) or indented lines
// (ClickHouse), plus the raw output for copying.
type ExplainResult struct {
	Driver  Driver `json:"driver"`
	Analyze bool   `json:"analyze"`
	// Statement is the EXPLAIN statement that ran.
	Statement string `json:"statement"`
	Raw       string `json:"raw"`

	Plan        *PlanNode `json:"plan,omitempty"`
	PlanningMs  float64   `json:"planningMs"`
	ExecutionMs float64   `json:"executionMs"`
	// Warnings counts the warnings of every node.
	Warnings int `json:"warnings"`
	// RolledBack is set when ANALYZE ran the statement in a transaction that was rolled back.
	RolledBack bool `json:"rolledBack"`

	Lines []ExplainLine `json:"lines,omitempty"`

	// NeedsConfirm is set (and nothing ran) when ANALYZE would execute a statement that
	// may modify data; the caller asks the user and retries with allowWrites.
	NeedsConfirm  bool   `json:"needsConfirm"`
	ConfirmReason string `json:"confirmReason,omitempty"`

	ElapsedMs int64 `json:"elapsedMs"`
}

// explainTarget strips trailing semicolons and refuses statements that already are an EXPLAIN.
func explainTarget(sql string) (string, error) {
	s := strings.TrimRight(strings.TrimSpace(sql), "; \t\r\n")
	if s == "" {
		return "", errors.New("nothing to explain")
	}
	if firstKeyword(s) == "EXPLAIN" {
		return "", errors.New("the statement already is an EXPLAIN; run it as a query")
	}
	return s, nil
}

// postgresExplainSQL builds the EXPLAIN statement for a Postgres query.
func postgresExplainSQL(sql string, analyze bool) string {
	if analyze {
		return "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) " + sql
	}
	return "EXPLAIN (FORMAT JSON) " + sql
}

// clickhouseExplainSQL builds the EXPLAIN statement for ClickHouse: the logical plan
// (with index usage) or, for "analyze", the physical pipeline. Neither runs the query.
func clickhouseExplainSQL(sql string, pipeline bool) string {
	if pipeline {
		return "EXPLAIN PIPELINE " + sql
	}
	return "EXPLAIN PLAN indexes = 1 " + sql
}

var dmlWord = regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|MERGE|TRUNCATE|CALL|DO|COPY|INTO)\b`)

// analyzeModifies reports whether EXPLAIN ANALYZE of sql may change data: anything but a
// plain SELECT / VALUES / TABLE, or a WITH (or SELECT … INTO) naming a writing keyword.
// False positives only cost an extra confirmation.
func analyzeModifies(sql string) bool {
	switch firstKeyword(sql) {
	case "SELECT", "WITH":
		return dmlWord.MatchString(stripLiterals(sql))
	case "VALUES", "TABLE":
		return false
	}
	return true
}

// stripLiterals blanks quoted strings, quoted identifiers and comments so keyword checks
// don't match inside them.
func stripLiterals(sql string) string {
	var b strings.Builder
	b.Grow(len(sql))
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch {
		case c == '\'' || c == '"':
			j := i + 1
			for j < len(sql) {
				if sql[j] == c {
					if j+1 < len(sql) && sql[j+1] == c {
						j += 2
						continue
					}
					break
				}
				j++
			}
			b.WriteByte(' ')
			i = j
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
			b.WriteByte(' ')
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				i = len(sql)
			} else {
				i += end + 3
			}
			b.WriteByte(' ')
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// ParsePostgresPlan decodes EXPLAIN (FORMAT JSON) output into a node tree with self
// times, cost shares and warnings.
func ParsePostgresPlan(raw []byte) (*ExplainResult, error) {
	var top []map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("parse EXPLAIN JSON: %w", err)
	}
	if len(top) == 0 {
		return nil, errors.New("parse EXPLAIN JSON: empty plan")
	}
	plan, ok := top[0]["Plan"].(map[string]any)
	if !ok {
		return nil, errors.New(`parse EXPLAIN JSON: no "Plan"`)
	}
	root := parsePlanNode(plan)
	res := &ExplainResult{
		Driver:      Postgres,
		Analyze:     root.Analyzed,
		Raw:         string(raw),
		Plan:        &root,
		PlanningMs:  num(top[0], "Planning Time"),
		ExecutionMs: num(top[0], "Execution Time"),
	}
	res.Warnings = countWarnings(root)
	return res, nil
}

func countWarnings(n PlanNode) int {
	c := len(n.Warnings)
	for _, ch := range n.Children {
		c += countWarnings(ch)
	}
	return c
}

// planDetailKeys are shown (in this order) when a node is expanded.
var planDetailKeys = []string{
	"Index Cond", "Recheck Cond", "Hash Cond", "Merge Cond", "Join Filter", "Sort Key",
	"Presorted Key", "Group Key", "Strategy", "Partial Mode", "Scan Direction", "Sort Method",
	"Sort Space Used", "Sort Space Type", "Hash Buckets", "Hash Batches", "Peak Memory Usage",
	"Rows Removed by Index Recheck", "Rows Removed by Join Filter", "Heap Fetches",
	"Workers Planned", "Workers Launched", "CTE Name", "Subplan Name", "Function Name",
	"Command", "Operation", "Cache Key", "Cache Hits", "Cache Misses", "Output",
}

func parsePlanNode(m map[string]any) PlanNode {
	n := PlanNode{
		NodeType:     str(m, "Node Type"),
		Relationship: str(m, "Parent Relationship"),
		Alias:        str(m, "Alias"),
		Index:        str(m, "Index Name"),
		JoinType:     str(m, "Join Type"),
		StartupCost:  num(m, "Startup Cost"),
		TotalCost:    num(m, "Total Cost"),
		PlanRows:     num(m, "Plan Rows"),
		PlanWidth:    int64(num(m, "Plan Width")),
		Filter:       str(m, "Filter"),
		Details:      []PlanDetail{},
		Warnings:     []string{},
		Children:     []PlanNode{},
	}
	if rel := str(m, "Relation Name"); rel != "" {
		if schema := str(m, "Schema"); schema != "" {
			rel = schema + "." + rel
		}
		n.Relation = rel
	}
	if _, ok := m["Actual Loops"]; ok {
		n.Analyzed = true
		n.ActualLoops = num(m, "Actual Loops")
		n.ActualRows = num(m, "Actual Rows")
		n.ActualStartupMs = num(m, "Actual Startup Time")
		n.ActualTotalMs = num(m, "Actual Total Time")
		n.NeverExecuted = n.ActualLoops == 0
		n.TotalMs = n.ActualTotalMs * n.ActualLoops
		n.RowsRemovedByFilter = num(m, "Rows Removed by Filter")
	}
	n.Buffers = parseBuffers(m)
	for _, key := range planDetailKeys {
		if v, ok := m[key]; ok {
			n.Details = append(n.Details, PlanDetail{Key: key, Value: detailText(v)})
		}
	}
	if children, ok := m["Plans"].([]any); ok {
		for _, c := range children {
			if cm, ok := c.(map[string]any); ok {
				n.Children = append(n.Children, parsePlanNode(cm))
			}
		}
	}

	var childCost, childMs float64
	for _, c := range n.Children {
		// InitPlans/SubPlans are charged to the parent's cost but not inside its total;
		// subtracting them would undercount, so only regular children are subtracted.
		if c.Relationship == "InitPlan" || c.Relationship == "SubPlan" {
			continue
		}
		childCost += c.TotalCost
		childMs += c.TotalMs
	}
	n.SelfCost = math.Max(0, n.TotalCost-childCost)
	if n.Analyzed {
		n.SelfMs = math.Max(0, n.TotalMs-childMs)
	}
	n.Warnings = planWarnings(n, m)
	return n
}

// planWarnings flags the usual suspects: big sequential scans, bad row estimates and
// sorts or hashes that spilled to disk.
func planWarnings(n PlanNode, m map[string]any) []string {
	w := []string{}
	if n.NodeType == "Seq Scan" || n.NodeType == "Parallel Seq Scan" {
		if n.Analyzed && !n.NeverExecuted {
			scanned := (n.ActualRows + n.RowsRemovedByFilter) * n.ActualLoops
			if scanned >= bigScanRows {
				w = append(w, fmt.Sprintf("Seq Scan on a big table: %s rows read", humanCount(scanned)))
			}
		} else if !n.Analyzed && (n.PlanRows >= bigScanRows || n.TotalCost >= bigScanCost) {
			w = append(w, fmt.Sprintf("Seq Scan on a big table (estimated %s rows, cost %.0f)", humanCount(n.PlanRows), n.TotalCost))
		}
	}
	if n.Analyzed && !n.NeverExecuted {
		est, act := math.Max(n.PlanRows, 1), math.Max(n.ActualRows, 1)
		if f := math.Max(est/act, act/est); f >= misestimateFactor {
			dir := "under"
			if est > act {
				dir = "over"
			}
			w = append(w, fmt.Sprintf("Row estimate off by %s× (%sestimated: %s planned, %s actual)",
				humanCount(math.Round(f)), dir, humanCount(n.PlanRows), humanCount(n.ActualRows)))
		}
	}
	method := str(m, "Sort Method")
	if str(m, "Sort Space Type") == "Disk" || strings.Contains(method, "external") {
		w = append(w, fmt.Sprintf("Sort spilled to disk (%s, %.0f kB)", method, num(m, "Sort Space Used")))
	}
	if batches := num(m, "Hash Batches"); batches > 1 {
		w = append(w, fmt.Sprintf("Hash spilled to disk in %.0f batches (raise work_mem?)", batches))
	}
	return w
}

func parseBuffers(m map[string]any) *ExplainBuffers {
	b := ExplainBuffers{
		SharedHit:     int64(num(m, "Shared Hit Blocks")),
		SharedRead:    int64(num(m, "Shared Read Blocks")),
		SharedDirtied: int64(num(m, "Shared Dirtied Blocks")),
		SharedWritten: int64(num(m, "Shared Written Blocks")),
		LocalHit:      int64(num(m, "Local Hit Blocks")),
		LocalRead:     int64(num(m, "Local Read Blocks")),
		TempRead:      int64(num(m, "Temp Read Blocks")),
		TempWritten:   int64(num(m, "Temp Written Blocks")),
	}
	if _, ok := m["Shared Hit Blocks"]; !ok {
		return nil
	}
	return &b
}

func str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func num(m map[string]any, key string) float64 {
	f, _ := m[key].(float64)
	return f
}

func detailText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return humanCount(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case []any:
		parts := make([]string, len(x))
		for i, p := range x {
			parts[i] = detailText(p)
		}
		return strings.Join(parts, ", ")
	}
	data, _ := json.Marshal(v)
	return string(data)
}

// humanCount prints whole numbers without a fraction and others with up to 2 decimals.
func humanCount(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e15 {
		return fmt.Sprintf("%.0f", f)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", f), "0"), ".")
}

// ParseTextPlan turns indented text plan lines (ClickHouse EXPLAIN output) into lines
// with a nesting depth: two leading spaces per level, a tab counting as one level.
func ParseTextPlan(lines []string) []ExplainLine {
	out := make([]ExplainLine, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimRight(l, " \r")
		if l == "" {
			continue
		}
		text := strings.TrimLeft(l, " \t")
		indent := l[:len(l)-len(text)]
		spaces := len(indent) + strings.Count(indent, "\t") // a tab counts as two spaces
		out = append(out, ExplainLine{Depth: spaces / 2, Text: text})
	}
	return out
}
