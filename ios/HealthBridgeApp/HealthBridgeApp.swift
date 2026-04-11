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
import UserNotifications
import OSLog

/// All HealthKit + drain-loop diagnostics flow through this logger.
/// View them in Console.app under subsystem `li.shuyang.healthbridge`,
/// or in Xcode's debug console while the app is running.
private let log = Logger(subsystem: "li.shuyang.healthbridge", category: "auth")

@main
struct HealthBridgeApp: App {
    @UIApplicationDelegateAdaptor private var appDelegate: AppDelegate
    @StateObject private var coordinator = AppCoordinator.shared
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

// MARK: - AppDelegate (push notification plumbing)

/// UIKit AppDelegate adapter for receiving APNs device tokens and
/// silent push callbacks. SwiftUI has no native equivalent for
/// `didRegisterForRemoteNotificationsWithDeviceToken` or
/// `didReceiveRemoteNotification:fetchCompletionHandler:`.
@MainActor
final class AppDelegate: NSObject, UIApplicationDelegate {
    /// Use the shared coordinator so silent-push wakes still have
    /// access to pairing and auth state even if no SwiftUI view has
    /// appeared in this process yet.
    private let coordinator = AppCoordinator.shared

    func application(
        _ application: UIApplication,
        didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data
    ) {
        let hex = deviceToken.map { String(format: "%02x", $0) }.joined()
        log.info("APNs device token received (\(hex.prefix(8), privacy: .public)…)")
        Task { @MainActor in
            await self.coordinator.didReceiveDeviceToken(hex)
        }
    }

    func application(
        _ application: UIApplication,
        didFailToRegisterForRemoteNotificationsWithError error: Error
    ) {
        log.error("APNs registration failed: \(error.localizedDescription, privacy: .public)")
    }

    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        // Request notification permission early so alert pushes from the
        // relay display immediately. The user sees a one-time system
        // prompt; if they decline, pushes still wake the app via
        // content-available but won't show a banner.
        UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound]) { granted, error in
            if let error = error {
                log.error("notification auth error: \(error.localizedDescription, privacy: .public)")
            }
            log.info("notification auth granted=\(granted, privacy: .public)")
        }
        return true
    }

    /// Push handler — iOS calls this when a push with `content-available: 1`
    /// arrives (whether alert or silent). We kick the drain loop and call
    /// the completion handler when the first poll returns so iOS credits
    /// us for finishing fast.
    func application(
        _ application: UIApplication,
        didReceiveRemoteNotification userInfo: [AnyHashable: Any],
        fetchCompletionHandler completionHandler: @escaping (UIBackgroundFetchResult) -> Void
    ) {
        Task { @MainActor in
            let drained = await self.coordinator.drainOnPush()
            completionHandler(drained ? .newData : .noData)
        }
    }
}

@MainActor
final class AppCoordinator: ObservableObject {
    static let shared = AppCoordinator()

    enum AuthState: Equatable {
        case unknown
        case unavailable          // HKHealthStore.isHealthDataAvailable() == false
        case requesting
        case authorized
        case denied(String)
    }

    @Published var status: String = "Starting up…"
    @Published var lastError: String?
    @Published var drainedCount: Int = 0
    @Published var auth: AuthState = .unknown
    /// The currently-paired Mac, if any. nil = no pair on disk yet,
    /// in which case the UI shows the pairing flow.
    @Published var pair: StoredPair?

    private var drainTask: Task<Void, Never>?
    /// True while drainOnPush() is actively processing jobs. Prevents
    /// startDrainLoopIfNeeded() and subsequent push callbacks from
    /// launching a concurrent drain over the same cursor.
    private var isDrainingOnPush = false
    /// Set when a push arrives while isDrainingOnPush is true. The
    /// active drain will do another pass after finishing so jobs
    /// enqueued between pushes are picked up promptly.
    private var pushDrainRequested = false
    private let store = HKHealthStore()
    private let authStateStore = AuthStateStore()

    init() {
        self.pair = PairStorage.load()
        self.auth = authStateStore.load()
    }

