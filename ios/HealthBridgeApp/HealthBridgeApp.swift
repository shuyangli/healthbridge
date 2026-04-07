// HealthBridge — the iOS companion app.
//
// SwiftUI entry point + foreground drain loop. Imports HealthKit and
// SwiftUI, so this file only builds inside the Xcode project produced
// by `xcodegen generate` from ios/project.yml. The HealthKit-free heart
// of the app lives in the HealthBridgeKit Swift package and is exercised
// by `swift test` on macOS.

#if os(iOS)
import SwiftUI
import HealthKit
import CryptoKit
import HealthBridgeKit
import OSLog

/// All HealthKit + drain-loop diagnostics flow through this logger.
/// View them in Console.app under subsystem `li.shuyang.healthbridge`,
/// or in Xcode's debug console while the app is running.
private let log = Logger(subsystem: "li.shuyang.healthbridge", category: "auth")

@main
struct HealthBridgeApp: App {
    @StateObject private var coordinator = AppCoordinator()
    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(coordinator)
                // Reactivate the drain loop whenever the scene becomes
                // foreground-active. We do NOT auto-request HealthKit
                // permission here — that runs only on user button tap, so
                // there's always an active host view to present the
                // permission sheet from.
                .onChange(of: scenePhase) { _, phase in
                    coordinator.scenePhaseChanged(phase)
                }
        }
    }
}

@MainActor
final class AppCoordinator: ObservableObject {

    enum AuthState: Equatable {
        case unknown
        case unavailable          // HKHealthStore.isHealthDataAvailable() == false
        case requesting
        case authorized
        case denied(String)
    }

    @Published var status: String = "Tap “Connect to HealthKit” to begin."
    @Published var lastError: String?
    @Published var drainedCount: Int = 0
    @Published var auth: AuthState = .unknown

    private var drainTask: Task<Void, Never>?
    private let store = HKHealthStore()

    // MARK: - Lifecycle

    func scenePhaseChanged(_ phase: ScenePhase) {
        switch phase {
        case .active:
            // If we're already authorised, restart the drain loop on
            // foreground. Don't auto-request auth here — that's a button.
            if case .authorized = auth {
                startDrainLoopIfNeeded()
            }
        case .inactive, .background:
            stopDrainLoop()
        @unknown default:
            break
        }
    }

    // MARK: - Authorisation (called from a button tap)

    func requestAuthorizationFromUser() {
        log.info("requestAuthorizationFromUser tapped; current auth=\(String(describing: self.auth), privacy: .public)")
        guard auth != .requesting else {
            log.info("ignoring tap — already requesting")
            return
        }

        let available = HKHealthStore.isHealthDataAvailable()
        log.info("HKHealthStore.isHealthDataAvailable() = \(available, privacy: .public)")
        guard available else {
            auth = .unavailable
            status = "HealthKit is not available on this device."
            log.error("HealthKit reports unavailable — likely an iOS Simulator without HealthKit, or a non-iPhone device")
            return
        }

        auth = .requesting
        status = "Asking HealthKit for permission…"
        log.info("transitioning to .requesting; will call HKHealthStore.requestAuthorization next")

        Task { @MainActor in
            do {
                try await self.requestAuthorization()
                self.auth = .authorized
                self.status = "Connected — draining relay"
                log.info("requestAuthorization returned successfully; transitioning to .authorized")
                self.startDrainLoopIfNeeded()
            } catch {
                self.auth = .denied("\(error)")
                self.lastError = "\(error)"
                self.status = "HealthKit permission failed: \(error.localizedDescription)"
                log.error("requestAuthorization threw: \(error.localizedDescription, privacy: .public)")
            }
        }
    }

    private func requestAuthorization() async throws {
        // M1 read scope only — step count.
        guard let stepType = HKObjectType.quantityType(forIdentifier: .stepCount) else {
            log.error("HKObjectType.quantityType(forIdentifier:.stepCount) returned nil")
            throw NSError(
                domain: "HealthBridge",
                code: 10,
                userInfo: [NSLocalizedDescriptionKey: "step_count type unavailable"]
            )
        }

        // authorizationStatus is what HealthKit thinks the *write* status is.
        // It does NOT report read status (Apple does this on purpose to avoid
        // leaking the existence of records). Still useful as a hint.
        let preStatus = store.authorizationStatus(for: stepType)
        log.info("pre-call store.authorizationStatus(for: stepCount) = \(preStatus.rawValue, privacy: .public) (\(String(describing: preStatus), privacy: .public))")

        let read: Set<HKObjectType> = [stepType]
        log.info("calling HKHealthStore.requestAuthorization(toShare:[], read:[stepCount]) — sheet should appear now")
        try await store.requestAuthorization(toShare: [], read: read)
        log.info("HKHealthStore.requestAuthorization completed without throwing")

        let postStatus = store.authorizationStatus(for: stepType)
        log.info("post-call store.authorizationStatus(for: stepCount) = \(postStatus.rawValue, privacy: .public) (\(String(describing: postStatus), privacy: .public))")
    }

    // MARK: - Drain loop

    func startDrainLoopIfNeeded() {
        guard drainTask == nil else { return }
        drainTask = Task { @MainActor in
            do {
                try await self.drainLoop()
            } catch is CancellationError {
                // expected on backgrounding
            } catch {
                self.lastError = "\(error)"
                self.status = "Drain stopped: \(error.localizedDescription)"
            }
            self.drainTask = nil
        }
    }

