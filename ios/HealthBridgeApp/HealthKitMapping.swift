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

    static func quantityIdentifier(for sampleType: SampleType) -> HKQuantityTypeIdentifier? {
        switch sampleType {
        case .stepCount:             return .stepCount
        case .activeEnergyBurned:    return .activeEnergyBurned
        case .basalEnergyBurned:     return .basalEnergyBurned
        case .heartRate:             return .heartRate
        case .heartRateResting:      return .restingHeartRate
        case .bodyMass:              return .bodyMass
        case .bodyMassIndex:         return .bodyMassIndex
        case .bloodGlucose:          return .bloodGlucose
        case .dietaryEnergyConsumed: return .dietaryEnergyConsumed
        case .dietaryProtein:        return .dietaryProtein
        case .dietaryCarbohydrates:  return .dietaryCarbohydrates
        case .dietaryFatTotal:       return .dietaryFatTotal
        case .dietaryFatSaturated:   return .dietaryFatSaturated
        case .dietaryFiber:          return .dietaryFiber
        case .dietarySugar:          return .dietarySugar
        case .dietaryCholesterol:    return .dietaryCholesterol
        case .dietarySodium:         return .dietarySodium
        case .dietaryCaffeine:       return .dietaryCaffeine
        case .dietaryWater:          return .dietaryWater
        case .sleepAnalysis, .workout: return nil
        }
    }

    /// The canonical unit string for a given sample type. This must stay
    /// in sync with cli/cmd/healthbridge/cmd/types.go's
    /// `canonicalUnitForType` so that read responses round-trip cleanly
    /// through the wire format.
    static func canonicalUnit(for sampleType: SampleType) -> String {
        switch sampleType {
        case .stepCount:                                       return "count"
        case .activeEnergyBurned, .basalEnergyBurned, .dietaryEnergyConsumed:
            return "kcal"
        case .heartRate, .heartRateResting:                    return "count/min"
        case .bodyMass:                                        return "kg"
        case .bodyMassIndex:                                   return "count"
        case .bloodGlucose:                                    return "mg/dL"
        case .dietaryProtein, .dietaryCarbohydrates,
             .dietaryFatTotal, .dietaryFatSaturated,
             .dietaryFiber, .dietarySugar:                     return "g"
        case .dietaryCholesterol, .dietarySodium, .dietaryCaffeine:
            return "mg"
        case .dietaryWater:                                    return "mL"
        case .sleepAnalysis, .workout:                         return ""
        }
    }

    /// Map a CLI-side unit string into an HKUnit. The CLI is supposed to
    /// send the canonical unit per `healthbridge types`, but we accept a
    /// few common aliases.
    static func unit(from string: String) -> HKUnit {
        switch string.lowercased() {
        case "count":      return .count()
        case "count/min":  return HKUnit.count().unitDivided(by: .minute())
        case "kcal":       return .kilocalorie()
        case "cal":        return .smallCalorie()
        case "kj":         return .jouleUnit(with: .kilo)
        case "g":          return .gram()
        case "mg":         return .gramUnit(with: .milli)
        case "kg":         return .gramUnit(with: .kilo)
        case "lb":         return .pound()
        case "ml":         return .literUnit(with: .milli)
        case "l":          return .liter()
        case "fl_oz_us":   return .fluidOunceUS()
        case "mg/dl":      return HKUnit(from: "mg/dL")
        case "mmol/l":     return HKUnit(from: "mmol/L")
        default:           return HKUnit(from: string) // last-ditch parse
        }
    }

    /// Read scopes the app requests at pairing time.
    static func readScopes() -> Set<HKObjectType> {
        var out: Set<HKObjectType> = []
        for s in SampleType.allCases {
            if let q = quantityType(for: s) {
                out.insert(q)
            }
        }
        return out
    }

    /// Write scopes the app requests at pairing time. We deliberately
    /// request write for the "agent-friendly" categories — calories
    /// (in/out) and the dietary macros + water — so the agent can log
    /// meals and exercise on the user's behalf without re-prompting.
    static func writeScopes() -> Set<HKSampleType> {
        let writable: [SampleType] = [
            .activeEnergyBurned,
            .dietaryEnergyConsumed,
            .dietaryProtein,
            .dietaryCarbohydrates,
            .dietaryFatTotal,
            .dietaryFatSaturated,
            .dietaryFiber,
            .dietarySugar,
            .dietaryCholesterol,
            .dietarySodium,
            .dietaryCaffeine,
            .dietaryWater,
            .bodyMass,
        ]
        var out: Set<HKSampleType> = []
        for s in writable {
            if let q = quantityType(for: s) {
                out.insert(q)
            }
        }
        return out
    }
}

#endif
