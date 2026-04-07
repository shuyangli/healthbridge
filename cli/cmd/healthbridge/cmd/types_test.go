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
