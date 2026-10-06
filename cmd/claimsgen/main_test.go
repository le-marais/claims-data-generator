package main

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strconv"
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
	for _, name := range []string{"policies.csv", "claims.csv", "transactions.csv", "triangles.csv", "exposure.csv"} {
		a, _ := os.ReadFile(filepath.Join(outA, name))
		b, _ := os.ReadFile(filepath.Join(outB, name))
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs across identical seeds", name)
		}
	}
}

func TestGenerateWritesTrianglesAndExposure(t *testing.T) {
	out := filepath.Join(t.TempDir(), "output")
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "--out", out, "--years", "2", "--initial-book-size", "100", "--seed", "7"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}
	for _, name := range []string{"policies.csv", "claims.csv", "transactions.csv", "triangles.csv", "exposure.csv"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	if !strings.Contains(stdout.String(), "triangle rows") {
		t.Errorf("stdout %q should report the triangle row count", stdout.String())
	}
	if !strings.Contains(stdout.String(), "exposure rows") {
		t.Errorf("stdout %q should report the exposure row count", stdout.String())
	}
}

func TestGenerateAcceptsBothOriginBases(t *testing.T) {
	for _, basis := range []string{"accident", "underwriting"} {
		out := filepath.Join(t.TempDir(), "output")
		var stdout, stderr bytes.Buffer
		code := run([]string{"generate", "--out", out, "--years", "2", "--initial-book-size", "100",
			"--seed", "7", "--origin-basis", basis}, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("basis %q: exit code = %d, stderr: %s", basis, code, stderr.String())
		}
		if _, err := os.Stat(filepath.Join(out, "triangles.csv")); err != nil {
			t.Errorf("basis %q: missing triangles.csv: %v", basis, err)
		}
	}
}

