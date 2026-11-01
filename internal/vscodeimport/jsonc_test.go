package vscodeimport

import (
	"encoding/json"
	"testing"
)

func TestStripJSONC(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"plain":                {`{"a":1}`, `{"a":1}`},
		"line comment":         {"{\"a\":1 // one\n}", "{\"a\":1 \n}"},
		"block comment":        {`{/* x */"a":1}`, `{"a":1}`},
		"multiline block":      {"{/* x\n y */\"a\":1}", "{\n\"a\":1}"},
		"trailing comma obj":   {`{"a":1,}`, `{"a":1}`},
		"trailing comma arr":   {`[1,2 , ]`, `[1,2  ]`},
		"comma before comment": {"[1, // c\n]", "[1 \n]"},
		"slashes in string":    {`{"url":"http://x/*y*/"}`, `{"url":"http://x/*y*/"}`},
		"escaped quote":        {`{"a":"say \"//hi\"",}`, `{"a":"say \"//hi\""}`},
		"comma in string":      {`{"a":",}"}`, `{"a":",}"}`},
		"bom":                  {"\ufeff{}", "{}"},
		"empty":                {"", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := StripJSONC([]byte(c.in))
			if err != nil {
				t.Fatalf("StripJSONC(%q) error = %v", c.in, err)
			}
			if string(got) != c.want {
				t.Errorf("StripJSONC(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestStripJSONCErrors(t *testing.T) {
	for _, in := range []string{`{"a":"open`, `{/* never closed`} {
		if _, err := StripJSONC([]byte(in)); err == nil {
			t.Errorf("StripJSONC(%q) accepted malformed input", in)
		}
	}
}

func TestStripJSONCProducesValidJSON(t *testing.T) {
	in := `// VS Code settings
{
	"editor.fontSize": 14, // big
	/* theme */
	"workbench.colorTheme": "Monokai",
	"files.exclude": {
		"**/.git": true,
	},
}`
	out, err := StripJSONC([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, out)
	}
	if v["editor.fontSize"] != 14.0 || v["workbench.colorTheme"] != "Monokai" {
		t.Errorf("decoded %v", v)
	}
}
