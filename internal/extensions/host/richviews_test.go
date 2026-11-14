package host

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"flint/internal/extensions/manifest"
)

func viewManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	m, err := manifest.Parse([]byte(`{"contract":1,"id":"acme.jira","name":"Jira","version":"0.1.0","run":{"command":"bin/jira"},
  "contributes":{"commands":[{"id":"acme.jira.open","title":"Open"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCleanTable(t *testing.T) {
	m := viewManifest(t)
	v, _ := cleanResolved(json.RawMessage(`{"kind":"table",
  "columns":[{"id":"key","title":"Key"},{"id":"points","type":"number"},{"id":"updated","type":"date","align":"center"},
             {"id":"status","type":"badge","badges":{"Done":"ok","Blocked":"error","Odd":"#ff0000"}},{"id":"key"},{"id":""},{"id":"raw","type":"html","align":"justify"}],
  "rows":[{"id":"ABC-1","cells":{"key":"ABC-1\u001b[31m","points":3,"updated":"2026-11-01T09:30:00Z","status":"Done","raw":"<b>x</b>","other":1},
           "command":{"id":"acme.jira.open","args":{"key":"ABC-1"}},"actions":[{"id":"assign","label":"Assign"}]},
          {"id":"ABC-2","cells":{"key":12,"points":"3","updated":"last week","status":true},"command":{"id":"git.push"}},
          {"id":"ABC-1","cells":{}},{"cells":{"key":"no id"}},{"id":"ABC-3"}],
  "sort":{"column":"updated","descending":true},
  "actions":[{"id":"open","label":"Open"},{"id":"","label":"x"}]}`), "table", m)
	if v.Kind != "table" || v.Table == nil {
		t.Fatalf("view = %+v", v)
	}
	tb := v.Table
	wantCols := []Column{
		{ID: "key", Title: "Key", Type: "text", Align: "start"},
		{ID: "points", Title: "points", Type: "number", Align: "end"},
		{ID: "updated", Title: "updated", Type: "date", Align: "center"},
		{ID: "status", Title: "status", Type: "badge", Align: "start", Badges: map[string]string{"Done": "ok", "Blocked": "error"}},
		{ID: "raw", Title: "raw", Type: "text", Align: "start"},
	}
	if !reflect.DeepEqual(tb.Columns, wantCols) {
		t.Errorf("columns = %+v", tb.Columns)
	}
	if len(tb.Rows) != 3 {
		t.Fatalf("rows = %+v", tb.Rows)
	}
	if got, want := tb.Rows[0].Cells, []any{"ABC-1[31m", float64(3), "2026-11-01T09:30:00Z", "Done", "<b>x</b>"}; !reflect.DeepEqual(got, want) {
		t.Errorf("row 1 = %#v", got)
	}
	if tb.Rows[0].Command == nil || tb.Rows[0].Command.ID != "acme.jira.open" || len(tb.Rows[0].Actions) != 1 {
		t.Errorf("row 1 command %+v, actions %+v", tb.Rows[0].Command, tb.Rows[0].Actions)
	}
	if got, want := tb.Rows[1].Cells, []any{"12", nil, nil, "true", nil}; !reflect.DeepEqual(got, want) {
		t.Errorf("row 2 = %#v", got)
	}
	if tb.Rows[1].Command != nil {
		t.Errorf("a command of Flint's is kept: %+v", tb.Rows[1].Command)
	}
	if got, want := tb.Rows[2].Cells, []any{nil, nil, nil, nil, nil}; !reflect.DeepEqual(got, want) {
		t.Errorf("row 3 = %#v", got)
	}
	if tb.Sort == nil || tb.Sort.Column != "updated" || !tb.Sort.Descending {
		t.Errorf("sort = %+v", tb.Sort)
	}
	if len(tb.Actions) != 1 || tb.Actions[0].ID != "open" {
		t.Errorf("actions = %+v", tb.Actions)
	}
}

func TestCleanTableBounds(t *testing.T) {
	m := viewManifest(t)
	var cols, rows []string
	for i := 0; i < maxColumns+5; i++ {
		cols = append(cols, fmt.Sprintf(`{"id":"c%d"}`, i))
	}
	for i := 0; i < maxItems+3; i++ {
		rows = append(rows, fmt.Sprintf(`{"id":"r%d","cells":{"c0":%q}}`, i, strings.Repeat("x", 10)))
	}
	rows[0] = `{"id":"r0","cells":{"c0":"` + strings.Repeat("é", maxLabel+50) + `"}}`
	raw := `{"kind":"table","columns":[` + strings.Join(cols, ",") + `],"rows":[` + strings.Join(rows, ",") + `],"sort":{"column":"nope"}}`
	v, _ := cleanResolved(json.RawMessage(raw), "table", m)
	if v.Table == nil || len(v.Table.Columns) != maxColumns || len(v.Table.Rows) != maxItems || !v.Truncated || v.Table.Sort != nil {
		t.Fatalf("kind %s, truncated %v", v.Kind, v.Truncated)
	}
	if s := v.Table.Rows[0].Cells[0].(string); len([]rune(s)) != maxLabel {
		t.Errorf("a cell of %d characters", len([]rune(s)))
	}

	for name, tc := range map[string]struct{ raw, want string }{
		"no columns":   {`{"kind":"table","columns":[],"rows":[]}`, "it has no columns"},
		"wrong shape":  {`{"kind":"table","columns":{"a":1}}`, "Flint can't draw"},
		"another kind": {`{"kind":"chart","type":"bar"}`, "sent a chart view for a table panel"},
		"not a view":   {`[1,2]`, "not a view"},
		"a message":    {`{"kind":"message","text":"Sign in first.","actions":[{"id":"in","label":"Sign In"}]}`, "Sign in first."},
	} {
		t.Run(name, func(t *testing.T) {
			v, _ := cleanResolved(json.RawMessage(tc.raw), "table", m)
			if v.Kind != "message" || !strings.Contains(v.Text, tc.want) || v.Table != nil {
				t.Errorf("view = %+v, want a message with %q", v, tc.want)
			}
		})
	}
}

func TestCleanForm(t *testing.T) {
	v, fields := cleanResolved(json.RawMessage(`{"kind":"form","id":"comment","description":"Say something.","submit":"Add Comment",
  "fields":[{"id":"comment","type":"string","title":"Comment","multiline":true,"maxLength":5000,"required":true,"placeholder":"Text"},
            {"id":"notify","type":"boolean","title":"Notify watchers","default":true,"value":false},
            {"id":"points","type":"integer","minimum":1,"maximum":13,"default":3,"value":40},
            {"id":"kind","type":"string","enum":["bug","task"],"value":"task","multiline":true,"secret":true},
            {"id":"token","type":"string","secret":true,"multiline":true,"value":"s3cret","default":"s3cret"},
            {"id":"labels","type":"array","items":{"type":"string"},"value":["a","b"],"someNewKeyword":{"x":1}}]}`), "form", viewManifest(t))
	if v.Kind != "form" || v.Form == nil || len(v.Form.Fields) != 6 || len(fields) != 6 {
		t.Fatalf("view = %+v", v)
	}
	f := v.Form
	if f.ID != "comment" || f.Submit != "Add Comment" || f.Description != "Say something." {
		t.Errorf("form = %+v", f)
	}
	if c := f.Fields[0]; !c.Multiline || !c.Required || c.Secret || c.Value != "" || c.Placeholder != "Text" || c.Property.MaxLength != maxFieldLength {
		t.Errorf("comment = %+v", c)
	}
	if n := f.Fields[1]; n.Value != false || n.Property.Default != true {
		t.Errorf("notify = %+v", n)
	}
	if p := f.Fields[2]; p.Value != float64(3) {
		t.Errorf("an invalid value is kept: %+v", p)
	}
	if k := f.Fields[3]; k.Value != "task" || k.Multiline || k.Secret {
		t.Errorf("kind = %+v", k)
	}
	if s := f.Fields[4]; !s.Secret || s.Multiline || s.Value != "" || s.Property.Default != "" {
		t.Errorf("a secret's value reaches the page: %+v", s)
	}
	if data, _ := json.Marshal(v); strings.Contains(string(data), "s3cret") {
		t.Errorf("the view carries the secret: %s", data)
	}
	if l := f.Fields[5]; !reflect.DeepEqual(l.Value, []any{"a", "b"}) {
		t.Errorf("labels = %+v", l)
	}

	var many []string
	for i := 0; i <= maxFormFields; i++ {
		many = append(many, fmt.Sprintf(`{"id":"f%d","type":"string"}`, i))
	}
	for name, tc := range map[string]struct{ raw, want string }{
		"unknown type":  {`{"kind":"form","fields":[{"id":"x","type":"object"}]}`, `field 1: type "object" is not supported`},
		"no id":         {`{"kind":"form","fields":[{"type":"string"}]}`, "field 1"},
		"twice":         {`{"kind":"form","fields":[{"id":"x","type":"string"},{"id":"x","type":"boolean"}]}`, "field x is there twice"},
		"bad default":   {`{"kind":"form","fields":[{"id":"x","type":"integer","default":"1"}]}`, "default"},
		"bad pattern":   {`{"kind":"form","fields":[{"id":"x","type":"string","pattern":"("}]}`, "pattern"},
		"too many":      {`{"kind":"form","fields":[` + strings.Join(many, ",") + `]}`, "more than 50 fields"},
		"control chars": {`{"kind":"form","fields":[{"id":"x","type":"string","title":"a\u0007b"}]}`, "field 1"},
	} {
		t.Run(name, func(t *testing.T) {
			v, fields := cleanResolved(json.RawMessage(tc.raw), "form", viewManifest(t))
			if v.Kind != "message" || !strings.Contains(v.Text, tc.want) || fields != nil {
				t.Errorf("view = %+v, want a message with %q", v, tc.want)
			}
		})
	}
	if v, _ := cleanResolved(json.RawMessage(`{"kind":"form","fields":[]}`), "form", viewManifest(t)); v.Form == nil || v.Form.Submit != "Submit" {
		t.Errorf("an empty form = %+v", v)
	}
}

func TestCleanChart(t *testing.T) {
	m := viewManifest(t)
	v, _ := cleanResolved(json.RawMessage(`{"kind":"chart","type":"line","title":"Open issues","color":"#ff0000",
  "x":{"type":"date","label":"Week"},"y":{"label":"Issues","min":0},
  "series":[{"id":"open","label":"Open","color":"red","points":[["2026-10-01",14],["2026-10-08T00:00:00Z",11],["soon",1],["2026-10-15","many"],["2026-10-15"],["2026-10-22",null]]},
            {"points":[["2026-10-01",2]]},{"id":"open","points":[]}]}`), "chart", m)
	if v.Kind != "chart" || v.Chart == nil {
		t.Fatalf("view = %+v", v)
	}
	c := v.Chart
	if c.Type != "line" || c.Title != "Open issues" || c.X.Type != "date" || c.X.Label != "Week" || c.Y.Min == nil || *c.Y.Min != 0 || c.Y.Max != nil {
		t.Errorf("chart = %+v", c)
	}
	if len(c.Series) != 2 || c.Series[1].ID != "2" || c.Series[1].Label != "2" {
		t.Fatalf("series = %+v", c.Series)
	}
	want := []Point{{X: float64(1790812800000), Y: 14}, {X: float64(1791417600000), Y: 11}}
	if !reflect.DeepEqual(c.Series[0].Points, want) {
		t.Errorf("points = %+v", c.Series[0].Points)
	}
	if data, _ := json.Marshal(v); strings.Contains(string(data), "red") || strings.Contains(string(data), "ff0000") {
		t.Errorf("a color reaches the page: %s", data)
	}

	// Categories and numbers; bounds.
	var series, points []string
	for i := 0; i < maxPoints+10; i++ {
		points = append(points, fmt.Sprintf(`[%d,%d]`, i, i*2))
	}
	for i := 0; i < maxSeries+2; i++ {
		series = append(series, fmt.Sprintf(`{"id":"s%d","points":[%s]}`, i, strings.Join(points, ",")))
	}
	v, _ = cleanResolved(json.RawMessage(`{"kind":"chart","type":"area","x":{"type":"number"},"y":{"min":5,"max":1},"series":[`+strings.Join(series, ",")+`]}`), "chart", m)
	if v.Chart == nil || len(v.Chart.Series) != maxSeries || len(v.Chart.Series[0].Points) != maxPoints || !v.Truncated || v.Chart.Y.Min != nil || v.Chart.Y.Max != nil {
		t.Fatalf("bounds: kind %s, truncated %v", v.Kind, v.Truncated)
	}
	if p := v.Chart.Series[0].Points[3]; p.X != float64(3) || p.Y != 6 {
		t.Errorf("point = %+v", p)
	}
	v, _ = cleanResolved(json.RawMessage(`{"kind":"chart","type":"bar","x":{"type":"weird"},"series":[{"id":"n","points":[["go",3],[7,1],["",2],[true,2],["`+strings.Repeat("x", 300)+`",1]]}]}`), "chart", m)
	if v.Chart == nil || v.Chart.X.Type != "category" {
		t.Fatalf("view = %+v", v)
	}
	pts := v.Chart.Series[0].Points
	if len(pts) != 3 || pts[0].X != "go" || pts[1].X != "7" || len([]rune(pts[2].X.(string))) != maxCategory {
		t.Errorf("category points = %+v", pts)
	}
	if v, _ := cleanResolved(json.RawMessage(`{"kind":"chart","type":"pie","series":[]}`), "chart", m); v.Kind != "message" || !strings.Contains(v.Text, `chart type "pie"`) {
		t.Errorf("a pie chart = %+v", v)
	}
}

func TestParseDate(t *testing.T) {
	for s, want := range map[string]bool{
		"2026-11-01": true, "2026-11-01T09:30:00Z": true, "2026-11-01T09:30:00+02:00": true, "2026-11-01T09:30:00.123Z": true,
		"2026-11-01T09:30": true, "2026-11-01T09:30:00": true, "": false, "11/01/2026": false, "2026-13-01": false, "yesterday": false,
	} {
		if _, ok := parseDate(s); ok != want {
			t.Errorf("parseDate(%q) = %v", s, ok)
		}
	}
}

func TestRichViewsOfAnExtension(t *testing.T) {
	f := setup(t, nil)
	ctx := context.Background()

	// A table panel: typed cells, own commands only, text kept as text.
	v, err := f.h.ResolveView(ctx, "test.ext", "test.ext.table", "", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind != "table" || v.Table == nil || len(v.Table.Columns) != 5 || len(v.Table.Rows) != 3 {
		t.Fatalf("table = %+v", v)
	}
	if got, want := v.Table.Rows[0].Cells, []any{"<b>one</b>", float64(3), "2026-11-01T09:30:00Z", "up", "true"}; !reflect.DeepEqual(got, want) {
		t.Errorf("row 1 = %#v", got)
	}
	if v.Table.Columns[0].Title != "Name[31m" || v.Table.Rows[1].Command != nil || v.Table.Rows[0].Command == nil {
		t.Errorf("columns %+v, rows %+v", v.Table.Columns, v.Table.Rows)
	}
	// The old entry point draws the new kinds too.
	if old, err := f.h.Resolve(ctx, "test.ext", "test.ext.table", ""); err != nil || old.Table == nil {
		t.Errorf("Resolve = %+v, %v", old, err)
	}
	if err := f.h.ViewAction(ctx, "test.ext", "test.ext.table", "", "r1", "open"); err != nil {
		t.Fatal(err)
	}
	if log := f.log(t); !strings.Contains(log, `view/action {"action":"open","item":"r1","view":"test.ext.table"}`) {
		t.Errorf("view/action in the log:\n%s", log)
	}

	// A tab view: the key and the tab's data reach the extension.
	tab, err := f.h.ResolveView(ctx, "test.ext", "test.ext.tab", "board", json.RawMessage(`{"sprint":42}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if last := tab.Table.Rows[len(tab.Table.Rows)-1]; last.ID != "key:board" || last.Cells[0] != `{"sprint":42}` {
		t.Errorf("tab row = %+v", last)
	}
	if err := f.h.ViewAction(ctx, "test.ext", "test.ext.tab", "board", "r1", "open"); err != nil {
		t.Fatal(err)
	}
	if log := f.log(t); !strings.Contains(log, `"key":"board"`) {
		t.Errorf("the tab's key in view/action:\n%s", log)
	}
	if err := f.h.SetViewVisible("test.ext", "test.ext.tab", "board", true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "view/visible with the key", func() bool {
		return strings.Contains(f.log(t), `view/visible {"key":"board","view":"test.ext.tab","visible":true}`)
	})
	for name, call := range map[string]func() error{
		"a tab without a key": func() error { _, err := f.h.ResolveView(ctx, "test.ext", "test.ext.tab", "", nil, ""); return err },
		"a panel with a key":  func() error { _, err := f.h.ResolveView(ctx, "test.ext", "test.ext.table", "k", nil, ""); return err },
		"an unknown view":     func() error { _, err := f.h.ResolveView(ctx, "test.ext", "test.ext.nope", "", nil, ""); return err },
		"too much data": func() error {
			_, err := f.h.ResolveView(ctx, "test.ext", "test.ext.tab", "k", json.RawMessage(`"`+strings.Repeat("x", maxTabData)+`"`), "")
			return err
		},
		"an action on an unknown view": func() error { return f.h.ViewAction(ctx, "test.ext", "git.push", "", "r1", "open") },
		"visible, unknown view":        func() error { return f.h.SetViewVisible("test.ext", "test.ext.nope", "", true) },
	} {
		if err := call(); err == nil {
			t.Errorf("%s: no error", name)
		}
	}

	// view/refresh for a tab carries its key; an unknown view is refused.
	if err := f.exec(t, "status", map[string]string{"key": "board"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the tab's refresh", func() bool { return len(f.app.eventsLike(EventRefresh)) >= 2 })
	if r := f.app.eventsLike(EventRefresh); len(r) != 2 || !strings.Contains(r[1], `"view":"test.ext.tab"`) || !strings.Contains(r[1], `"key":"board"`) {
		t.Errorf("refreshes = %v", r)
	}
	waitFor(t, "the refused refresh", func() bool { return strings.Contains(f.log(t), `view/refresh: no panel or tab view "test.ext.nope"`) })

	// A chart.
	c, err := f.h.ResolveView(ctx, "test.ext", "test.ext.chart", "", nil, "")
	if err != nil || c.Chart == nil || len(c.Chart.Series) != 2 || len(c.Chart.Series[0].Points) != 2 || c.Chart.Series[1].Label != "blocked" {
		t.Fatalf("chart = %+v, %v", c, err)
	}
}

func TestFormSubmit(t *testing.T) {
	f := setup(t, nil)
	ctx := context.Background()
	submit := func(values string) (FormResult, error) {
		return f.h.SubmitForm(ctx, "test.ext", "test.ext.form", "", json.RawMessage(values))
	}

	// Before the form was drawn there is nothing to check the values against.
	if _, err := submit(`{"name":"Ada"}`); err == nil || !strings.Contains(err.Error(), "refresh") {
		t.Errorf("a submit before the form was drawn: %v", err)
	}
	v, err := f.h.ResolveView(ctx, "test.ext", "test.ext.form", "", nil, "")
	if err != nil || v.Form == nil || len(v.Form.Fields) != 5 {
		t.Fatalf("form = %+v, %v", v, err)
	}
	if data, _ := json.Marshal(v); strings.Contains(string(data), "leaked") {
		t.Errorf("the secret field's value reached the page: %s", data)
	}
	if v.Form.Fields[2].Value != float64(3) || v.Form.Fields[3].Value != "safe" || !v.Form.Fields[4].Multiline {
		t.Errorf("fields = %+v", v.Form.Fields)
	}

	// Values Flint refuses never reach the extension.
	res, err := submit(`{"name":"","token":7,"count":12,"mode":"reckless","notes":"fine"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"name": "is required", "token": "must be text", "count": "must be between 1 and 9", "mode": "must be one of fast, safe"}
	if !reflect.DeepEqual(res.Errors, want) {
		t.Errorf("errors = %v", res.Errors)
	}
	if strings.Contains(f.log(t), "form/submit") {
		t.Errorf("refused values were sent:\n%s", f.log(t))
	}
	if res, _ := submit(`{"name":"much too long a name"}`); res.Errors["name"] != "at most 10 characters" {
		t.Errorf("errors = %v", res.Errors)
	}
	for _, bad := range []string{`[1]`, `"x"`, `{`} {
		if _, err := submit(bad); err == nil {
			t.Errorf("values %s: no error", bad)
		}
	}

	// Valid values: defaults filled in, unknown keys dropped, the secret passed on.
	res, err = submit(`{"name":"Ada","token":"hunter2","extra":"dropped","notes":"two\nlines"}`)
	if err != nil || len(res.Errors) != 0 || res.Error != "" || res.Message != "Saved." {
		t.Fatalf("submit = %+v, %v", res, err)
	}
	wantSent := `form/submit {"values":{"count":3,"mode":"fast","name":"Ada","notes":"two\nlines","token":"hunter2"},"view":"test.ext.form"}`
	waitFor(t, "form/submit in the log", func() bool { return strings.Contains(f.log(t), "form/submit") })
	if log := f.log(t); !strings.Contains(log, wantSent) {
		t.Errorf("sent:\n%s\nwant %s", log, wantSent)
	}

	// The extension's own refusals: only for fields of the form, as plain text.
	res, err = submit(`{"name":"taken"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Errors, map[string]string{"name": "is taken[31m"}) || res.Error != "Nothing was saved." {
		t.Errorf("result = %+v", res)
	}

	// Not a form; a form Flint can't draw leaves nothing to submit.
	if _, err := f.h.SubmitForm(ctx, "test.ext", "test.ext.table", "", json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "not a form") {
		t.Errorf("submit to a table: %v", err)
	}
	bad, err := f.h.ResolveView(ctx, "test.ext", "test.ext.badform", "", nil, "")
	if err != nil || bad.Kind != "message" || !strings.Contains(bad.Text, "can't draw") {
		t.Errorf("bad form = %+v, %v", bad, err)
	}
	if _, err := f.h.SubmitForm(ctx, "test.ext", "test.ext.badform", "", json.RawMessage(`{}`)); err == nil {
		t.Error("a submit to a form that wasn't drawn")
	}
}

func TestInitializeListsViews(t *testing.T) {
	f := setup(t, nil)
	if _, err := f.h.Resolve(context.Background(), "test.ext", "test.ext.items", ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "initialize in the log", func() bool { return strings.Contains(f.log(t), "initialize {") })
	log := f.log(t)
	for _, want := range []string{`"views":["tree","list","table","form","chart","message"]`, `"charts":["line","bar","area"]`} {
		if !strings.Contains(log, want) {
			t.Errorf("initialize lacks %s:\n%s", want, log)
		}
	}
}

func TestFormCache(t *testing.T) {
	var c formCache
	for i := 0; i < maxFormCache+10; i++ {
		c.put(fmt.Sprint(i), []FormField{{}})
	}
	if len(c.m) != maxFormCache {
		t.Errorf("%d forms remembered", len(c.m))
	}
	c.put("x", []FormField{{Required: true}})
	if f, ok := c.get("x"); !ok || !f[0].Required {
		t.Error("the last form is not remembered")
	}
	c.put("x", nil)
	if _, ok := c.get("x"); ok {
		t.Error("a form that is gone is remembered")
	}
}