func TestGenerateOriginBasesDifferInTheOutput(t *testing.T) {
	read := func(t *testing.T, basis string) []byte {
		t.Helper()
		out := filepath.Join(t.TempDir(), "output")
		var buf bytes.Buffer
		if code := run([]string{"generate", "--out", out, "--years", "2", "--initial-book-size", "100",
			"--seed", "7", "--origin-basis", basis}, &buf, &buf); code != 0 {
			t.Fatalf("basis %q failed: %s", basis, buf.String())
		}
		b, err := os.ReadFile(filepath.Join(out, "triangles.csv"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if bytes.Equal(read(t, "accident"), read(t, "underwriting")) {
		t.Error("the two origin bases produced identical triangles.csv; the flag is not wired through")
	}
}

func TestGenerateRejectsAnUnknownOriginBasis(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"generate", "--out", filepath.Join(t.TempDir(), "o"), "--origin-basis", "policy"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("expected nonzero exit for an unknown origin basis")
	}
	if !strings.Contains(stderr.String(), "origin basis") {
		t.Errorf("stderr %q should name the origin basis problem", stderr.String())
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

// --preset motor-personal is the default, byte for byte.
func TestGeneratePresetMatchesTheDefault(t *testing.T) {
	outA := filepath.Join(t.TempDir(), "a")
	outB := filepath.Join(t.TempDir(), "b")
	var buf bytes.Buffer
	if code := run([]string{"generate", "--out", outA, "--years", "2", "--initial-book-size", "100"}, &buf, &buf); code != 0 {
		t.Fatalf("default run failed: %s", buf.String())
	}
	if code := run([]string{"generate", "--out", outB, "--years", "2", "--initial-book-size", "100", "--preset", "motor-personal"}, &buf, &buf); code != 0 {
		t.Fatalf("preset run failed: %s", buf.String())
	}
	for _, name := range []string{"policies.csv", "claims.csv", "transactions.csv", "triangles.csv", "exposure.csv"} {
		a, _ := os.ReadFile(filepath.Join(outA, name))
		b, _ := os.ReadFile(filepath.Join(outB, name))
		if !bytes.Equal(a, b) {
			t.Errorf("%s differs between the default and --preset motor-personal", name)
		}
	}
}

// The commercial preset writes a fleet book: some vehicles share a fleet.
func TestGenerateCommercialPresetWritesFleets(t *testing.T) {
	out := filepath.Join(t.TempDir(), "output")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"generate", "--out", out, "--years", "2", "--initial-book-size", "50", "--preset", "motor-commercial"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "motor-commercial:") {
		t.Errorf("stdout %q should name the preset", stdout.String())
	}
	policies, err := os.ReadFile(filepath.Join(out, "policies.csv"))
	if err != nil {
		t.Fatal(err)
	}
	fleets := map[string]int{}
	for _, row := range strings.Split(strings.TrimSpace(string(policies)), "\n")[1:] {
		fleets[strings.Split(row, ",")[1]]++
	}
	shared := 0
	for _, n := range fleets {
		if n > 1 {
			shared++
		}
	}
	if shared == 0 {
		t.Fatalf("no fleet_id is shared by two policies among %d fleets", len(fleets))
	}
}

func TestGenerateRejectsPresetWithConfig(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := filepath.Join("..", "..", "internal", "infrastructure", "config", "motor-personal.yaml")
	if code := run([]string{"generate", "--preset", "motor-personal", "--config", path}, &stdout, &stderr); code == 0 {
		t.Fatal("expected nonzero exit for --preset with --config")
	}
	if !strings.Contains(stderr.String(), "--preset") || !strings.Contains(stderr.String(), "--config") {
		t.Errorf("stderr %q should name both flags", stderr.String())
	}
}

func TestGenerateRejectsAnUnknownPreset(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"generate", "--preset", "marine-cargo"}, &stdout, &stderr); code == 0 {
		t.Fatal("expected nonzero exit for an unknown preset")
	}
	if !strings.Contains(stderr.String(), "marine-cargo") {
		t.Errorf("stderr %q should name the preset", stderr.String())
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

func TestUIRejectsBadFlags(t *testing.T) {
	var stdout, stderr strings.Builder
	if code := run([]string{"ui", "--bogus"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestUnknownSubcommandPrintsUsage(t *testing.T) {
	var stdout, stderr strings.Builder
	if code := run([]string{"frobnicate"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage") {
		t.Fatalf("stderr = %q, want usage text", stderr.String())
	}
}

// sectionDetailArgs is a small, fast run; extra flags are appended.
func sectionDetailArgs(out string, extra ...string) []string {
	args := []string{"generate", "--out", out, "--years", "2", "--initial-book-size", "100", "--seed", "7"}
	return append(args, extra...)
}

func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		rows = append(rows, strings.Split(line, ","))
	}
	return rows
}

func TestGenerateWithoutSectionDetailWritesExactlyFiveFiles(t *testing.T) {
	out := filepath.Join(t.TempDir(), "output")
	var buf bytes.Buffer
	if code := run(sectionDetailArgs(out), &buf, &buf); code != 0 {
		t.Fatalf("exit code = %d: %s", code, buf.String())
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	want := []string{"claims.csv", "exposure.csv", "policies.csv", "transactions.csv", "triangles.csv"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("files = %v, want %v", got, want)
	}
}

func TestGenerateSectionDetailLeavesTheFiveFilesUnchanged(t *testing.T) {
	plain := filepath.Join(t.TempDir(), "plain")
	detail := filepath.Join(t.TempDir(), "detail")
	var buf bytes.Buffer
	if code := run(sectionDetailArgs(plain), &buf, &buf); code != 0 {
		t.Fatalf("plain run failed: %s", buf.String())
	}
	if code := run(sectionDetailArgs(detail, "--section-detail"), &buf, &buf); code != 0 {
		t.Fatalf("detail run failed: %s", buf.String())
	}
	for _, name := range []string{"policies.csv", "claims.csv", "transactions.csv", "triangles.csv", "exposure.csv"} {
		a, _ := os.ReadFile(filepath.Join(plain, name))
		b, _ := os.ReadFile(filepath.Join(detail, name))
		if !bytes.Equal(a, b) {
			t.Errorf("%s changed when --section-detail was set", name)
		}
	}
}

func TestGenerateSectionDetailClaimSectionsMatchClaims(t *testing.T) {
	known := map[string]bool{"own_damage": true, "third_party_property": true, "third_party_injury": true}
	out := filepath.Join(t.TempDir(), "output")
	var buf bytes.Buffer
	if code := run(sectionDetailArgs(out, "--section-detail"), &buf, &buf); code != 0 {
		t.Fatalf("exit code = %d: %s", code, buf.String())
	}
	claims := readCSV(t, filepath.Join(out, "claims.csv"))
	sections := readCSV(t, filepath.Join(out, "claim_sections.csv"))
	if strings.Join(sections[0], ",") != "claim_id,section" {
		t.Errorf("header = %v", sections[0])
	}
	if len(sections) != len(claims) {
		t.Fatalf("claim_sections.csv has %d rows, claims.csv has %d", len(sections), len(claims))
	}
	for i := 1; i < len(claims); i++ {
		if sections[i][0] != claims[i][0] {
			t.Fatalf("row %d: claim id %s, want %s", i, sections[i][0], claims[i][0])
		}
		if !known[sections[i][1]] {
			t.Fatalf("row %d: unknown section %q", i, sections[i][1])
		}
	}
}

func TestGenerateSectionDetailExposureSectionsSumToExposure(t *testing.T) {
	for _, basis := range []string{"accident", "underwriting"} {
		t.Run(basis, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "output")
			var buf bytes.Buffer
			if code := run(sectionDetailArgs(out, "--section-detail", "--origin-basis", basis), &buf, &buf); code != 0 {
				t.Fatalf("exit code = %d: %s", code, buf.String())
			}
			exposure := readCSV(t, filepath.Join(out, "exposure.csv"))
			detail := readCSV(t, filepath.Join(out, "exposure_sections.csv"))
			if strings.Join(detail[0], ",") != "origin_month,section,premium" {
				t.Errorf("header = %v", detail[0])
			}
			const sections = 3
			if got, want := len(detail)-1, (len(exposure)-1)*sections; got != want {
				t.Fatalf("exposure_sections.csv has %d rows, want %d", got, want)
			}
			wantOrder := []string{"own_damage", "third_party_property", "third_party_injury"}
			for m := 1; m < len(exposure); m++ {
				total := 0.0
				for s := 0; s < sections; s++ {
					row := detail[(m-1)*sections+s+1]
					if row[0] != exposure[m][0] {
						t.Fatalf("row for month %s section %d has origin_month %s", exposure[m][0], s, row[0])
					}
					if row[1] != wantOrder[s] {
						t.Fatalf("month %s: section %d is %q, want %q", row[0], s, row[1], wantOrder[s])
					}
					p, err := strconv.ParseFloat(row[2], 64)
					if err != nil {
						t.Fatal(err)
					}
					total += p
				}
				premium, _ := strconv.ParseFloat(exposure[m][1], 64)
				policies, _ := strconv.Atoi(exposure[m][3])
				// A policy's sections are rounded to the cent separately from its
				// premium (up to a cent apart, pro-rated by the days earned in the
				// month), then each printed figure is rounded again.
				tol := 0.01*float64(policies) + 0.01*sections
				if math.Abs(total-premium) > tol {
					t.Errorf("month %s: section premiums sum to %.2f, exposure premium %.2f (tolerance %.2f)", exposure[m][0], total, premium, tol)
				}
			}
		})
	}
}

func TestGenerateSectionDetailSameSeedSameBytes(t *testing.T) {
	outA := filepath.Join(t.TempDir(), "a")
	outB := filepath.Join(t.TempDir(), "b")
	var buf bytes.Buffer
	for _, out := range []string{outA, outB} {
		if code := run(sectionDetailArgs(out, "--section-detail"), &buf, &buf); code != 0 {
			t.Fatalf("run failed: %s", buf.String())
		}
	}
	for _, name := range []string{"claim_sections.csv", "exposure_sections.csv"} {
		a, _ := os.ReadFile(filepath.Join(outA, name))
		b, _ := os.ReadFile(filepath.Join(outB, name))
		if len(a) == 0 || !bytes.Equal(a, b) {
			t.Errorf("%s is empty or differs across identical seeds", name)
		}
	}
}