    private func stopDrainLoop() {
        drainTask?.cancel()
        drainTask = nil
        if case .authorized = auth {
            status = "Backgrounded — open the app to keep draining."
        }
    }

    private func drainLoop() async throws {
        // M2: pair_id + session key still come from environment for now;
        // the real pairing UI lands later in M3 and writes them to the
        // consent ledger.
        let env = ProcessInfo.processInfo.environment
        let pairID = env["HEALTHBRIDGE_PAIR"] ?? ""
        let relayURL = URL(string: env["HEALTHBRIDGE_RELAY"] ?? "http://127.0.0.1:8787")!
        guard !pairID.isEmpty else {
            self.status = "Authorised. (No HEALTHBRIDGE_PAIR set in scheme — drain loop idle.)"
            return
        }
        guard let keyHex = env["HEALTHBRIDGE_KEY"],
              let keyBytes = Data(hexString: keyHex),
              keyBytes.count == 32 else {
            self.status = "Authorised. (No valid HEALTHBRIDGE_KEY in scheme — drain loop idle.)"
            return
        }
        let session = JobsSession(key: SymmetricKey(data: keyBytes), pairID: pairID)
        let authToken = env["HEALTHBRIDGE_AUTH_TOKEN"] ?? ""
        let client = RelayClient(baseURL: relayURL, pairID: pairID, authToken: authToken)

        var cursor: Int64 = 0
        while !Task.isCancelled {
            let page = try await client.pollJobs(since: cursor, waitMs: 25_000)
            for jb in page.jobs {
                let job = try session.openJob(jobID: jb.jobID, blob: jb.blob)
                let result = try await self.execute(job: job)
                let blob = try session.sealResult(jobID: job.id, pageIndex: result.pageIndex, result)
                _ = try await client.postResult(jobID: job.id, pageIndex: result.pageIndex, blob: blob)
                self.drainedCount += 1
            }
            if page.nextCursor > cursor {
                cursor = page.nextCursor
            }
        }
    }

    private func execute(job: Job) async throws -> JobResult {
        switch job.kind {
        case .read:
            let payload = try job.decodeReadPayload()
            let samples = try await self.runReadQuery(payload: payload)
            let rr = ReadResult(type: payload.type, samples: samples)
            return JobResult(jobID: job.id, status: .done, result: try .from(rr))
        case .write, .sync:
            return JobResult(
                jobID: job.id,
                status: .failed,
                error: JobError(code: "not_implemented", message: "kind \(job.kind.rawValue) is M3+")
            )
        }
    }

    private func runReadQuery(payload: ReadPayload) async throws -> [Sample] {
        // M1: only step_count, only quantity samples summed by HealthKit's
        // own statistics query. Real type-to-identifier mapping arrives in M3.
        guard payload.type == .stepCount else {
            throw NSError(
                domain: "HealthBridge",
                code: 2,
                userInfo: [NSLocalizedDescriptionKey: "M1 only supports step_count"]
            )
        }
        let stepType = HKObjectType.quantityType(forIdentifier: .stepCount)!
        let predicate = HKQuery.predicateForSamples(withStart: payload.from, end: payload.to)

        return try await withCheckedThrowingContinuation { (cont: CheckedContinuation<[Sample], Error>) in
            let q = HKSampleQuery(
                sampleType: stepType,
                predicate: predicate,
                limit: payload.limit ?? HKObjectQueryNoLimit,
                sortDescriptors: nil
            ) { _, raw, error in
                if let error = error {
                    cont.resume(throwing: error)
                    return
                }
                let samples: [Sample] = (raw ?? []).compactMap { obj in
                    guard let q = obj as? HKQuantitySample else { return nil }
                    return Sample(
                        uuid: q.uuid.uuidString,
                        type: .stepCount,
                        value: q.quantity.doubleValue(for: HKUnit.count()),
                        unit: "count",
                        start: q.startDate,
                        end: q.endDate,
                        source: Source(
                            name: q.sourceRevision.source.name,
                            bundleID: q.sourceRevision.source.bundleIdentifier
                        )
                    )
                }
                cont.resume(returning: samples)
            }
            store.execute(q)
        }
    }
}

struct ContentView: View {
    @EnvironmentObject var coordinator: AppCoordinator

    var body: some View {
        VStack(spacing: 20) {
            Text("HealthBridge").font(.largeTitle).bold()
            Text(coordinator.status)
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
                .padding(.horizontal)

            switch coordinator.auth {
            case .unknown, .denied:
                Button(action: { coordinator.requestAuthorizationFromUser() }) {
                    Text("Connect to HealthKit")
                        .padding(.horizontal, 20)
                        .padding(.vertical, 10)
                }
                .buttonStyle(.borderedProminent)

            case .requesting:
                ProgressView()

            case .unavailable:
                Text("HealthKit is not available on this device.")
                    .foregroundStyle(.red)

            case .authorized:
                Text("Drained \(coordinator.drainedCount) jobs")
                Image(systemName: "checkmark.circle.fill")
                    .foregroundStyle(.green)
                    .imageScale(.large)
            }

            if let err = coordinator.lastError {
                Text(err)
                    .foregroundStyle(.red)
                    .font(.caption)
                    .padding(.horizontal)
            }

            Spacer()

            Text("Keep this screen open for the agent to read your Health data.")
                .multilineTextAlignment(.center)
                .padding()
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .padding()
    }
}
#endif
