package manifest

import (
	"strings"
	"testing"
)

func TestRunEnv(t *testing.T) {
	ok := `{"contract":1,"id":"flint.kubernetes","name":"K","version":"0.1.0","run":{"command":"k","env":["KUBECONFIG","AWS_CONFIG_FILE"]}}`
	m, err := Parse([]byte(ok))
	if err != nil || len(m.Run.Env) != 2 {
		t.Fatalf("Parse() = %+v, %v", m, err)
	}
	for name, env := range map[string]string{
		"path":      `["PATH"]`,
		"flint":     `["FLINT_EXTENSION_ID"]`,
		"lower":     `["kubeconfig"]`,
		"twice":     `["A","A"]`,
		"assign":    `["A=B"]`,
		"too many":  `["A0","A1","A2","A3","A4","A5","A6","A7","A8","A9","B0","B1","B2","B3","B4","B5","B6","B7","B8","B9","C0","C1","C2","C3","C4","C5","C6","C7","C8","C9","D0"]`,
		"preloader": `["DYLD_INSERT_LIBRARIES"]`,
	} {
		src := strings.Replace(ok, `["KUBECONFIG","AWS_CONFIG_FILE"]`, env, 1)
		if _, err := Parse([]byte(src)); err == nil || !strings.Contains(err.Error(), "run.env") {
			t.Errorf("%s: Parse() = %v", name, err)
		}
	}
}
