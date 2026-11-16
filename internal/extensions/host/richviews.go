package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"sync"
	"time"

	"flint/internal/extensions/manifest"
)

// The table, form and chart views (docs/design/extensions.md, "Component
// catalog"), for panels and tab views. Like a tree's items, what the
// extension answers view/resolve with is untrusted: it is decoded here, cut
// to the limits below, stripped of control characters and typed (a cell by
// its column, a chart point by its axis), so the frontend draws only values
// Go made. Text is plain text; colors are never the extension's.

// Limits on a table, form or chart.
const (
	maxColumns     = 30
	maxColumnTitle = 100
	maxBadges      = 50
	maxFormFields  = 50
	maxFormText    = 500
	maxFieldLength = 4096 // of a text field, the settings schema's limit
	maxFormValues  = 1 << 20
	maxFormCache   = 64
	maxSeries      = 10
	maxPoints      = 2000
	maxCategory    = 200
)

// ViewKinds are the view kinds this Flint draws from data (initialize's
// capabilities.views) and ChartTypes the charts (capabilities.charts).
var (
	ViewKinds  = []string{"tree", "list", "table", "form", "chart", "message"}
	ChartTypes = []string{"line", "bar", "area"}
)

var severities = []string{"info", "ok", "warning", "error"}

// Table is a table view: typed columns and rows the frontend sorts.
type Table struct {
	Columns []Column `json:"columns"`
	Rows    []Row    `json:"rows"`
	// Sort is the order the table is first shown in.
	Sort *TableSort `json:"sort,omitempty"`
	// Actions are the context menu of every row that has none of its own.
	Actions []Action `json:"actions,omitempty"`
}

// Column types its cells.
type Column struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Type is "text", "number", "date" or "badge".
	Type string `json:"type"`
	// Align is "start", "center" or "end".
	Align string `json:"align"`
	// Badges maps a badge column's values to a severity (info, ok, warning, error).
	Badges map[string]string `json:"badges,omitempty"`
}

// Row is a table row; Cells are in the order of the columns: a string (text,
// badge, an ISO date), a number, or null for a missing or mistyped value.
type Row struct {
	ID      string      `json:"id"`
	Cells   []any       `json:"cells"`
	Actions []Action    `json:"actions,omitempty"`
	Command *CommandRef `json:"command,omitempty"`
}

// TableSort names a column to sort by.
type TableSort struct {
	Column     string `json:"column"`
	Descending bool   `json:"descending,omitempty"`
}

// Form is a form view: fields with the settings schema's rules and a submit button.
type Form struct {
	ID          string      `json:"id,omitempty"`
	Description string      `json:"description,omitempty"`
	Fields      []FormField `json:"fields"`
	// Submit is the button's label.
	Submit string `json:"submit"`
}

