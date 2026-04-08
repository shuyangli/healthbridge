// Command gen-types regenerates the artifacts that mirror
// cli/internal/health.Catalog into other languages and the agent
// skill docs:
//
//   - proto/schema.json — replaces the $defs.sampleType.enum array.
//   - skill/healthbridge/references/TYPES.md — full catalog tables.
//
// In M3 it will also regenerate ios/Sources/HealthBridgeKit/Generated/
// Catalog.swift; that target is intentionally absent until the Swift
// SampleType migration lands so the iOS package keeps building.
//
// Usage:
//
//	cd cli && go run ./cmd/gen-types          # write artifacts in place
//	cd cli && go run ./cmd/gen-types -check   # CI mode: fail on drift
//
// The pure functions (generateSchemaJSON, generateTypesMD) are exported
// to the test package so unit tests can drive them without touching
// the filesystem.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/shuyangli/healthbridge/cli/internal/health"
)

func main() {
	check := flag.Bool("check", false, "verify checked-in files match the catalog and exit non-zero on drift")
	flag.Parse()

	root, err := repoRoot()
	if err != nil {
		fatalf("locate repo root: %v", err)
	}

	type target struct {
		path string
		gen  func(string) (string, error)
	}
	targets := []target{
		{
			path: filepath.Join(root, "proto", "schema.json"),
			gen:  generateSchemaJSON,
		},
		{
			path: filepath.Join(root, "skill", "healthbridge", "references", "TYPES.md"),
			gen:  func(_ string) (string, error) { return generateTypesMD(), nil },
		},
	}

	drift := false
	for _, t := range targets {
		existing, err := os.ReadFile(t.path)
		if err != nil {
			fatalf("read %s: %v", t.path, err)
		}
		next, err := t.gen(string(existing))
		if err != nil {
			fatalf("generate %s: %v", t.path, err)
		}
		if next == string(existing) {
			fmt.Printf("ok       %s\n", relPath(root, t.path))
			continue
		}
		if *check {
			fmt.Printf("DRIFT    %s\n", relPath(root, t.path))
			drift = true
			continue
		}
		if err := os.WriteFile(t.path, []byte(next), 0o644); err != nil {
			fatalf("write %s: %v", t.path, err)
		}
		fmt.Printf("wrote    %s\n", relPath(root, t.path))
	}

	if drift {
		fmt.Fprintln(os.Stderr, "\nDrift detected. Run `cd cli && go run ./cmd/gen-types` to update.")
		os.Exit(1)
	}
}

// repoRoot returns the absolute path of the healthbridge repository
// root. It works whether the caller cd'd into cli/ or invoked the tool
// from the repo root.
func repoRoot() (string, error) {
	// runtime.Caller anchors to this source file, which lives at
	// cli/cmd/gen-types/main.go — so the repo root is three parents up.
	_, this, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(this), "..", "..", "..")), nil
}

func relPath(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return rel
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gen-types: "+format+"\n", args...)
	os.Exit(2)
}

// ---- schema.json -----------------------------------------------------------

// schemaEnumPattern anchors on the "sampleType" $def, captures
// everything up to (but not including) the `[` that opens its enum
// array in group 1, and matches the array contents in group 2 so we
// can drop them entirely. Non-greedy so it stops at the first closing
// `]`. Multiple enum blocks elsewhere in the schema (jobKind, status)
// are unaffected because the leading `"sampleType":` anchor binds the
// match.
var schemaEnumPattern = regexp.MustCompile(`("sampleType":\s*\{[\s\S]*?"enum":\s*)\[[\s\S]*?\]`)

// generateSchemaJSON returns the schema.json contents with the
// sampleType enum replaced by the catalog's wire names plus the
// non-quantity carryover (sleep_analysis, workout). Other enum blocks
// in the schema are left untouched.
func generateSchemaJSON(existing string) (string, error) {
	if !schemaEnumPattern.MatchString(existing) {
		return "", fmt.Errorf("could not locate sampleType.enum in schema.json")
	}
	bracketed := buildSchemaEnumBlock()
	out := schemaEnumPattern.ReplaceAllStringFunc(existing, func(match string) string {
		sub := schemaEnumPattern.FindStringSubmatch(match)
		return sub[1] + bracketed
	})
	return out, nil
}

