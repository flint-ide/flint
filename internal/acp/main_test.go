package acp

import (
	"os"
	"testing"

	"flint/internal/acp/acptest"
)

// TestMain lets the test binary double as the fake agent (see acptest).
func TestMain(m *testing.M) {
	acptest.RunIfAgent()
	os.Exit(m.Run())
}
