package main

import (
	"slices"
	"testing"
)

func TestTerminalEnv(t *testing.T) {
	got, err := terminalEnv(map[string]string{"AWS_REGION": "eu-west-1", "AWS_PROFILE": "prod"})
	if err != nil || !slices.Equal(got, []string{"AWS_PROFILE=prod", "AWS_REGION=eu-west-1"}) {
		t.Fatalf("env %q err %v", got, err)
	}
	for _, bad := range []map[string]string{
		{"aws_profile": "x"},
		{"PATH=/tmp": "x"},
		{"AWS_PROFILE": "a\nrm -rf"},
	} {
		if _, err := terminalEnv(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