// FormField is a field: a property of the settings schema subset (its key is
// the field's id), the value to show and how to draw it.
type FormField struct {
	Property manifest.Property `json:"property"`
	// Value is the current value, valid for the property; a secret field's
	// is always "".
	Value       any    `json:"value"`
	Multiline   bool   `json:"multiline,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
}

// FormResult is what a submit came to: nothing (accepted), or why not.
type FormResult struct {
	// Errors are messages for fields, by field id.
	Errors map[string]string `json:"errors,omitempty"`
	// Error is about the form as a whole.
	Error string `json:"error,omitempty"`
	// Message is shown after an accepted submit.
	Message string `json:"message,omitempty"`
}

// Chart is a chart view: up to maxSeries series of up to maxPoints points.
type Chart struct {
	// Type is "line", "bar" or "area".
	Type   string     `json:"type"`
	Title  string     `json:"title,omitempty"`
	X      ChartXAxis `json:"x"`
	Y      ChartYAxis `json:"y"`
	Series []Series   `json:"series"`
}

// ChartXAxis types the points' x values.
type ChartXAxis struct {
	// Type is "number", "date" or "category".
	Type  string `json:"type"`
	Label string `json:"label,omitempty"`
}

// ChartYAxis labels and bounds the y axis.
type ChartYAxis struct {
	Label string   `json:"label,omitempty"`
	Min   *float64 `json:"min,omitempty"`
	Max   *float64 `json:"max,omitempty"`
}

// Series is one line, area or group of bars.
type Series struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	Points []Point `json:"points"`
}

// Point is a value: X is a number (a date is milliseconds since 1970 UTC) or,
// on a category axis, a string.
type Point struct {
	X any     `json:"x"`
	Y float64 `json:"y"`
}

func messageView(format string, args ...any) View {
	return View{Kind: "message", Items: []Item{}, Text: cleanText(fmt.Sprintf(format, args...), maxLabel)}
}

// cleanResolved makes a view/resolve answer safe to draw as a view of kind
// want. A form's fields are returned too, for the checks of its submit.
func cleanResolved(raw json.RawMessage, want string, m *manifest.Manifest) (View, []FormField) {
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return messageView("The extension's answer is not a view."), nil
	}
	if head.Kind == "message" || head.Kind != want || want == "tree" || want == "list" {
		var v View
		if err := json.Unmarshal(raw, &v); err != nil {
			return messageView("The extension's answer is not a %s view.", want), nil
		}
		return cleanView(v, manifest.Panel{View: want}, m), nil
	}
	out := View{Kind: want, Items: []Item{}}
	var err error
	var fields []FormField
	switch want {
	case "table":
		out.Table, out.Truncated, err = cleanTable(raw, m)
	case "form":
		out.Form, err = cleanForm(raw)
		if out.Form != nil {
			fields = out.Form.Fields
		}
	case "chart":
		out.Chart, out.Truncated, err = cleanChart(raw)
	default:
		err = fmt.Errorf("this Flint doesn't draw %q views", want)
	}
	if err != nil {
		return messageView("The extension sent a %s Flint can't draw: %v", want, err), nil
	}
	return out, fields
}

func cleanTable(raw json.RawMessage, m *manifest.Manifest) (*Table, bool, error) {
	var in struct {
		Columns []Column `json:"columns"`
		Rows    []struct {
			ID      string                     `json:"id"`
			Cells   map[string]json.RawMessage `json:"cells"`
			Actions []Action                   `json:"actions"`
			Command *CommandRef                `json:"command"`
		} `json:"rows"`
		Sort    *TableSort `json:"sort"`
		Actions []Action   `json:"actions"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, false, errors.New("its columns or rows are not as the protocol describes them")
	}
	t := &Table{Columns: []Column{}, Rows: []Row{}, Actions: cleanActions(in.Actions)}
	var keys []string // the columns' ids as the extension wrote them, for its cells
	seen := map[string]bool{}
	for _, c := range in.Columns {
		if len(t.Columns) == maxColumns {
			break
		}
		key := c.ID
		c.ID = cleanText(c.ID, 64)
		if c.ID == "" || seen[c.ID] {
			continue
		}
		seen[c.ID] = true
		if c.Title = cleanText(c.Title, maxColumnTitle); c.Title == "" {
			c.Title = c.ID
		}
		if !slices.Contains([]string{"text", "number", "date", "badge"}, c.Type) {
			c.Type = "text"
		}
		if !slices.Contains([]string{"start", "center", "end"}, c.Align) {
			c.Align = "start"
			if c.Type == "number" {
				c.Align = "end"
			}
		}
		c.Badges = cleanBadges(c)
		t.Columns = append(t.Columns, c)
		keys = append(keys, key)
	}
	if len(t.Columns) == 0 {
		return nil, false, errors.New("it has no columns")
	}
	truncated := false
	rows := map[string]bool{}
	for _, r := range in.Rows {
		if len(t.Rows) == maxItems {
			truncated = true
			break
		}
		id := cleanText(r.ID, maxItemID)
		if id == "" || rows[id] {
			continue
		}
		rows[id] = true
		row := Row{ID: id, Cells: make([]any, len(t.Columns)), Actions: cleanActions(r.Actions), Command: cleanCommand(r.Command, m)}
		for i, c := range t.Columns {
			row.Cells[i] = cleanCell(c.Type, r.Cells[keys[i]])
		}
		t.Rows = append(t.Rows, row)
	}
	if in.Sort != nil {
		if col := cleanText(in.Sort.Column, 64); seen[col] {
			t.Sort = &TableSort{Column: col, Descending: in.Sort.Descending}
		}
	}
	return t, truncated, nil
}

func cleanBadges(c Column) map[string]string {
	if c.Type != "badge" {
		return nil
	}
	out := map[string]string{}
	// In key order, so which ones a too long list keeps doesn't change between answers.
	keys := make([]string, 0, len(c.Badges))
	for k := range c.Badges {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if len(out) == maxBadges {
			break
		}
		if value := cleanText(k, maxColumnTitle); value != "" && slices.Contains(severities, c.Badges[k]) {
			out[value] = c.Badges[k]
		}
	}
	return out
}

