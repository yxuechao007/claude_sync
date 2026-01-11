package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPathHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home dir: %v", err)
	}

	cases := []struct {
		input string
		want  string
	}{
		{input: "~", want: home},
		{input: "~/claude", want: filepath.Join(home, "claude")},
	}

	for _, tc := range cases {
		got, err := ExpandPath(tc.input)
		if err != nil {
			t.Fatalf("ExpandPath(%q) error: %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("ExpandPath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
