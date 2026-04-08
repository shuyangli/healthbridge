package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shuyangli/healthbridge/cli/internal/health"
)

func TestTypesHumanLists(t *testing.T) {
	root := Root()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"types"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, t2 := range health.AllSampleTypes() {
		if !strings.Contains(out, string(t2)) {
			t.Errorf("missing %q in types output:\n%s", t2, out)
		}
	}
}

func TestTypesJSON(t *testing.T) {
	root := Root()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"types", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Types []map[string]string `json:"types"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, buf.String())
	}
	if len(got.Types) != len(health.AllSampleTypes()) {
		t.Errorf("got %d types in JSON, want %d", len(got.Types), len(health.AllSampleTypes()))
	}
	for _, entry := range got.Types {
		if entry["type"] == "" || entry["unit"] == "" {
			t.Errorf("entry missing fields: %+v", entry)
		}
	}
}

func TestCanonicalUnitForKnownTypes(t *testing.T) {
	cases := map[health.SampleType]string{
		health.StepCount:             "count",
		health.DietaryEnergyConsumed: "kcal",
		health.BodyMass:              "kg",
		health.HeartRate:             "count/min",
		health.BloodGlucose:          "mg/dL",
	}
	for in, want := range cases {
		if got := canonicalUnitForType(in); got != want {
			t.Errorf("canonicalUnitForType(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestTypesJSONHasFullCatalog asserts the `types --json` output
// exposes at least 100 entries (so a stray catalog deletion can't
// silently halve the surface) and has a non-empty unit on every entry.
// Spot-checks one type from each category to make sure no category
// dropped out of the catalog or the JSON serialiser.
func TestTypesJSONHasFullCatalog(t *testing.T) {
	root := Root()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"types", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Types []map[string]string `json:"types"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, buf.String())
	}
	if len(got.Types) < 100 {
		t.Errorf("got %d types in JSON, want at least 100", len(got.Types))
	}
	for _, e := range got.Types {
		if e["type"] == "" || e["unit"] == "" {
			t.Errorf("entry missing fields: %+v", e)
		}
	}

	// One spot-check per category, including types that only exist
	// after the M3 catalog expansion. If any of these are missing the
	// catalog has lost a section.
	want := map[string]string{
		"step_count":                 "count",       // Activity
		"running_power":              "W",           // Activity (iOS 16+)
		"vo2_max":                    "ml/(kg*min)", // Activity (compound unit)
		"waist_circumference":        "m",           // Body measurement (new)
		"walking_heart_rate_average": "count/min",   // Vital sign (new)
		"blood_pressure_systolic":    "mmHg",        // Vital sign (new)
		"forced_vital_capacity":      "L",           // Lab result (new)
		"insulin_delivery":           "IU",          // Lab result (new)
		"dietary_vitamin_d":          "mcg",         // Nutrition (new)
		"dietary_sodium":             "mg",          // Nutrition (existing)
		"environmental_audio_exposure": "dBASPL",    // Hearing (new)
		"apple_walking_steadiness":   "%",           // Mobility (new)
		"basal_body_temperature":     "degC",        // Reproductive (new)
		"uv_exposure":                "count",       // UV (new)
		"underwater_depth":           "m",           // Diving (new)
		"blood_alcohol_content":      "%",           // Alcohol (new)
		"sleep_analysis":             "s",           // Carryover
		"workout":                    "s",           // Carryover
	}
	have := make(map[string]string, len(got.Types))
	for _, e := range got.Types {
		have[e["type"]] = e["unit"]
	}
	for wire, wantUnit := range want {
		if have[wire] != wantUnit {
			t.Errorf("types[%q].unit = %q, want %q", wire, have[wire], wantUnit)
		}
	}
}