// buildSchemaEnumBlock returns the `[...]` bracketed enum body the
// generator splices into schema.json. Indented with 8 / 6 spaces to
// match the existing two-space-step JSON layout.
func buildSchemaEnumBlock() string {
	const itemIndent = "        "
	const closeIndent = "      "
	wires := allWireNamesInSchemaOrder()
	var b strings.Builder
	b.WriteString("[\n")
	for i, w := range wires {
		b.WriteString(itemIndent)
		b.WriteByte('"')
		b.WriteString(string(w))
		b.WriteByte('"')
		if i < len(wires)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString(closeIndent)
	b.WriteString("]")
	return b.String()
}

// allWireNamesInSchemaOrder returns the wire names to embed in
// schema.json's enum: catalog order followed by the non-quantity
// carryover types. The order is intentionally stable so future diffs
// only show real additions.
func allWireNamesInSchemaOrder() []health.SampleType {
	out := make([]health.SampleType, 0, len(health.Catalog)+2)
	for i := range health.Catalog {
		out = append(out, health.Catalog[i].Wire)
	}
	out = append(out, health.SleepAnalysis, health.Workout)
	return out
}

// ---- TYPES.md --------------------------------------------------------------

// canonical category order for the TYPES.md tables. Sleep_analysis
// and workout share a single trailing section because they are not
// quantity types.
var typesMDCategoryOrder = []health.Category{
	health.CategoryActivity,
	health.CategoryBodyMeasurement,
	health.CategoryVitalSign,
	health.CategoryLabResult,
	health.CategoryNutrition,
	health.CategoryHearing,
	health.CategoryMobility,
	health.CategoryReproductive,
	health.CategoryUVExposure,
	health.CategoryDiving,
	health.CategoryAlcohol,
	health.CategorySleep,
}

var typesMDCategoryTitles = map[health.Category]string{
	health.CategoryActivity:        "Activity",
	health.CategoryBodyMeasurement: "Body measurements",
	health.CategoryVitalSign:       "Vital signs",
	health.CategoryLabResult:       "Lab and test results",
	health.CategoryNutrition:       "Nutrition",
	health.CategoryHearing:         "Hearing health",
	health.CategoryMobility:        "Mobility",
	health.CategoryReproductive:    "Reproductive health",
	health.CategoryUVExposure:      "UV exposure & daylight",
	health.CategoryDiving:          "Diving",
	health.CategoryAlcohol:         "Alcohol",
	health.CategorySleep:           "Sleep (extra quantity types)",
}

// generateTypesMD returns the full contents of TYPES.md from the
// catalog. The "picking the right type" / "logging a meal" / "unit
// gotchas" prose at the bottom is hand-written-but-baked-in here so
// the entire file is regeneratable.
func generateTypesMD() string {
	var b strings.Builder
	b.WriteString(typesMDHeader)

	byCategory := groupByCategory()

	for _, cat := range typesMDCategoryOrder {
		entries := byCategory[cat]
		if len(entries) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s (%d)\n\n", typesMDCategoryTitles[cat], len(entries))
		b.WriteString("| type | unit | write |\n")
		b.WriteString("|---|---|---|\n")
		// Sort within category by wire name for deterministic output
		// (the catalog itself is loosely ordered "existing first" which
		// would create churn if a new entry is added in the middle).
		sort.Slice(entries, func(i, j int) bool {
			return string(entries[i].Wire) < string(entries[j].Wire)
		})
		for _, d := range entries {
			write := ""
			if d.Writable {
				write = "yes"
			}
			fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", d.Wire, d.Unit, write)
		}
	}

	// Carryover non-quantity section.
	b.WriteString(typesMDSleepWorkoutSection)
	b.WriteString(typesMDFooter)
	return b.String()
}

func groupByCategory() map[health.Category][]health.Definition {
	out := make(map[health.Category][]health.Definition, 12)
	for _, d := range health.Catalog {
		out[d.Category] = append(out[d.Category], d)
	}
	return out
}

const typesMDHeader = "# healthbridge — sample type catalog\n" +
	"\n" +
	"The canonical list of HealthKit sample types this CLI supports,\n" +
	"with the unit string each one expects on writes. **This file is\n" +
	"generated** from `cli/internal/health/catalog.go` by\n" +
	"`cli/cmd/gen-types`. Do not edit by hand. Run\n" +
	"`cd cli && go run ./cmd/gen-types` to regenerate after a catalog\n" +
	"change.\n" +
	"\n" +
	"Run `healthbridge types --json` if you need the authoritative list\n" +
	"for the binary that's actually installed.\n" +
	"\n" +
	"`write` = `yes` means the iOS app requests HealthKit write\n" +
	"authorization for that type at pairing time, so the agent can log\n" +
	"new samples for it. Read access is requested for every type below.\n"

const typesMDSleepWorkoutSection = "\n## Sleep & workouts (HKCategory / HKWorkout, read-only)\n" +
	"\n" +
	"Both are reported as one `Sample` per HealthKit record, with\n" +
	"`value` set to the **duration in seconds** and `unit` set to `s`.\n" +
	"Categorical or activity-type information travels in `metadata`.\n" +
	"\n" +
	"| type | unit | metadata fields |\n" +
	"|---|---|---|\n" +
	"| `sleep_analysis` | `s` | `state`: one of `in_bed`, `awake`, `asleep_unspecified`, `asleep_core`, `asleep_deep`, `asleep_rem` |\n" +
	"| `workout` | `s` | `activity_type` (e.g. `running`, `cycling`, `hiit`, …), and when present `total_energy_burned_kcal` and `total_distance_m` |\n" +
	"\n" +
	"Writes are not yet implemented for either type.\n"

const typesMDFooter = "\n## Picking the right type\n" +
	"\n" +
	"- **\"calories\" without context** → `dietary_energy_consumed`. Don't\n" +
	"  guess between active vs basal — those are *expenditure* types written\n" +
	"  by Apple Watch, not by users.\n" +
	"- **Body weight** → always `body_mass`, never invent `weight`.\n" +
	"- **Heart rate** → use `heart_rate_resting` only when the user said\n" +
	"  \"resting\"; otherwise `heart_rate`.\n" +
	"- **Distance for a run/ride** → prefer the modality-specific type\n" +
	"  (`distance_walking_running`, `distance_cycling`, `distance_swimming`)\n" +
	"  over a generic distance count.\n" +
	"\n" +
	"## Logging a meal with macros\n" +
	"\n" +
	"When the user gives you both calories and macros, write each as its own\n" +
	"sample with the same `--at` timestamp:\n" +
	"\n" +
	"```sh\n" +
	"T=\"2026-04-07T12:30:00Z\"\n" +
	"healthbridge write dietary_energy_consumed --value 620 --unit kcal --at \"$T\" --json\n" +
	"healthbridge write dietary_protein         --value 38  --unit g    --at \"$T\" --json\n" +
	"healthbridge write dietary_carbohydrates   --value 72  --unit g    --at \"$T\" --json\n" +
	"healthbridge write dietary_fat_total       --value 18  --unit g    --at \"$T\" --json\n" +
	"```\n" +
	"\n" +
	"HealthKit will group samples written within the same minute under the\n" +
	"\"Food\" entry in the Health app.\n" +
	"\n" +
	"## Unit gotchas\n" +
	"\n" +
	"- HealthKit unit strings are case-sensitive. `kcal`, `kg`, `g`, `mg`,\n" +
	"  `mcg`, `mL` (capital L), `count`, `count/min`, `mg/dL`.\n" +
	"- Compound units use `/` and parens: `count/min`, `mg/dL`,\n" +
	"  `ml/(kg*min)` for VO₂max.\n" +
	"- Percentages: HealthKit stores percentage-typed quantities (body\n" +
	"  fat, oxygen saturation, walking steadiness, AFib burden) as a\n" +
	"  fraction in `[0, 1]`. Convert before writing — pass `0.18` for\n" +
	"  18 %, not `18`.\n" +
	"- Water: HealthKit prefers `mL`; if the user says \"16 oz\", convert\n" +
	"  to `473` mL.\n" +
	"- Distances are metres. Convert miles/feet/yards before writing.\n" +
	"- Speeds are `m/s`. Convert km/h or mph before writing.\n" +
	"- Power is `W` (watts). HealthKit accepts `W` directly.\n" +
	"- Temperatures are degrees Celsius (`degC`). HealthKit will convert\n" +
	"  on read but `degC` is the canonical write unit.\n"
