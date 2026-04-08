package health

import (
	"encoding/json"
	"testing"
	"time"
)

// TestSampleJSONRoundTripEveryCatalogEntry marshals and unmarshals a
// Sample for every catalog entry. This is the cheap-but-thorough Layer-2
// guard against wire breakage: any catalog Unit string that contains
// JSON-incompatible bytes, any SampleType value that JSON encoding
// rejects, or any struct-tag drift surfaces here.
func TestSampleJSONRoundTripEveryCatalogEntry(t *testing.T) {
	start := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)
	for _, d := range Catalog {
		t.Run(string(d.Wire), func(t *testing.T) {
			in := Sample{
				UUID:  "01J9ZX0EXAMPLE0000000000001",
				Type:  d.Wire,
				Value: 1.5,
				Unit:  d.Unit,
				Start: start,
				End:   end,
			}
			b, err := json.Marshal(in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var out Sample
			if err := json.Unmarshal(b, &out); err != nil {
				t.Fatalf("unmarshal: %v\n%s", err, b)
			}
			if out.Type != in.Type {
				t.Errorf("type round-trip: got %q, want %q", out.Type, in.Type)
			}
			if out.Unit != in.Unit {
				t.Errorf("unit round-trip: got %q, want %q", out.Unit, in.Unit)
			}
			if out.Value != in.Value {
				t.Errorf("value round-trip: got %v, want %v", out.Value, in.Value)
			}
			if !out.Start.Equal(in.Start) || !out.End.Equal(in.End) {
				t.Errorf("time round-trip: got [%v, %v], want [%v, %v]", out.Start, out.End, in.Start, in.End)
			}
		})
	}
}

// TestAllSampleTypesIncludesCatalogAndCarryover asserts AllSampleTypes
// is the union of catalog wires and the non-quantity carryover.
func TestAllSampleTypesIncludesCatalogAndCarryover(t *testing.T) {
	all := AllSampleTypes()
	if want := len(Catalog) + len(nonQuantitySampleTypes); len(all) != want {
		t.Fatalf("AllSampleTypes() = %d entries, want %d", len(all), want)
	}
	have := make(map[SampleType]bool, len(all))
	for _, st := range all {
		have[st] = true
	}
	for _, d := range Catalog {
		if !have[d.Wire] {
			t.Errorf("AllSampleTypes() missing catalog entry %q", d.Wire)
		}
	}
	for _, st := range nonQuantitySampleTypes {
		if !have[st] {
			t.Errorf("AllSampleTypes() missing carryover %q", st)
		}
	}
}

// TestAllSampleTypesIsCopy asserts mutating the returned slice doesn't
// corrupt the package-level cache.
func TestAllSampleTypesIsCopy(t *testing.T) {
	a := AllSampleTypes()
	if len(a) == 0 {
		t.Fatal("AllSampleTypes returned empty")
	}
	a[0] = "tampered"
	b := AllSampleTypes()
	if b[0] == "tampered" {
		t.Fatal("AllSampleTypes returned a shared slice; mutations leaked into the cache")
	}
}