    // MARK: - Lifecycle

    func scenePhaseChanged(_ phase: ScenePhase) {
        switch phase {
        case .active:
            // First-launch: as soon as the scene goes active, ask HealthKit
            // for permission. We deliberately do NOT do this from .onAppear
            // — the system permission sheet will not present unless the
            // scene is fully active, which is exactly what this callback
            // signals. Once the user has answered (authorized or denied)
            // we never re-prompt automatically; the .denied UI shows a
            // retry button.
            if case .unknown = auth {
                requestAuthorizationFromUser()
            }
            // Re-register on every foreground in case the token rotated.
            if pair != nil {
                UIApplication.shared.registerForRemoteNotifications()
            }
            if case .authorized = auth, pair != nil {
                startDrainLoopIfNeeded()
            }
        case .inactive, .background:
            stopDrainLoop()
        @unknown default:
            break
        }
    }

    // MARK: - Pairing

    func pairingCompleted(_ stored: StoredPair) {
        log.info("pairingCompleted pair_id=\(stored.pairID, privacy: .public)")
        self.pair = stored
        // Register for silent push so the relay can wake us on new jobs.
        UIApplication.shared.registerForRemoteNotifications()
        // If HealthKit is already authorised, kick off the drain loop
        // immediately. Otherwise the user still needs to tap "Connect
        // to HealthKit" first.
        if case .authorized = auth {
            startDrainLoopIfNeeded()
        }
    }

