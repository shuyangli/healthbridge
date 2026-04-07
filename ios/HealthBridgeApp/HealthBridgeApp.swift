// HealthBridge — the iOS companion app.
//
// This file holds the SwiftUI app entry point and the foreground drain
// loop that polls the relay for pending jobs and executes them against
// HealthKit.
//
// This file imports HealthKit and SwiftUI, so it only builds inside an
// Xcode project targeting iOS 17+. The testable, HealthKit-free portion
// of the app lives in the HealthBridgeKit Swift package alongside this
// directory; that package is what `swift test` exercises on the host.
//
// To set up the Xcode project: see HealthBridgeApp/README.md.

#if os(iOS)
import SwiftUI
import HealthKit
import HealthBridgeKit

@main
struct HealthBridgeApp: App {
    @StateObject private var coordinator = AppCoordinator()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(coordinator)
                .onAppear { coordinator.start() }
                .onChange(of: ScenePhase.active) { _, phase in
                    if phase == .active {
                        coordinator.start()
                    } else {
                        coordinator.stop()
                    }
                }
        }
    }
}

@MainActor
final class AppCoordinator: ObservableObject {
    @Published var status: String = "Idle"
    @Published var lastError: String?
    @Published var drainedCount: Int = 0

    private var drainTask: Task<Void, Never>?
    private let store = HKHealthStore()

    func start() {
        guard drainTask == nil else { return }
        status = "Requesting HealthKit access…"
        drainTask = Task {
            do {
                try await self.requestAuthorization()
                self.status = "Connected — draining relay"
                try await self.drainLoop()
            } catch {
                self.lastError = "\(error)"
                self.status = "Stopped: \(error)"
            }
        }
    }

    func stop() {
        drainTask?.cancel()
        drainTask = nil
        status = "Backgrounded"
    }

    private func requestAuthorization() async throws {
        // M1 read scope only — step count.
        let read: Set<HKObjectType> = [
            HKObjectType.quantityType(forIdentifier: .stepCount)!,
        ]
        try await store.requestAuthorization(toShare: [], read: read)
    }

    private func drainLoop() async throws {
        // M2: pair_id + session key still come from environment for now;
        // the real pairing UI lands later in M2 and writes them to the
        // consent ledger.
        let env = ProcessInfo.processInfo.environment
        let pairID = env["HEALTHBRIDGE_PAIR"] ?? ""
        let relayURL = URL(string: env["HEALTHBRIDGE_RELAY"] ?? "http://127.0.0.1:8787")!
        guard !pairID.isEmpty else {
            throw NSError(domain: "HealthBridge", code: 1, userInfo: [NSLocalizedDescriptionKey: "no pair_id configured"])
        }
        guard let keyHex = env["HEALTHBRIDGE_KEY"], let keyBytes = Data(hexString: keyHex), keyBytes.count == 32 else {
            throw NSError(domain: "HealthBridge", code: 2, userInfo: [NSLocalizedDescriptionKey: "no valid 32-byte session key in HEALTHBRIDGE_KEY"])
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
            throw NSError(domain: "HealthBridge", code: 2, userInfo: [NSLocalizedDescriptionKey: "M1 only supports step_count"])
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
                        source: Source(name: q.sourceRevision.source.name, bundleID: q.sourceRevision.source.bundleIdentifier)
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
            Text(coordinator.status).foregroundStyle(.secondary)
            Text("Drained \(coordinator.drainedCount) jobs")
            if let err = coordinator.lastError {
                Text(err).foregroundStyle(.red).font(.caption)
            }
            Spacer()
            Text("Keep this screen open for the agent to read your Health data.")
                .multilineTextAlignment(.center)
                .padding()
                .font(.footnote)
        }
        .padding()
    }
}
#endif
