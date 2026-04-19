package pipery

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCompletionContext(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line            string
		wantToken       string
		wantCommandSlot bool
	}{
		{line: "", wantToken: "", wantCommandSlot: true},
		{line: "pw", wantToken: "pw", wantCommandSlot: true},
		{line: "echo he", wantToken: "he", wantCommandSlot: false},
		{line: "echo hi | pw", wantToken: "pw", wantCommandSlot: true},
		{line: "cd ./in", wantToken: "./in", wantCommandSlot: false},
	}

	for _, test := range tests {
		test := test
		t.Run(test.line, func(t *testing.T) {
			t.Parallel()

			gotToken, gotCommandSlot := completionContext(test.line)
			if gotToken != test.wantToken {
				t.Fatalf("completionContext(%q) token = %q, want %q", test.line, gotToken, test.wantToken)
			}

			if gotCommandSlot != test.wantCommandSlot {
				t.Fatalf("completionContext(%q) command slot = %v, want %v", test.line, gotCommandSlot, test.wantCommandSlot)
			}
		})
	}
}

func TestCommandCompletionSuffixes(t *testing.T) {
	t.Parallel()

	binDir := t.TempDir()
	executable := filepath.Join(binDir, "echo")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}

	got := commandCompletionSuffixes("ec", map[string]string{"PATH": binDir}, shellBuiltins)
	want := []string{"ho"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commandCompletionSuffixes() = %v, want %v", got, want)
	}
}

func TestPathCompletionSuffixes(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	if err := os.Mkdir(filepath.Join(cwd, "internal"), 0o755); err != nil {
		t.Fatalf("mkdir internal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "index.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write index.txt: %v", err)
	}

	got := pathCompletionSuffixes(cwd, "./in", "")
	want := []string{"dex.txt", "ternal/"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pathCompletionSuffixes() = %v, want %v", got, want)
	}
}