    func unpair() {
        stopDrainLoop()
        try? PairStorage.clear()
        self.pair = nil
        self.status = "Unpaired. Run `healthbridge pair` on your Mac to pair again."
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
                self.authStateStore.save(.authorized)
                self.status = self.pair == nil
                    ? "Connected — pair with your Mac to start draining."
                    : "Connected — draining relay"
                log.info("requestAuthorization returned successfully; transitioning to .authorized")
                if self.pair != nil {
                    self.startDrainLoopIfNeeded()
                }
            } catch {
                self.auth = .denied("\(error)")
                self.authStateStore.save(self.auth)
                self.lastError = "\(error)"
                self.status = "HealthKit permission failed: \(error.localizedDescription)"
                log.error("requestAuthorization threw: \(error.localizedDescription, privacy: .public)")
            }
        }
    }

    private func requestAuthorization() async throws {
        let read = HealthKitMapping.readScopes()
        let write = HealthKitMapping.writeScopes()
        log.info("requesting auth: \(read.count, privacy: .public) read scopes, \(write.count, privacy: .public) write scopes")
        log.info("calling HKHealthStore.requestAuthorization — sheet should appear now")
        try await store.requestAuthorization(toShare: write, read: read)
        log.info("HKHealthStore.requestAuthorization completed without throwing")
    }

    /// The APNs environment matching the aps-environment entitlement.
    /// Debug builds (Xcode → device) use sandbox; Release builds
    /// (App Store / TestFlight) use production. This must match the
    /// entitlement so we hit the same APNs server that issued the
    /// device token.
    private static func apnsEnvironment() -> String {
        #if DEBUG
        let env = "development"
        #else
        let env = "production"
        #endif
        log.info("APNs environment: \(env, privacy: .public)")
        return env
    }

    // MARK: - Drain loop

    func startDrainLoopIfNeeded() {
        guard drainTask == nil, !isDrainingOnPush else { return }
        guard let pair = pair else {
            log.info("startDrainLoopIfNeeded: no pair — skipping")
            return
        }
        drainTask = Task { @MainActor in
            do {
                try await self.drainLoop(pair: pair)
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
        if case .authorized = auth, pair != nil {
            status = "Backgrounded — waiting for push."
        }
    }

    // MARK: - Push notifications

    /// Called by AppDelegate when APNs hands us a device token.
    /// Posts it to the relay so the relay can send us silent pushes.
    func didReceiveDeviceToken(_ tokenHex: String) async {
        guard let pair = self.pair,
              let relayURL = URL(string: pair.relayURL) else {
            log.error("didReceiveDeviceToken skipped: missing pair or relayURL")
            return
        }
        let client = RelayClient(
            baseURL: relayURL,
            pairID: pair.pairID,
            authToken: pair.authToken
        )
        let env = Self.apnsEnvironment()
        do {
            try await client.registerDeviceToken(tokenHex, environment: env)
            log.info("device token registered with relay")
        } catch {
            log.error("failed to register device token: \(error.localizedDescription, privacy: .public)")
        }
    }

    /// Called by AppDelegate on push notification. Runs one drain pass
    /// (non-long-poll) and returns whether any jobs were processed.
    /// The caller passes the result to the UIKit completion handler
    /// so iOS knows whether we fetched anything.
    func drainOnPush() async -> Bool {
        guard case .authorized = auth else {
            log.info("drainOnPush skipped: auth=\(String(describing: self.auth), privacy: .public)")
            return false
        }
        guard let pair = self.pair else {
            log.info("drainOnPush skipped: no pair")
            return false
        }
        // If the foreground drain loop is running, it will pick up the
        // new job via its long-poll.
        if drainTask != nil {
            log.info("drainOnPush: foreground drain loop active")
            return true
        }
        // If another push drain is active, request another pass so
        // jobs enqueued between pushes are picked up.
        if isDrainingOnPush {
            log.info("drainOnPush: coalescing with active push drain")
            pushDrainRequested = true
            return true
        }
        // We own the drain. Set the flag so concurrent pushes and
        // startDrainLoopIfNeeded() don't start a second drain.
        isDrainingOnPush = true
        defer {
            isDrainingOnPush = false
            pushDrainRequested = false
            // If the app came to foreground while we were draining
            // (user tapped the notification), start the long-poll
            // drain loop now that the one-shot drain is done.
            startDrainLoopIfNeeded()
        }

        guard let relayURL = URL(string: pair.relayURL),
              let keyBytes = Data(hexString: pair.sessionKeyHex),
              keyBytes.count == 32 else { return false }
        let session = JobsSession(
            key: CryptoKit.SymmetricKey(data: keyBytes),
            pairID: pair.pairID
        )
        let client = RelayClient(
            baseURL: relayURL,
            pairID: pair.pairID,
            authToken: pair.authToken
        )
        var cursor = pair.lastDrainedMs
        if cursor == 0 {
            cursor = Int64(Date().timeIntervalSince1970 * 1000)
            self.advanceCursor(to: cursor)
        }
        var everDrained = false
        // Loop: drain once, then again if more pushes arrived while
        // we were processing. This coalesces N rapid pushes into at
        // most 2 passes instead of N concurrent drains.
        repeat {
            pushDrainRequested = false
            do {
                let page = try await client.pollJobs(sinceMs: cursor, waitMs: 0)
                for jb in page.jobs {
                    let outcome = await self.processOneJob(
                        jb: jb, session: session, client: client
                    )
                    switch outcome {
                    case .processed, .skipped:
                        cursor = jb.enqueuedAt
                        self.advanceCursor(to: jb.enqueuedAt)
                        everDrained = true
                    case .transient:
                        break
                    }
                }
                if page.nextCursorMs > cursor {
                    self.advanceCursor(to: page.nextCursorMs)
                }
            } catch {
                log.error("drainOnPush failed: \(error.localizedDescription, privacy: .public)")
                return everDrained
            }
        } while pushDrainRequested
        return everDrained
    }

    private func drainLoop(pair startingPair: StoredPair) async throws {
        guard let relayURL = URL(string: startingPair.relayURL) else {
            self.status = "Invalid relay URL in stored pair."
            return
        }
        guard let keyBytes = Data(hexString: startingPair.sessionKeyHex), keyBytes.count == 32 else {
            self.status = "Invalid session key in stored pair — re-pair from your Mac."
            return
        }
        let session = JobsSession(key: SymmetricKey(data: keyBytes), pairID: startingPair.pairID)
        let client = RelayClient(baseURL: relayURL, pairID: startingPair.pairID, authToken: startingPair.authToken)

        // Resume from the wall-clock timestamp of the most recent job
        // we've already drained, persisted in the pair record. This
        // makes a foreground/background cycle (or a process restart)
        // idempotent — without it, we'd re-execute every job in the
        // relay's mailbox and double-save HealthKit samples on every
        // wake.
        //
        // First-run case: lastDrainedMs == 0 means this is a freshly
        // installed binary or a freshly paired device. Seed the cursor
        // to "now" so historical wedged jobs in the relay (e.g. left
        // over from a botched migration on the relay side) are skipped
        // instead of re-executed. Anything legitimately in flight at
        // this exact instant is rare; the user can re-issue.
        var cursor: Int64 = startingPair.lastDrainedMs
        if cursor == 0 {
            cursor = Int64(Date().timeIntervalSince1970 * 1000)
            log.info("drainLoop first-run cursor seeded to now=\(cursor, privacy: .public)")
            self.advanceCursor(to: cursor)
        }
        log.info("drainLoop start cursor=\(cursor, privacy: .public)")
        while !Task.isCancelled {
            let page = try await client.pollJobs(sinceMs: cursor, waitMs: 25_000)
            for jb in page.jobs {
                let outcome = await self.processOneJob(jb: jb, session: session, client: client)
                switch outcome {
                case .processed, .skipped:
                    // Persist after every job, not just at the end of
                    // the page, so a crash mid-page doesn't replay
                    // earlier jobs.
                    cursor = jb.enqueuedAt
                    self.advanceCursor(to: jb.enqueuedAt)
                case .transient(let err):
                    // Network/5xx failure on the relay POST. Don't
                    // advance — the next loop iteration will retry the
                    // same job. Re-throw to bubble up so the parent
                    // task either retries or surfaces the error.
                    throw err
                }
            }
            if page.nextCursorMs > cursor {
                cursor = page.nextCursorMs
                self.advanceCursor(to: page.nextCursorMs)
            }
        }
    }

    /// Outcome of one drain iteration's per-job step. Distinguishes
    /// "advance the cursor" from "stop and retry on next foreground".
    private enum JobOutcome {
        /// Result was successfully sealed and posted (or an equivalent
        /// fallback was posted on a permanent error). Cursor advances.
        case processed
        /// Job could not be processed at all (e.g. decryption failed
        /// because of a stale session) but is unrecoverable, so the
        /// cursor advances anyway to avoid wedging on it.
        case skipped
        /// Network or 5xx during the post. Cursor does NOT advance;
        /// the loop bubbles up the error and retries on next pull.
        case transient(Error)
    }

    /// Decide whether a result should persist into the relay's
    /// snapshot. Read/sync results are ephemeral — their blobs can be
    /// large and the CLI is normally still long-polling, so the relay
    /// keeps them in memory only and lets them vanish on a Durable
    /// Object eviction. Write/profile results are tiny and the CLI
    /// may legitimately come back later, so they're persisted. A
    /// failed status is always persisted regardless of kind so the
    /// agent never silently loses an error report.
    private static func shouldPersistResult(jobKind: JobKind, status: ResultStatus) -> Bool {
        if status == .failed { return true }
        switch jobKind {
        case .read, .sync:
            return false
        case .write, .profile:
            return true
        }
    }

    /// Wire-format error codes the relay returns when a result blob
    /// cannot be accepted at all. The iOS side treats these as
    /// "permanent" — replace the result with a tiny failure payload,
    /// post that, and advance the cursor. Anything not in this set is
    /// transient (network, 5xx) and the cursor stays put.
    ///
    /// Keep in sync with relay/src/handler.ts and mailbox.ts.
    private static let permanentPostErrorCodes: Set<String> = [
        "blob_too_large",
        "body_too_large",
        "invalid_blob",
        "invalid_job_id",
        "invalid_page_index",
        "too_many_result_pages",
    ]

    private func processOneJob(
        jb: RelayClient.JobBlob,
        session: JobsSession,
        client: RelayClient
    ) async -> JobOutcome {
        // Decryption / decoding failures are unrecoverable for this
        // specific blob — the most common cause is a stale ciphertext
        // from a previous pair. Skip and advance.
        let job: Job
        do {
            job = try session.openJob(jobID: jb.jobID, blob: jb.blob)
        } catch {
            log.error("openJob failed for seq=\(jb.seq, privacy: .public): \(error.localizedDescription, privacy: .public) — skipping")
            return .skipped
        }

        // Per-job execution failures (e.g. unsupported sample type)
        // must NOT abort the drain loop — convert them into a failed
        // JobResult so the relay reports the error to the agent and
        // the cursor still advances.
        let result: JobResult
        do {
            result = try await self.execute(job: job)
        } catch {
            log.error("execute failed for job \(job.id, privacy: .public): \(error.localizedDescription, privacy: .public)")
            result = JobResult(
                jobID: job.id,
                status: .failed,
                error: JobError(code: "execute_failed", message: error.localizedDescription)
            )
        }

        // Read/sync results have potentially-large blobs and a CLI
        // that's normally still long-polling at the other end →
        // ephemeral. Write/profile/(failed) results have tiny blobs
        // that the CLI may legitimately come back to retrieve →
        // persistent. The relay strips ephemeral entries from its
        // snapshot so they vanish on a Durable Object eviction.
        let persistent = Self.shouldPersistResult(jobKind: job.kind, status: result.status)

        do {
            let blob = try session.sealResult(jobID: job.id, pageIndex: result.pageIndex, result)
            _ = try await client.postResult(
                jobID: job.id,
                pageIndex: result.pageIndex,
                blob: blob,
                persistent: persistent
            )
            self.drainedCount += 1
            return .processed
        } catch let err as RelayClient.RelayError where err.code == "duplicate_result_page" {
            // We crashed (or this loop restarted) between executing
            // and persisting the cursor for this seq. The relay
            // already has the result page; advance and move on.
            log.info("tolerating duplicate_result_page for seq=\(jb.seq, privacy: .public) — advancing cursor")
            return .processed
        } catch let err as RelayClient.RelayError where Self.permanentPostErrorCodes.contains(err.code) {
            // The result blob can't be posted as-is — most often
            // because it exceeds the relay's MAX_BLOB_BYTES cap.
            // Without this branch the iOS drain loop would re-fetch
            // and re-execute the same poison job on every foreground
            // forever. Replace the oversized result with a tiny
            // failure payload that comfortably fits and try once more
            // so the agent sees something concrete instead of
            // permanently `pending`.
            log.error("permanent post failure for seq=\(jb.seq, privacy: .public) (\(err.code, privacy: .public)) — posting fallback failure result")
            let fallback = JobResult(
                jobID: job.id,
                status: .failed,
                error: JobError(
                    code: err.code,
                    message: "result was too large for the relay; narrow --from or pass --limit"
                )
            )
            do {
                let fallbackBlob = try session.sealResult(jobID: job.id, pageIndex: 0, fallback)
                // Failure fallbacks are tiny and the CLI almost
                // always wants to see them — persist.
                _ = try await client.postResult(
                    jobID: job.id,
                    pageIndex: 0,
                    blob: fallbackBlob,
                    persistent: true
                )
            } catch {
                log.error("fallback post also failed for seq=\(jb.seq, privacy: .public): \(error.localizedDescription, privacy: .public) — advancing anyway to unwedge")
            }
            return .processed
        } catch {
            // Network error, 5xx, anything else. Treat as transient.
            log.error("transient post failure for seq=\(jb.seq, privacy: .public): \(error.localizedDescription, privacy: .public)")
            return .transient(error)
        }
    }

    /// Update the in-memory + on-disk drain cursor for the active
    /// pair. The cursor is a Unix-millis timestamp (the enqueued_at
    /// of the most recent job we drained). Failure to persist is
    /// logged but non-fatal — the next successful save will catch up.
    private func advanceCursor(to ms: Int64) {
        guard var p = self.pair, ms > p.lastDrainedMs else { return }
        p.lastDrainedMs = ms
        self.pair = p
        do {
            try PairStorage.save(p)
        } catch {
            log.error("failed to persist drain cursor: \(error.localizedDescription, privacy: .public)")
        }
    }

    private func execute(job: Job) async throws -> JobResult {
        log.info("execute job \(job.id, privacy: .public) kind=\(job.kind.rawValue, privacy: .public)")
        switch job.kind {
        case .read:
            let payload = try job.decodeReadPayload()
            let samples = try await self.runReadQuery(payload: payload)
            let rr = ReadResult(type: payload.type, samples: samples)
            return JobResult(jobID: job.id, status: .done, result: try .from(rr))
        case .write:
            let payload = try job.decodeWritePayload()
            let uuid = try await self.runWrite(payload: payload)
            let wr = WriteResult(uuid: uuid)
            return JobResult(jobID: job.id, status: .done, result: try .from(wr))
        case .sync:
            return JobResult(
                jobID: job.id,
                status: .failed,
                error: JobError(code: "not_implemented", message: "kind sync is M4+")
            )
        case .profile:
            let payload = try job.decodeProfilePayload()
            do {
                let value = try HealthKitMapping.characteristicValue(payload.field, store: self.store)
                let pr = ProfileResult(field: payload.field, value: value)
                return JobResult(jobID: job.id, status: .done, result: try .from(pr))
            } catch {
                return JobResult(
                    jobID: job.id,
                    status: .failed,
                    error: JobError(code: "healthkit_error", message: error.localizedDescription)
                )
            }
        }
    }

    // MARK: - Read

    private func runReadQuery(payload: ReadPayload) async throws -> [Sample] {
        switch payload.type {
        case .sleepAnalysis:
            return try await runSleepReadQuery(payload: payload)
        case .workout:
            return try await runWorkoutReadQuery(payload: payload)
        default:
            return try await runQuantityReadQuery(payload: payload)
        }
    }

    /// Newest-first sort so that `--limit N` returns the N most recent
    /// samples in the window. Used by every read query path.
    private static let sortNewestFirst = NSSortDescriptor(
        key: HKSampleSortIdentifierStartDate,
        ascending: false
    )

    /// Hard cap on samples per read result. The relay rejects any
    /// blob over MAX_BLOB_BYTES (1 MiB at the time of writing — see
    /// relay/src/mailbox.ts). With ChaCha20-Poly1305 + base64 + JSON
    /// envelope overhead each sample averages 350-450 bytes
    /// post-encryption, so 2000 samples comfortably fits under the
    /// 1 MiB cap with headroom for source metadata. The newest-first
    /// sort means truncation drops the OLDEST samples first; agents
    /// that need older data should narrow `--from` or pass `--limit`.
    private static let maxSamplesPerReadResult = 2000

    /// Resolve the HKSampleQuery limit from the wire payload. A nil
    /// or zero `payload.limit` means "as many as fit in one result
    /// blob"; an explicit limit is honoured up to the hard cap.
    private static func resolveReadLimit(_ requested: Int?) -> Int {
        guard let r = requested, r > 0 else {
            return maxSamplesPerReadResult
        }
        return min(r, maxSamplesPerReadResult)
    }

    private func runQuantityReadQuery(payload: ReadPayload) async throws -> [Sample] {
        guard let qType = HealthKitMapping.quantityType(for: payload.type) else {
            throw NSError(
                domain: "HealthBridge",
                code: 2,
                userInfo: [NSLocalizedDescriptionKey: "unsupported sample type \(payload.type.rawValue)"]
            )
        }
        let unit = HealthKitMapping.unit(from: HealthKitMapping.canonicalUnit(for: payload.type))
        // HKQuantity.doubleValue(for:) raises an Objective-C
        // NSException (NOT a Swift error) when the unit is
        // incompatible with the type — and ObjC exceptions abort the
        // process under Swift's exception model. We pre-flight the
        // compatibility check here so a wrong catalog unit becomes a
        // structured Swift error → failed JobResult → cursor advance,
        // instead of crashing the entire HealthBridge app on the next
        // drain pass.
        guard qType.is(compatibleWith: unit) else {
            throw NSError(
                domain: "HealthBridge",
                code: 5,
                userInfo: [NSLocalizedDescriptionKey:
                    "catalog unit '\(HealthKitMapping.canonicalUnit(for: payload.type))' is not compatible with HKQuantityType \(qType.identifier) — please file an issue"]
            )
        }
        let predicate = HKQuery.predicateForSamples(withStart: payload.from, end: payload.to)

        return try await withCheckedThrowingContinuation { (cont: CheckedContinuation<[Sample], Error>) in
            let q = HKSampleQuery(
                sampleType: qType,
                predicate: predicate,
                limit: Self.resolveReadLimit(payload.limit),
                sortDescriptors: [Self.sortNewestFirst]
            ) { _, raw, error in
                if let error = error {
                    cont.resume(throwing: error)
                    return
                }
                let samples: [Sample] = (raw ?? []).compactMap { obj in
                    guard let q = obj as? HKQuantitySample else { return nil }
                    return Sample(
                        uuid: q.uuid.uuidString,
                        type: payload.type,
                        value: q.quantity.doubleValue(for: unit),
                        unit: HealthKitMapping.canonicalUnit(for: payload.type),
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

    /// Read sleep_analysis samples. Each `HKCategorySample` becomes one
    /// wire `Sample` whose `value` is the duration in seconds and whose
    /// `metadata["state"]` is the snake_case sleep state name (e.g.
    /// "asleep_deep", "in_bed").
    private func runSleepReadQuery(payload: ReadPayload) async throws -> [Sample] {
        guard let cType = HKObjectType.categoryType(forIdentifier: .sleepAnalysis) else {
            throw NSError(
                domain: "HealthBridge",
                code: 4,
                userInfo: [NSLocalizedDescriptionKey: "sleep_analysis category type unavailable on this OS"]
            )
        }
        let predicate = HKQuery.predicateForSamples(withStart: payload.from, end: payload.to)
        return try await withCheckedThrowingContinuation { (cont: CheckedContinuation<[Sample], Error>) in
            let q = HKSampleQuery(
                sampleType: cType,
                predicate: predicate,
                limit: Self.resolveReadLimit(payload.limit),
                sortDescriptors: [Self.sortNewestFirst]
            ) { _, raw, error in
                if let error = error {
                    cont.resume(throwing: error)
                    return
                }
                let samples: [Sample] = (raw ?? []).compactMap { obj in
                    guard let cs = obj as? HKCategorySample else { return nil }
                    let state = HealthKitMapping.sleepStateName(forRawValue: cs.value)
                    let duration = cs.endDate.timeIntervalSince(cs.startDate)
                    return Sample(
                        uuid: cs.uuid.uuidString,
                        type: payload.type,
                        value: duration,
                        unit: "s",
                        start: cs.startDate,
                        end: cs.endDate,
                        metadata: ["state": AnyCodable(state)],
                        source: Source(
                            name: cs.sourceRevision.source.name,
                            bundleID: cs.sourceRevision.source.bundleIdentifier
                        )
                    )
                }
                cont.resume(returning: samples)
            }
            store.execute(q)
        }
    }

    /// Read workouts. Each `HKWorkout` becomes one wire `Sample` whose
    /// `value` is the workout duration in seconds and whose metadata
    /// carries the activity type and (optional) totals.
    private func runWorkoutReadQuery(payload: ReadPayload) async throws -> [Sample] {
        let predicate = HKQuery.predicateForSamples(withStart: payload.from, end: payload.to)
        return try await withCheckedThrowingContinuation { (cont: CheckedContinuation<[Sample], Error>) in
            let q = HKSampleQuery(
                sampleType: .workoutType(),
                predicate: predicate,
                limit: Self.resolveReadLimit(payload.limit),
                sortDescriptors: [Self.sortNewestFirst]
            ) { _, raw, error in
                if let error = error {
                    cont.resume(throwing: error)
                    return
                }
                let samples: [Sample] = (raw ?? []).compactMap { obj in
                    guard let w = obj as? HKWorkout else { return nil }
                    var meta: [String: AnyCodable] = [
                        "activity_type": AnyCodable(
                            HealthKitMapping.workoutActivityName(for: w.workoutActivityType)
                        ),
                    ]
                    if let kcal = w.totalEnergyBurned?.doubleValue(for: .kilocalorie()) {
                        meta["total_energy_burned_kcal"] = AnyCodable(kcal)
                    }
                    if let meters = w.totalDistance?.doubleValue(for: .meter()) {
                        meta["total_distance_m"] = AnyCodable(meters)
                    }
                    return Sample(
                        uuid: w.uuid.uuidString,
                        type: payload.type,
                        value: w.duration,
                        unit: "s",
                        start: w.startDate,
                        end: w.endDate,
                        metadata: meta,
                        source: Source(
                            name: w.sourceRevision.source.name,
                            bundleID: w.sourceRevision.source.bundleIdentifier
                        )
                    )
                }
                cont.resume(returning: samples)
            }
            store.execute(q)
        }
    }

    // MARK: - Write

    private func runWrite(payload: WritePayload) async throws -> String {
        let s = payload.sample
        guard let qType = HealthKitMapping.quantityType(for: s.type) else {
            throw NSError(
                domain: "HealthBridge",
                code: 3,
                userInfo: [NSLocalizedDescriptionKey: "unsupported write type \(s.type.rawValue)"]
            )
        }
        let unit = HealthKitMapping.unit(from: s.unit)
        let quantity = HKQuantity(unit: unit, doubleValue: s.value)
        let hkSample = HKQuantitySample(
            type: qType,
            quantity: quantity,
            start: s.start,
            end: s.end
        )
        log.info("saving HKQuantitySample type=\(s.type.rawValue, privacy: .public) value=\(s.value, privacy: .public) unit=\(s.unit, privacy: .public)")
        try await store.save(hkSample)
        log.info("saved sample uuid=\(hkSample.uuid.uuidString, privacy: .public)")
        return hkSample.uuid.uuidString
    }
}

private struct AuthStateStore {
    private enum PersistedAuthState: String {
        case unknown
        case authorized
        case denied
    }

    private let defaults = UserDefaults.standard
    private let key = "healthbridge.healthkit_auth_state"

    func load() -> AppCoordinator.AuthState {
        guard let raw = defaults.string(forKey: key),
              let persisted = PersistedAuthState(rawValue: raw) else {
            return .unknown
        }
        switch persisted {
        case .unknown:
            return .unknown
        case .authorized:
            return .authorized
        case .denied:
            return .denied("HealthKit permission was previously denied.")
        }
    }

    func save(_ state: AppCoordinator.AuthState) {
        let persisted: PersistedAuthState
        switch state {
        case .unknown, .requesting, .unavailable:
            persisted = .unknown
        case .authorized:
            persisted = .authorized
        case .denied:
            persisted = .denied
        }
        defaults.set(persisted.rawValue, forKey: key)
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
            case .unknown:
                // The auto-prompt fires from scenePhaseChanged the moment
                // the scene goes active; .unknown is just the brief
                // window before that callback runs.
                ProgressView()

            case .denied:
                // The user explicitly declined; offer a retry button
                // rather than re-prompting on every foreground.
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
                if coordinator.pair == nil {
                    PairingView(model: makePairingModel())
                        .frame(maxHeight: .infinity)
                } else {
                    pairedSummary
                }
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

    private var pairedSummary: some View {
        VStack(spacing: 8) {
            Image(systemName: "checkmark.circle.fill")
                .foregroundStyle(.green)
                .imageScale(.large)
            Text("Drained \(coordinator.drainedCount) jobs")
            if let pair = coordinator.pair {
                Text("Paired with \(pair.pairID)")
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                Button("Unpair", role: .destructive) {
                    coordinator.unpair()
                }
                .padding(.top, 4)
            }
        }
    }

    private func makePairingModel() -> PairingFlowModel {
        let m = PairingFlowModel()
        m.onPaired = { stored in
            coordinator.pairingCompleted(stored)
        }
        return m
    }
}
#endif
