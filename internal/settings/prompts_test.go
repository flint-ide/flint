package settings

import "testing"

func TestPromptsDefaults(t *testing.T) {
	if !Default().Prompts.Builtins {
		t.Fatal("built-in prompts are off by default")
	}
	s, err := decode([]byte(`{"prompts": {}}`))
	if err != nil || !s.Prompts.Builtins {
		t.Fatalf("missing key lost the default: %+v %v", s.Prompts, err)
	}
	s, err = decode([]byte(`{"prompts": {"builtins": false}}`))
	if err != nil || s.Prompts.Builtins {
		t.Fatalf("builtins not switched off: %+v %v", s.Prompts, err)
	}
	s, err = decode([]byte(`{"prompts": {"builtins": "no"}}`))
	if err != nil || !s.Prompts.Builtins {
		t.Fatalf("a wrong type did not fall back to the default: %+v %v", s.Prompts, err)
	}
}
