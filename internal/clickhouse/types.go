package clickhouse

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // zone names in DateTime('Europe/Warsaw') must resolve on systems without zoneinfo
)

// maxSafeInt is the largest integer a JS number represents exactly.
const maxSafeInt = 1<<53 - 1

// timeLayout is how date-times are shown: the column's zone offset is part of the value.
const timeLayout = "2006-01-02 15:04:05.999999999 Z07:00"

// chType is a parsed ClickHouse type name: Array(Nullable(UInt64)) is {Array, [{Nullable, [{UInt64}]}]}.
type chType struct {
	name string
	// args are the raw top-level arguments; element types are parsed lazily by sub.
	args []string
}

func parseType(s string) chType {
	s = strings.TrimSpace(s)
	open := strings.IndexByte(s, '(')
	if open < 0 || !strings.HasSuffix(s, ")") {
		return chType{name: s}
	}
	return chType{name: s[:open], args: splitArgs(s[open+1 : len(s)-1])}
}

// sub parses argument i as a type.
func (t chType) sub(i int) chType {
	if i >= len(t.args) {
		return chType{}
	}
	return parseType(t.args[i])
}

// splitArgs splits a comma-separated argument list at depth zero, ignoring commas inside
// parentheses and quotes (enum values, time zone names).
func splitArgs(s string) []string {
	var out []string
	depth, start := 0, 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '\'' || c == '`' || c == '"':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			out = append(out, strings.TrimSpace(s[start:i]))
			start = i + 1
		}
	}
	if rest := strings.TrimSpace(s[start:]); rest != "" {
		out = append(out, rest)
	}
	return out
}

// tupleElement splits a Tuple argument into its element name ("" when unnamed) and type.
// A name is a word followed by a space before any parenthesis: "a UInt8", "b Array(String)".
func tupleElement(arg string) (name string, typ chType) {
	sp := strings.IndexByte(arg, ' ')
	paren := strings.IndexByte(arg, '(')
	if sp > 0 && (paren < 0 || sp < paren) {
		return strings.Trim(arg[:sp], "`\""), parseType(arg[sp+1:])
	}
	return "", parseType(arg)
}

// converter turns decoded JSON cells into values the UI shows without losing precision:
// 64-bit integers beyond JS's exact range and decimals stay strings, date-times carry
// their zone offset, containers are converted element by element.
type converter struct {
	serverZone *time.Location // zone of date-times without one of their own; nil keeps them as sent
}

func (cv converter) convert(v any, t chType) any {
	if v == nil {
		return nil
	}
	switch t.name {
	case "Nullable", "LowCardinality":
		return cv.convert(v, t.sub(0))
	case "SimpleAggregateFunction":
		return cv.convert(v, t.sub(1))
	case "Array":
		items, ok := v.([]any)
		if !ok {
			return plain(v)
		}
		elem := t.sub(0)
		out := make([]any, len(items))
		for i, it := range items {
			out[i] = cv.convert(it, elem)
		}
		return out
	case "Map":
		obj, ok := v.(map[string]any)
		if !ok {
			return plain(v)
		}
		val := t.sub(1)
		out := make(map[string]any, len(obj))
		for k, it := range obj {
			out[k] = cv.convert(it, val)
		}
		return out
	case "Tuple":
		return cv.tuple(v, t)
	case "Int64", "UInt64":
		return safeInteger(v)
	case "Int8", "Int16", "Int32", "UInt8", "UInt16", "UInt32":
		return plain(v)
	case "Float32", "Float64":
		return plain(v)
	case "DateTime", "DateTime64":
		return cv.dateTime(v, t)
	}
	if strings.HasPrefix(t.name, "Decimal") {
		return padDecimal(v, decimalScale(t))
	}
	return plain(v)
}

func (cv converter) tuple(v any, t chType) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, it := range x {
			_, et := tupleElement(argAt(t, i))
			out[i] = cv.convert(it, et)
		}
		return out
	case map[string]any: // named tuples are objects
		types := make(map[string]chType, len(t.args))
		for _, a := range t.args {
			if n, et := tupleElement(a); n != "" {
				types[n] = et
			}
		}
		out := make(map[string]any, len(x))
		for k, it := range x {
			out[k] = cv.convert(it, types[k])
		}
		return out
	}
	return plain(v)
}

func argAt(t chType, i int) string {
	if i < len(t.args) {
		return t.args[i]
	}
	return ""
}

// plain turns json.Number leaves into int64 / float64 (strings when they don't fit)
// and keeps everything else.
func plain(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return safeInt(i)
		}
		if f, err := x.Float64(); err == nil {
			return f
		}
		return x.String()
	case []any:
		out := make([]any, len(x))
		for i, it := range x {
			out[i] = plain(it)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, it := range x {
			out[k] = plain(it)
		}
		return out
	}
	return v
}

func safeInt(i int64) any {
	if i > maxSafeInt || i < -maxSafeInt {
		return strconv.FormatInt(i, 10)
	}
	return i
}

// safeInteger reads a (quoted) 64-bit integer: a number while JS holds it exactly, else the digits.
func safeInteger(v any) any {
	s, ok := v.(string)
	if n, isNum := v.(json.Number); isNum {
		s, ok = n.String(), true
	}
	if !ok {
		return plain(v)
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return safeInt(i)
	}
	if u, err := strconv.ParseUint(s, 10, 64); err == nil && u <= maxSafeInt {
		return int64(u)
	}
	return s
}

// decimalScale reads S from Decimal(P, S) or Decimal32(S) … Decimal256(S).
func decimalScale(t chType) int {
	if len(t.args) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(t.args[len(t.args)-1])
	return n
}

// padDecimal restores trailing zeros the JSON output drops: 1.5 of Decimal(18, 2) is 1.50.
func padDecimal(v any, scale int) any {
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case json.Number:
		s = x.String()
	default:
		return plain(v)
	}
	if scale <= 0 || strings.ContainsAny(s, "eEnN") { // exponent, nan, inf
		return s
	}
	whole, frac, _ := strings.Cut(s, ".")
	if len(frac) < scale {
		frac += strings.Repeat("0", scale-len(frac))
	}
	return whole + "." + frac
}

// dateTime shows a DateTime / DateTime64 with the offset of its column's zone (or the
// server's); a value or zone that can't be parsed is shown as the server sent it.
func (cv converter) dateTime(v any, t chType) any {
	s, ok := v.(string)
	if !ok {
		return plain(v)
	}
	loc := cv.serverZone
	if zone := zoneArg(t); zone != "" {
		if l, err := time.LoadLocation(zone); err == nil {
			loc = l
		}
	}
	if loc == nil {
		return s
	}
	ts, err := time.ParseInLocation("2006-01-02 15:04:05", s, loc)
	if err != nil {
		return s
	}
	return ts.Format(timeLayout)
}

// zoneArg returns the zone name of DateTime('Z') or DateTime64(P, 'Z').
func zoneArg(t chType) string {
	for _, a := range t.args {
		if len(a) >= 2 && a[0] == '\'' && a[len(a)-1] == '\'' {
			return a[1 : len(a)-1]
		}
	}
	return ""
}
