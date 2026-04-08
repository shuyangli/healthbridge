// HealthKitMapping translates between the wire-format SampleType enum
// (defined in HealthBridgeKit) and HealthKit's HKQuantityTypeIdentifier
// + HKUnit. This is the only place in the app that knows about
// HKQuantityTypeIdentifier strings — everything else uses the typed
// SampleType enum.

#if os(iOS)
import HealthKit
import HealthBridgeKit

enum HealthKitMapping {

    /// The HKQuantityType for a wire SampleType, or nil if the type is
    /// not a quantity (e.g. sleep_analysis is a category, workout is its
    /// own thing).
    static func quantityType(for sampleType: SampleType) -> HKQuantityType? {
        guard let id = quantityIdentifier(for: sampleType) else { return nil }
        return HKObjectType.quantityType(forIdentifier: id)
    }

    /// The HKSampleType for any wire SampleType — quantity, category
    /// (sleep), or workout. Returns nil only if HealthKit on this OS
    /// doesn't recognise the identifier (shouldn't happen for the
    /// types we ship).
    static func sampleObjectType(for sampleType: SampleType) -> HKSampleType? {
        switch sampleType {
        case .sleepAnalysis:
            return HKObjectType.categoryType(forIdentifier: .sleepAnalysis)
        case .workout:
            return HKObjectType.workoutType()
        default:
            return quantityType(for: sampleType)
        }
    }

    static func quantityIdentifier(for sampleType: SampleType) -> HKQuantityTypeIdentifier? {
        // sleep_analysis and workout intentionally have no entry in
        // the generated map — they are not HKQuantityType.
        return generatedQuantityIdentifiers[sampleType.rawValue]
    }

    /// The canonical unit string for a given sample type. For every
    /// HKQuantityTypeIdentifier-backed type this comes from the
    /// generated table, which mirrors the Go-side
    /// `canonicalUnitForType` byte-for-byte. The non-quantity carryover
    /// (sleep_analysis, workout) is reported as a duration in seconds,
    /// with the categorical / activity-type info travelling in
    /// Sample.metadata.
    static func canonicalUnit(for sampleType: SampleType) -> String {
        if let u = generatedCanonicalUnits[sampleType.rawValue] {
            return u
        }
        return "s"
    }

    /// Map an `HKCategoryValueSleepAnalysis` raw value to the stable
    /// snake_case state name we put in `Sample.metadata["state"]`.
    static func sleepStateName(forRawValue raw: Int) -> String {
        guard let v = HKCategoryValueSleepAnalysis(rawValue: raw) else {
            return "unknown"
        }
        switch v {
        case .inBed:             return "in_bed"
        case .asleepUnspecified: return "asleep_unspecified"
        case .awake:             return "awake"
        case .asleepCore:        return "asleep_core"
        case .asleepDeep:        return "asleep_deep"
        case .asleepREM:         return "asleep_rem"
        @unknown default:        return "unknown"
        }
    }

    /// Map an `HKWorkoutActivityType` to a stable snake_case name we
    /// put in `Sample.metadata["activity_type"]`. Unknown / future
    /// activity types fall back to `activity_<rawValue>` so the agent
    /// can still tell them apart.
    static func workoutActivityName(for activity: HKWorkoutActivityType) -> String {
        switch activity {
        case .running:                       return "running"
        case .walking:                       return "walking"
        case .cycling:                       return "cycling"
        case .swimming:                      return "swimming"
        case .yoga:                          return "yoga"
        case .functionalStrengthTraining:    return "functional_strength_training"
        case .traditionalStrengthTraining:   return "traditional_strength_training"
        case .highIntensityIntervalTraining: return "hiit"
        case .hiking:                        return "hiking"
        case .rowing:                        return "rowing"
        case .elliptical:                    return "elliptical"
        case .dance:                         return "dance"
        case .pilates:                       return "pilates"
        case .stairClimbing:                 return "stair_climbing"
        case .coreTraining:                  return "core_training"
        case .mixedCardio:                   return "mixed_cardio"
        case .other:                         return "other"
        default:                             return "activity_\(activity.rawValue)"
        }
    }

    /// Map a CLI-side unit string into an HKUnit.
    ///
    /// HealthKit's `HKUnit(from:)` already understands the catalog's
    /// canonical strings — `mg/dL`, `mmHg`, `degC`, `dBASPL`,
    /// `ml/(kg*min)`, `kcal/(kg*hr)`, and so on. We override the parser
    /// in two narrow cases:
    ///
    ///   1. CLI-side loose aliases that HealthKit would reject because
    ///      they're case-mangled (`mg/dl`, `ml`, `mmol/l`) or
    ///      non-standard names (`fl_oz_us`, `cal`, `kj`, `lb`, `in`).
    ///   2. `%` and `count` / `count/min`, which we route through the
    ///      typed convenience constructors so the resulting HKUnit
    ///      compares equal to whatever HealthKit returns from its own
    ///      type-specific accessors.
    static func unit(from string: String) -> HKUnit {
        switch string {
        case "%":         return .percent()
        case "count":     return .count()
        case "count/min": return HKUnit.count().unitDivided(by: .minute())
        default: break
        }
        switch string.lowercased() {
        case "cal":      return .smallCalorie()
        case "kj":       return .jouleUnit(with: .kilo)
        case "lb":       return .pound()
        case "ml":       return .literUnit(with: .milli)
        case "fl_oz_us": return .fluidOunceUS()
        case "in":       return .inch()
        case "mg/dl":    return HKUnit(from: "mg/dL")
        case "mmol/l":   return HKUnit(from: "mmol/L")
        default:
            // Trust HealthKit's parser for everything else. This
            // covers every catalog canonical unit (kcal, kg, g, mg,
            // mcg, mL, L, L/min, m, cm, m/s, min, ms, mmHg, degC,
            // degF, dBASPL, W, S, IU, ml/(kg*min), kcal/(kg*hr), …).
            return HKUnit(from: string)
        }
    }

    /// Read scopes the app requests at pairing time. Includes the
    /// sleep_analysis category type and the workout type in addition
    /// to every HKQuantityTypeIdentifier in the catalog. This is the
    /// "agent can read anything Apple ships" surface — the cost is a
    /// long HealthKit auth sheet.
    static func readScopes() -> Set<HKObjectType> {
        var out: Set<HKObjectType> = []
        for s in SampleType.allKnown {
            if let t = sampleObjectType(for: s) {
                out.insert(t)
            }
        }
        return out
    }

    /// Write scopes the app requests at pairing time. The set of
    /// writable types is generated from the catalog's `Writable`
    /// flags so that picking up the next vitamin is one Go-side
    /// catalog edit, not a Swift hand-edit. Sensors and clinical
    /// data stay read-only.
    static func writeScopes() -> Set<HKSampleType> {
        var out: Set<HKSampleType> = []
        for raw in generatedWritableSampleTypes {
            if let q = quantityType(for: SampleType(rawValue: raw)) {
                out.insert(q)
            }
        }
        return out
    }
}

#endif