// cleanCell types a cell by its column: nil when it is missing or not a
// value of that type.
func cleanCell(kind string, raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	switch kind {
	case "number":
		if n, ok := v.(float64); ok && !math.IsNaN(n) && !math.IsInf(n, 0) {
			return n
		}
		return nil
	case "date":
		s, _ := v.(string)
		if _, ok := parseDate(s); ok {
			return s
		}
		return nil
	}
	switch x := v.(type) {
	case string:
		return cleanText(x, maxLabel)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return nil
}

// parseDate reads an ISO 8601 date or date and time (without a zone: UTC).
func parseDate(s string) (time.Time, bool) {
	if len(s) < 10 || len(s) > 40 {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// schemaKeywords are the keywords of a form field that are the settings
// schema's; the others are the form's own or, unknown, ignored.
var schemaKeywords = []string{"type", "title", "description", "default", "enum", "minimum", "maximum", "maxLength", "pattern", "format", "items"}

func cleanForm(raw json.RawMessage) (*Form, error) {
	var in struct {
		ID          string                       `json:"id"`
		Description string                       `json:"description"`
		Fields      []map[string]json.RawMessage `json:"fields"`
		Submit      string                       `json:"submit"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, errors.New("its fields are not as the protocol describes them")
	}
	if len(in.Fields) > maxFormFields {
		return nil, fmt.Errorf("it has more than %d fields", maxFormFields)
	}
	f := &Form{ID: cleanText(in.ID, 64), Description: cleanText(in.Description, maxLabel), Fields: []FormField{}, Submit: cleanText(in.Submit, 60)}
	if f.Submit == "" {
		f.Submit = "Submit"
	}
	seen := map[string]bool{}
	for i, rawField := range in.Fields {
		field, err := cleanField(rawField)
		if err != nil {
			return nil, fmt.Errorf("field %d: %w", i+1, err)
		}
		if seen[field.Property.Key] {
			return nil, fmt.Errorf("field %s is there twice", field.Property.Key)
		}
		seen[field.Property.Key] = true
		f.Fields = append(f.Fields, field)
	}
	return f, nil
}

func cleanField(raw map[string]json.RawMessage) (FormField, error) {
	var id string
	_ = json.Unmarshal(raw["id"], &id)
	schema := map[string]json.RawMessage{}
	for _, k := range schemaKeywords {
		if v, ok := raw[k]; ok {
			schema[k] = v
		}
	}
	// A longer text than a setting holds is cut to that, not refused.
	var length float64
	if json.Unmarshal(schema["maxLength"], &length) == nil && length > maxFieldLength {
		schema["maxLength"] = json.RawMessage(strconv.Itoa(maxFieldLength))
	}
	data, _ := json.Marshal(schema)
	p, err := manifest.ParseProperty(id, data)
	if err != nil {
		return FormField{}, err
	}
	f := FormField{Property: p, Value: p.Default}
	flag := func(key string) bool {
		var b bool
		_ = json.Unmarshal(raw[key], &b)
		return b
	}
	text := p.Type == "string" && len(p.Enum) == 0
	f.Secret = flag("secret") && text
	f.Multiline = flag("multiline") && text && !f.Secret
	f.Required = flag("required")
	var placeholder string
	_ = json.Unmarshal(raw["placeholder"], &placeholder)
	f.Placeholder = cleanText(placeholder, 100)
	if f.Secret {
		// A secret is typed, never shown: no value and no default reach the page.
		f.Property.Default, f.Value = "", ""
		return f, nil
	}
	if v, ok := raw["value"]; ok {
		var value any
		if json.Unmarshal(v, &value) == nil && p.Check(value) == nil {
			f.Value = value
		}
	}
	return f, nil
}

// checkField reports why a submitted value is refused ("" when it isn't).
func checkField(f *FormField, v any) string {
	if err := f.Property.Check(v); err != nil {
		return cleanText(err.Error(), maxFormText)
	}
	if f.Required {
		switch x := v.(type) {
		case string:
			if x == "" {
				return "is required"
			}
		case []any:
			if len(x) == 0 {
				return "is required"
			}
		}
	}
	return ""
}

func cleanChart(raw json.RawMessage) (*Chart, bool, error) {
	var in struct {
		Type   string     `json:"type"`
		Title  string     `json:"title"`
		X      ChartXAxis `json:"x"`
		Y      ChartYAxis `json:"y"`
		Series []struct {
			ID     string              `json:"id"`
			Label  string              `json:"label"`
			Points [][]json.RawMessage `json:"points"`
		} `json:"series"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, false, errors.New("its series are not as the protocol describes them")
	}
	if !slices.Contains(ChartTypes, in.Type) {
		return nil, false, fmt.Errorf("chart type %q is not one of line, bar, area", cleanText(in.Type, 20))
	}
	c := &Chart{Type: in.Type, Title: cleanText(in.Title, maxColumnTitle), Series: []Series{}}
	c.X = ChartXAxis{Type: in.X.Type, Label: cleanText(in.X.Label, maxColumnTitle)}
	if !slices.Contains([]string{"number", "date", "category"}, c.X.Type) {
		c.X.Type = "category"
	}
	c.Y = ChartYAxis{Label: cleanText(in.Y.Label, maxColumnTitle), Min: finite(in.Y.Min), Max: finite(in.Y.Max)}
	if c.Y.Min != nil && c.Y.Max != nil && *c.Y.Min >= *c.Y.Max {
		c.Y.Min, c.Y.Max = nil, nil
	}
	truncated := false
	seen := map[string]bool{}
	for i, s := range in.Series {
		if len(c.Series) == maxSeries {
			truncated = true
			break
		}
		out := Series{ID: cleanText(s.ID, 64), Label: cleanText(s.Label, maxColumnTitle), Points: []Point{}}
		if out.ID == "" {
			out.ID = strconv.Itoa(i + 1)
		}
		if seen[out.ID] {
			continue
		}
		seen[out.ID] = true
		if out.Label == "" {
			out.Label = out.ID
		}
		for _, p := range s.Points {
			if len(out.Points) == maxPoints {
				truncated = true
				break
			}
			if pt, ok := cleanPoint(c.X.Type, p); ok {
				out.Points = append(out.Points, pt)
			}
		}
		c.Series = append(c.Series, out)
	}
	return c, truncated, nil
}

func finite(f *float64) *float64 {
	if f == nil || math.IsNaN(*f) || math.IsInf(*f, 0) {
		return nil
	}
	return f
}

// cleanPoint types a point [x, y] by the x axis; a point that isn't one is dropped.
func cleanPoint(axis string, p []json.RawMessage) (Point, bool) {
	if len(p) != 2 {
		return Point{}, false
	}
	var y float64
	if err := json.Unmarshal(p[1], &y); err != nil || string(p[1]) == "null" || math.IsNaN(y) || math.IsInf(y, 0) {
		return Point{}, false
	}
	var x any
	if err := json.Unmarshal(p[0], &x); err != nil {
		return Point{}, false
	}
	switch axis {
	case "number":
		if n, ok := x.(float64); ok && !math.IsNaN(n) && !math.IsInf(n, 0) {
			return Point{X: n, Y: y}, true
		}
	case "date":
		s, _ := x.(string)
		if t, ok := parseDate(s); ok {
			return Point{X: float64(t.UnixMilli()), Y: y}, true
		}
	default:
		switch v := x.(type) {
		case string:
			if s := cleanText(v, maxCategory); s != "" {
				return Point{X: s, Y: y}, true
			}
		case float64:
			return Point{X: strconv.FormatFloat(v, 'f', -1, 64), Y: y}, true
		}
	}
	return Point{}, false
}

// formCache remembers the fields of the forms last drawn, by view and tab
// key, so a submit is checked against the form the user saw.
type formCache struct {
	mu sync.Mutex
	m  map[string][]FormField
}

func (c *formCache) put(key string, fields []FormField) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if fields == nil {
		delete(c.m, key)
		return
	}
	if c.m == nil {
		c.m = map[string][]FormField{}
	}
	if _, ok := c.m[key]; !ok && len(c.m) >= maxFormCache {
		for k := range c.m { // any one: its tab resolves again before the next submit works
			delete(c.m, k)
			break
		}
	}
	c.m[key] = fields
}

func (c *formCache) get(key string) ([]FormField, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f, ok := c.m[key]
	return f, ok
}

// declarative finds a panel or tab view Flint draws from data and checks the
// tab key: "" for a panel, required for a tab view.
func (h *Host) declarative(id, view, key string) (*ext, string, string, error) {
	e, err := h.get(id)
	if err != nil {
		return nil, "", "", err
	}
	if e.m == nil {
		return nil, "", "", e.usable()
	}
	kind, tab, ok := e.m.ViewKind(view)
	switch {
	case !ok && key == "":
		return nil, "", "", fmt.Errorf("%s has no panel %q", e.name(), view)
	case !ok:
		return nil, "", "", fmt.Errorf("%s has no tab view %q", e.name(), view)
	case kind == "webview":
		return nil, "", "", fmt.Errorf("%s: %q is a webview", e.name(), view)
	case len(key) > maxTabKey:
		return nil, "", "", errors.New("the tab's key is too long")
	}
	key = cleanText(key, maxTabKey)
	if tab != (key != "") {
		if tab {
			return nil, "", "", fmt.Errorf("%s: a tab of %q needs a key", e.name(), view)
		}
		return nil, "", "", fmt.Errorf("%s: %q is a panel, not a tab view", e.name(), view)
	}
	return e, kind, key, nil
}

func viewParams(view, key string) map[string]any {
	params := map[string]any{"view": view}
	if key != "" {
		params["key"] = key
	}
	return params
}

// ResolveView asks for the content of a panel (key "") or of a tab view's
// tab (key, and the tab's saved data); parent expands a tree node.
func (h *Host) ResolveView(ctx context.Context, id, view, key string, data json.RawMessage, parent string) (View, error) {
	e, kind, key, err := h.declarative(id, view, key)
	if err != nil {
		return View{}, err
	}
	if len(data) > maxTabData {
		return View{}, fmt.Errorf("tab data is larger than %d KiB", maxTabData>>10)
	}
	params := viewParams(view, key)
	if len(data) > 0 && string(data) != "null" {
		params["data"] = data
	}
	if parent != "" {
		params["parent"] = parent
	}
	var raw json.RawMessage
	if err := e.call(ctx, h.o.ResolveTimeout, "view/resolve", params, &raw); err != nil {
		return View{}, err
	}
	v, fields := cleanResolved(raw, kind, e.m)
	if kind == "form" {
		e.forms.put(view+"\x00"+key, fields)
	}
	return v, nil
}

// ViewAction sends the choice of an action of a row or item (item "" for a
// message's button) in a panel or a tab.
func (h *Host) ViewAction(ctx context.Context, id, view, key, item, action string) error {
	e, _, key, err := h.declarative(id, view, key)
	if err != nil {
		return err
	}
	if len(item) > 4*maxItemID || len(action) > 256 {
		return errors.New("the action's ids are too long")
	}
	params := viewParams(view, key)
	params["item"], params["action"] = item, action
	return e.call(ctx, h.o.CallTimeout, "view/action", params, nil)
}

// SetViewVisible records that a declarative panel or tab was shown or
// hidden, like SetVisible.
func (h *Host) SetViewVisible(id, view, key string, visible bool) error {
	e, _, key, err := h.declarative(id, view, key)
	if err != nil {
		return err
	}
	e.setVisible(view, key, visible)
	return nil
}

// SubmitForm checks the values of a form view against the fields Flint last
// drew for it and, when they all pass, sends them to the extension
// (form/submit). The result holds the reasons a value was refused, by Flint
// or by the extension. values is a JSON object by field id; fields it lacks
// are sent with their defaults and keys that aren't fields are dropped.
func (h *Host) SubmitForm(ctx context.Context, id, view, key string, values json.RawMessage) (FormResult, error) {
	e, kind, key, err := h.declarative(id, view, key)
	if err != nil {
		return FormResult{}, err
	}
	if kind != "form" {
		return FormResult{}, fmt.Errorf("%s: %q is not a form", e.name(), view)
	}
	fields, ok := e.forms.get(view + "\x00" + key)
	if !ok {
		return FormResult{}, errors.New("the form has changed: refresh it and try again")
	}
	if len(values) > maxFormValues {
		return FormResult{}, errors.New("the form's values are too large")
	}
	var given map[string]any
	if err := json.Unmarshal(values, &given); err != nil {
		return FormResult{}, errors.New("the form's values must be an object")
	}
	checked := map[string]any{}
	refused := map[string]string{}
	for i := range fields {
		f := &fields[i]
		v, ok := given[f.Property.Key]
		if !ok {
			v = f.Property.Default
		}
		if why := checkField(f, v); why != "" {
			refused[f.Property.Key] = why
			continue
		}
		checked[f.Property.Key] = v
	}
	if len(refused) > 0 {
		return FormResult{Errors: refused}, nil
	}
	params := viewParams(view, key)
	params["values"] = checked
	var answer struct {
		Errors  map[string]string `json:"errors"`
		Error   string            `json:"error"`
		Message string            `json:"message"`
	}
	if err := e.call(ctx, h.o.CallTimeout, "form/submit", params, &answer); err != nil {
		return FormResult{}, err
	}
	out := FormResult{Error: cleanText(answer.Error, maxFormText), Message: cleanText(answer.Message, maxFormText)}
	for i := range fields {
		if why := cleanText(answer.Errors[fields[i].Property.Key], maxFormText); why != "" {
			if out.Errors == nil {
				out.Errors = map[string]string{}
			}
			out.Errors[fields[i].Property.Key] = why
		}
	}
	return out, nil
}
