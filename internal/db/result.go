package db

import (
	"database/sql/driver"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"time"
	"unicode/utf8"
)

// Column describes one result column.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// QueryResult is a fully materialised (row-limited) result set.
type QueryResult struct {
	Columns   []Column `json:"columns"`
	Rows      [][]any  `json:"rows"`
	Truncated bool     `json:"truncated"` // more rows existed beyond the limit
	Affected  int64    `json:"affected"`  // rows affected by DML, when the driver reports it
	Command   string   `json:"command"`   // e.g. "SELECT 3", "INSERT 0 1"
	ElapsedMs int64    `json:"elapsedMs"`

	// TableOID is the one table every table-backed column was read from (Postgres only,
	// 0 otherwise); Attnums maps each result column to its attribute number there (0 =
	// expression or repeated column). The grid offers editing for such results.
	TableOID uint32 `json:"tableOid,omitempty"`
	Attnums  []int  `json:"attnums,omitempty"`
	// EditBlocked says why a Postgres result can't be edited (several tables, …).
	EditBlocked string `json:"editBlocked,omitempty"`
}

// Table is a schema-introspection entry used for autocomplete and the schema tree.
type Table struct {
	Schema  string   `json:"schema"`
	Name    string   `json:"name"`
	Kind    string   `json:"kind"` // "table", "view", ...
	Columns []Column `json:"columns"`
}

// maxSafeInt is the largest integer a JS number represents exactly.
const maxSafeInt = 1<<53 - 1

const timeLayout = "2006-01-02 15:04:05.999999999 Z07:00"

// normalize converts driver values into JSON-friendly values that survive the trip to JS
// without losing precision (big ints, decimals) or failing to encode (NaN, binary).
func normalize(v any) any {
	if v == nil {
		return nil
	}
	// Drivers scan into (possibly nil) pointers. A nil *T whose String/Value method has a
	// value receiver would panic below, and *time.Time would hit the Stringer case, so
	// plain values are dereferenced first; other pointers keep their pointer methods.
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		if elem := rv.Elem(); isPlain(elem) {
			return normalize(elem.Interface())
		}
	}
	switch x := v.(type) {
	case string, bool:
		return x
	case time.Time:
		return x.Format(timeLayout)
	case []byte:
		return bytesValue(x)
	case [16]byte:
		return formatUUID(x)
	case int64:
		return safeInt(x)
	case int:
		return safeInt(int64(x))
	case uint64:
		if x > maxSafeInt {
			return fmt.Sprint(x)
		}
		return x
	case float64:
		return safeFloat(x)
	case float32:
		return safeFloat(float64(x))
	case *big.Int:
		return x.String()
	case driver.Valuer:
		val, err := x.Value()
		if err != nil {
			return fmt.Sprint(x)
		}
		if _, nested := val.(driver.Valuer); nested {
			return fmt.Sprint(val) // driver.Value must be a primitive; don't recurse forever
		}
		return normalize(val)
	case fmt.Stringer:
		return x.String()
	case error:
		return x.Error()
	}
	return normalizeReflect(reflect.ValueOf(v))
}

// isPlain reports whether a pointed-to value should be normalized by value rather than
// through methods on its pointer. Structs (errors, decimals, pgtype values) often
// implement interfaces on the pointer, so only time.Time among them is unwrapped.
func isPlain(rv reflect.Value) bool {
	return rv.Kind() != reflect.Struct || rv.Type() == reflect.TypeFor[time.Time]()
}

func normalizeReflect(rv reflect.Value) any {
	switch rv.Kind() {
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return normalize(rv.Elem().Interface())
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return nil
		}
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = normalize(rv.Index(i).Interface())
		}
		return out
	case reflect.Map:
		if rv.IsNil() {
			return nil
		}
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[fmt.Sprint(normalize(iter.Key().Interface()))] = normalize(iter.Value().Interface())
		}
		return out
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return safeInt(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return normalize(rv.Uint())
	case reflect.Float32, reflect.Float64:
		return safeFloat(rv.Float())
	case reflect.String:
		return rv.String()
	case reflect.Bool:
		return rv.Bool()
	}
	return fmt.Sprintf("%v", rv.Interface())
}

func safeInt(x int64) any {
	if x > maxSafeInt || x < -maxSafeInt {
		return fmt.Sprint(x)
	}
	return x
}

func safeFloat(f float64) any {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Sprint(f)
	}
	return f
}

func bytesValue(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	return `\x` + hex.EncodeToString(b)
}

func formatUUID(u [16]byte) string {
	h := hex.EncodeToString(u[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
