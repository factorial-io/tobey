package main

import (
	"testing"
)

func TestGetHostFromURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "https://factorial.io", want: "factorial.io"},
		{input: "https://factorial.io/", want: "factorial.io"},
		{input: "https://www.factorial.io/", want: "factorial.io"},
		{input: "https://www.factorial.io/foo/bar", want: "factorial.io"},
	}

	for _, tc := range tests {
		got := GetHostFromURL(tc.input)

		if got != tc.want {
			t.Fatalf("expected: %s, got: %s", tc.want, got)
		}
	}
}
