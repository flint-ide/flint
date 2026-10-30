package db

import (
	"database/sql/driver"
	"errors"
	"math"
	"math/big"
	"reflect"
	"testing"
	"time"
)

type cents int64

func (c cents) Value() (driver.Value, error) { return int64(c), nil }

type badValuer struct{}

func (badValuer) Value() (driver.Value, error) { return nil, errors.New("nope") }
func (badValuer) String() string               { return "badValuer" }

type nestedValuer struct{}

func (nestedValuer) Value() (driver.Value, error) { return cents(5), nil }

type stringer struct{ s string }

func (s stringer) String() string { return "S:" + s.s }

type label string

type point struct{ X, Y int }

func TestNormalize(t *testing.T) {
	ts := time.Date(2024, 1, 2, 3, 4, 5, 123000000, time.UTC)
	tsWarsaw := time.Date(2024, 7, 1, 12, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	i64 := int64(42)
	pi64 := &i64
	big1 := new(big.Int).Lsh(big.NewInt(1), 100)
	uuid := [16]byte{0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}

	tests := []struct {
		name string
		in   any
		want any
	}{
		{"nil", nil, nil},
		{"string", "zażółć", "zażółć"},
		{"bool", true, true},
		{"time utc", ts, "2024-01-02 03:04:05.123 Z"},
		{"time offset", tsWarsaw, "2024-07-01 12:00:00 +02:00"},
		{"bytes utf8", []byte("héllo"), "héllo"},
		{"bytes invalid utf8", []byte{0xde, 0xad, 0xbe, 0xef}, `\xdeadbeef`},
		{"bytes empty", []byte{}, ""},
		{"uuid", uuid, "12345678-9abc-def0-0123-456789abcdef"},
		{"int64 small", int64(-7), int64(-7)},
		{"int64 max safe", int64(maxSafeInt), int64(maxSafeInt)},
		{"int64 above 2^53", int64(1<<53 + 1), "9007199254740993"},
		{"int64 below -2^53", int64(-(1<<53 + 1)), "-9007199254740993"},
		{"int", 12, int64(12)},
		{"int32 via reflect", int32(-3), int64(-3)},
		{"int8 via reflect", int8(9), int64(9)},
		{"uint64 small", uint64(10), uint64(10)},
		{"uint64 huge", uint64(math.MaxUint64), "18446744073709551615"},
		{"uint16 via reflect", uint16(65535), uint64(65535)},
		{"uint via reflect big", uint(1 << 60), "1152921504606846976"},
		{"float64", 1.5, 1.5},
		{"float32", float32(0.25), 0.25},
		{"NaN", math.NaN(), "NaN"},
		{"+Inf", math.Inf(1), "+Inf"},
		{"-Inf float32", float32(math.Inf(-1)), "-Inf"},
		{"big.Int", big1, "1267650600228229401496703205376"},
		{"pointer", pi64, int64(42)},
		{"double pointer", &pi64, int64(42)},
		{"nil pointer", (*int64)(nil), nil},
		{"nil double pointer", (**int64)(nil), nil},
		{"pointer to nil pointer", func() any { var p *string; return &p }(), nil},
		{"slice", []int32{1, 2}, []any{int64(1), int64(2)}},
		{"nil slice", []string(nil), nil},
		{"array", [2]string{"a", "b"}, []any{"a", "b"}},
		{"nested slice", [][]any{{int64(1 << 60)}, {nil}}, []any{[]any{"1152921504606846976"}, []any{nil}}},
		{"map string keys", map[string]float64{"a": math.NaN()}, map[string]any{"a": "NaN"}},
		{"map int keys", map[int]string{1: "x"}, map[string]any{"1": "x"}},
		{"map uuid keys", map[[16]byte]bool{uuid: true}, map[string]any{"12345678-9abc-def0-0123-456789abcdef": true}},
		{"nil map", map[string]int(nil), nil},
		{"valuer", cents(250), int64(250)},
		{"valuer error falls back to Sprint", badValuer{}, "badValuer"},
		{"nested valuer not recursed", nestedValuer{}, "5"},
		{"stringer", stringer{"x"}, "S:x"},
		{"error", errors.New("boom"), "boom"},
		{"named string", label("tag"), "tag"},
		{"struct fallback", point{1, 2}, "{1 2}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalize(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("normalize(%#v) = %#v (%T), want %#v (%T)", tt.in, got, got, tt.want, tt.want)
			}
		})
	}
}

// ClickHouse scans every column into reflect.New(ScanType()), so normalize receives
// *time.Time for DateTime and **time.Time for Nullable(DateTime).
func TestNormalizeTimePointer(t *testing.T) {
	ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	if got, want := normalize(&ts), "2024-01-02 03:04:05 Z"; got != want {
		t.Errorf("normalize(*time.Time) = %#v, want %#v", got, want)
	}
}

func TestNormalizeNilPointerToValueReceiverStringer(t *testing.T) {
	var nullable *time.Time
	if got := normalize(&nullable); got != nil {
		t.Errorf("normalize(**time.Time -> nil) = %#v, want nil", got)
	}
}

func TestNormalizeNilBigInt(t *testing.T) {
	if got := normalize((*big.Int)(nil)); got != nil {
		t.Errorf("normalize(nil *big.Int) = %#v, want nil", got)
	}
}
