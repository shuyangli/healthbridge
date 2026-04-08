package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shuyangli/healthbridge/cli/internal/health"
)

// TestSchemaJSONIsStable verifies the checked-in proto/schema.json is
// what the generator would produce. Failing tests print the canonical
// hint instructing the contributor to re-run the generator.
func TestSchemaJSONIsStable(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	path := filepath.Join(root, "proto", "schema.json")
	existing, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	got, err := generateSchemaJSON(string(existing))
	if err != nil {
		t.Fatalf("generateSchemaJSON: %v", err)
	}
	if got != string(existing) {
		t.Errorf("proto/schema.json is out of date — run `cd cli && go run ./cmd/gen-types`")
	}
}

// TestTypesMDIsStable verifies the checked-in TYPES.md matches the
// generator output.
func TestTypesMDIsStable(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	path := filepath.Join(root, "skill", "healthbridge", "references", "TYPES.md")
	existing, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	got := generateTypesMD()
	if got != string(existing) {
		t.Errorf("skill/healthbridge/references/TYPES.md is out of date — run `cd cli && go run ./cmd/gen-types`")
	}
}

// TestSchemaJSONContainsEveryCatalogWire is a defensive check: if the
// regex-based replacement ever silently produces an empty enum (e.g.
// because the regex stopped matching), this test catches it before
// the schema ships.
func TestSchemaJSONContainsEveryCatalogWire(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "proto", "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(b)
	for _, d := range health.Catalog {
		needle := `"` + string(d.Wire) + `"`
		if !strings.Contains(contents, needle) {
			t.Errorf("schema.json missing wire %q", d.Wire)
		}
	}
	for _, w := range []health.SampleType{health.SleepAnalysis, health.Workout} {
		if !strings.Contains(contents, `"`+string(w)+`"`) {
			t.Errorf("schema.json missing carryover wire %q", w)
		}
	}
}

// TestGenerateSchemaJSONIsIdempotent runs the generator on its own
// output and asserts it produces the same result. Catches drift in
// the regex match boundaries (which previously caused a duplication
// bug during M2 development).
func TestGenerateSchemaJSONIsIdempotent(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	existing, err := os.ReadFile(filepath.Join(root, "proto", "schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := generateSchemaJSON(string(existing))
	if err != nil {
		t.Fatal(err)
	}
	second, err := generateSchemaJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("generateSchemaJSON is not idempotent")
	}
}
