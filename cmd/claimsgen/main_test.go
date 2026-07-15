package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateWritesDataset(t *testing.T) {
	out := filepath.Join(t.TempDir(), "output")
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "--out", out, "--years", "2", "--initial-book-size", "100", "--seed", "7"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}
	for _, name := range []string{"policies.csv", "claims.csv", "transactions.csv"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	if !strings.Contains(stdout.String(), "policies") {
		t.Errorf("stdout %q should summarize the run", stdout.String())
	}
}

func TestGenerateSameSeedSameBytes(t *testing.T) {
	outA := filepath.Join(t.TempDir(), "a")
	outB := filepath.Join(t.TempDir(), "b")
	var buf bytes.Buffer
	if code := run([]string{"generate", "--out", outA, "--years", "2", "--initial-book-size", "100", "--seed", "9"}, &buf, &buf); code != 0 {
		t.Fatalf("first run failed: %s", buf.String())
	}
	if code := run([]string{"generate", "--out", outB, "--years", "2", "--initial-book-size", "100", "--seed", "9"}, &buf, &buf); code != 0 {
		t.Fatalf("second run failed: %s", buf.String())
	}
	for _, name := range []string{"policies.csv", "claims.csv", "transactions.csv"} {
		a, _ := os.ReadFile(filepath.Join(outA, name))
		b, _ := os.ReadFile(filepath.Join(outB, name))
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs across identical seeds", name)
		}
	}
}

func TestGenerateBadConfigFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "--config", "/does/not/exist.yaml"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected nonzero exit for missing config")
	}
	if !strings.Contains(stderr.String(), "config") {
		t.Errorf("stderr %q should mention the config problem", stderr.String())
	}
}

func TestGenerateInvalidConfigNamesField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	content, err := os.ReadFile(filepath.Join("..", "..", "internal", "infrastructure", "config", "motor-personal.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(string(content), "spread: 0.4", "spread: -1", 1)
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"generate", "--config", path}, &stdout, &stderr); code == 0 {
		t.Fatal("expected nonzero exit for invalid config")
	}
	if !strings.Contains(stderr.String(), "book.spread") {
		t.Errorf("stderr %q should name book.spread", stderr.String())
	}
}

func TestUnknownCommandFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"frobnicate"}, &stdout, &stderr); code == 0 {
		t.Fatal("expected nonzero exit for unknown command")
	}
	if !strings.Contains(stderr.String(), "usage") {
		t.Errorf("stderr %q should include usage", stderr.String())
	}
}

func TestNoCommandFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code == 0 {
		t.Fatal("expected nonzero exit when no command given")
	}
}
