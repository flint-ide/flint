package terminal

import (
	"strings"
	"testing"
)

func TestCheckEnvVar(t *testing.T) {
	for _, ok := range [][2]string{{"AWS_PROFILE", "prod"}, {"_X", ""}, {"A1", "a b=c"}} {
		if err := CheckEnvVar(ok[0], ok[1]); err != nil {
			t.Errorf("CheckEnvVar(%q, %q) = %v", ok[0], ok[1], err)
		}
	}
	for _, bad := range [][2]string{
		{"aws_profile", "x"}, {"PATH=/tmp", "x"}, {"", "x"}, {"1A", "x"},
		{"A", "a\nb"}, {"A", "a\rb"}, {"A", "a\x00b"}, {"A", strings.Repeat("x", 1025)},
	} {
		if err := CheckEnvVar(bad[0], bad[1]); err == nil {
			t.Errorf("CheckEnvVar(%q, %.10q) was accepted", bad[0], bad[1])
		}
	}
}

func TestCheckEnvEntry(t *testing.T) {
	for _, ok := range []string{"AWS_PROFILE=prod", "A="} {
		if err := CheckEnvEntry(ok); err != nil {
			t.Errorf("CheckEnvEntry(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"AWS_PROFILE", "=x", "lower=x", "A=b\nc", ""} {
		if err := CheckEnvEntry(bad); err == nil {
			t.Errorf("CheckEnvEntry(%q) was accepted", bad)
		}
	}
}
