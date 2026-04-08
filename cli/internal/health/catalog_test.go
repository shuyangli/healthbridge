package health

import (
	"regexp"
	"testing"
)

// TestCatalogNoDuplicateWires asserts every wire name appears at most
// once. Duplicate wire names would silently shadow each other in the
// catalogByWire map.
func TestCatalogNoDuplicateWires(t *testing.T) {
	seen := make(map[SampleType]int)
	for i, d := range Catalog {
		if prev, ok := seen[d.Wire]; ok {
			t.Errorf("duplicate wire %q at indices %d and %d", d.Wire, prev, i)
		}
		seen[d.Wire] = i
	}
}

// TestCatalogNoDuplicateHKIdentifiers asserts every HK identifier
// appears at most once. Duplicates would mean two wire names point at
// the same HealthKit type, which is almost certainly a typo.
func TestCatalogNoDuplicateHKIdentifiers(t *testing.T) {
	seen := make(map[string]int)
	for i, d := range Catalog {
		if prev, ok := seen[d.HKIdentifier]; ok {
			t.Errorf("duplicate HKIdentifier %q at indices %d and %d", d.HKIdentifier, prev, i)
		}
		seen[d.HKIdentifier] = i
	}
}

func TestCatalogWireNamesAreSnakeCase(t *testing.T) {
	re := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	for _, d := range Catalog {
		if !re.MatchString(string(d.Wire)) {
			t.Errorf("wire %q is not snake_case", d.Wire)
		}
	}
}

func TestCatalogHKIdentifiersAreLowerCamel(t *testing.T) {
	re := regexp.MustCompile(`^[a-z][A-Za-z0-9]*$`)
	for _, d := range Catalog {
		if !re.MatchString(d.HKIdentifier) {
			t.Errorf("HKIdentifier %q is not lowerCamel", d.HKIdentifier)
		}
	}
}

func TestCatalogEveryEntryHasUnit(t *testing.T) {
	for _, d := range Catalog {
		if d.Unit == "" {
			t.Errorf("catalog entry %q has empty Unit", d.Wire)
		}
	}
}

func TestCatalogLookupByWire(t *testing.T) {
	for _, d := range Catalog {
		got := LookupByWire(d.Wire)
		if got == nil {
			t.Errorf("LookupByWire(%q) = nil", d.Wire)
			continue
		}
		if got.HKIdentifier != d.HKIdentifier {
			t.Errorf("LookupByWire(%q).HKIdentifier = %q, want %q", d.Wire, got.HKIdentifier, d.HKIdentifier)
		}
	}
	if LookupByWire("not_a_real_type") != nil {
		t.Errorf("LookupByWire(unknown) returned non-nil")
	}
	// sleep_analysis and workout are intentionally absent from the
	// catalog (not quantity types).
	if LookupByWire(SleepAnalysis) != nil {
		t.Errorf("LookupByWire(sleep_analysis) should be nil — not a quantity type")
	}
	if LookupByWire(Workout) != nil {
		t.Errorf("LookupByWire(workout) should be nil — not a quantity type")
	}
}

// TestCatalogPreservesShippedWireNames pins the wire names that have
// already been shipped to users. If a future refactor renames any of
// these (e.g. by trying to "fix" heart_rate_resting → resting_heart_rate),
// pre-paired CLIs would silently start sending unrecognized types.
// This test fails loudly first.
func TestCatalogPreservesShippedWireNames(t *testing.T) {
	pinned := map[SampleType]string{
		StepCount:             "stepCount",
		ActiveEnergyBurned:    "activeEnergyBurned",
		BasalEnergyBurned:     "basalEnergyBurned",
		HeartRate:             "heartRate",
		HeartRateResting:      "restingHeartRate", // wire diverges from HK
		BodyMass:              "bodyMass",
		BodyMassIndex:         "bodyMassIndex",
		BodyFatPercentage:     "bodyFatPercentage",
		LeanBodyMass:          "leanBodyMass",
		Height:                "height",
		BloodGlucose:          "bloodGlucose",
		DietaryEnergyConsumed: "dietaryEnergyConsumed",
		DietaryProtein:        "dietaryProtein",
		DietaryCarbohydrates:  "dietaryCarbohydrates",
		DietaryFatTotal:       "dietaryFatTotal",
		DietaryFatSaturated:   "dietaryFatSaturated",
		DietaryFiber:          "dietaryFiber",
		DietarySugar:          "dietarySugar",
		DietaryCholesterol:    "dietaryCholesterol",
		DietarySodium:         "dietarySodium",
		DietaryCaffeine:       "dietaryCaffeine",
		DietaryWater:          "dietaryWater",
	}
	for wire, wantHK := range pinned {
		d := LookupByWire(wire)
		if d == nil {
			t.Errorf("shipped wire %q missing from catalog", wire)
			continue
		}
		if d.HKIdentifier != wantHK {
			t.Errorf("wire %q: HKIdentifier = %q, want %q", wire, d.HKIdentifier, wantHK)
		}
	}
}

// TestCatalogPreservesShippedUnits pins canonical units for the
// 22 originally-shipped quantity types. Same rationale as the wire
// pin: silently changing kg → g would corrupt user data.
func TestCatalogPreservesShippedUnits(t *testing.T) {
	pinned := map[SampleType]string{
		StepCount:             "count",
		ActiveEnergyBurned:    "kcal",
		BasalEnergyBurned:     "kcal",
		HeartRate:             "count/min",
		HeartRateResting:      "count/min",
		BodyMass:              "kg",
		BodyMassIndex:         "count",
		BodyFatPercentage:     "%",
		LeanBodyMass:          "kg",
		Height:                "m",
		BloodGlucose:          "mg/dL",
		DietaryEnergyConsumed: "kcal",
		DietaryProtein:        "g",
		DietaryCarbohydrates:  "g",
		DietaryFatTotal:       "g",
		DietaryFatSaturated:   "g",
		DietaryFiber:          "g",
		DietarySugar:          "g",
		DietaryCholesterol:    "mg",
		DietarySodium:         "mg",
		DietaryCaffeine:       "mg",
		DietaryWater:          "mL",
	}
	for wire, wantUnit := range pinned {
		d := LookupByWire(wire)
		if d == nil {
			t.Fatalf("shipped wire %q missing", wire)
		}
		if d.Unit != wantUnit {
			t.Errorf("wire %q: Unit = %q, want %q", wire, d.Unit, wantUnit)
		}
	}
}

// TestCatalogSize is a coarse sanity check — the catalog should have
// roughly all of Apple's HKQuantityTypeIdentifier surface. If somebody
// accidentally deletes a category, this fails.
func TestCatalogSize(t *testing.T) {
	if got, min := len(Catalog), 100; got < min {
		t.Errorf("catalog has %d entries, expected at least %d", got, min)
	}
}
