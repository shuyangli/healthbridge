// SwiftUI flow that walks the user through scanning the CLI's QR,
// confirming the SAS, and persisting the pair record. The view itself
// is dumb — all of the protocol logic lives in HealthBridgeKit.

#if os(iOS)
import SwiftUI
import AVFoundation
import HealthBridgeKit

@MainActor
final class PairingFlowModel: ObservableObject {
    enum Phase: Equatable {
        case idle                       // initial; user hasn't tapped "Scan" yet
        case requestingCameraPermission
        case scanning
        case decoding                   // QR scanned, talking to relay
        case awaitingConfirmation(PairResult)
        case persisting
        case done(StoredPair)
        case failure(String)
    }

    @Published var phase: Phase = .idle

    /// Called by the parent (AppCoordinator) once a pair is committed
    /// so the drain loop can pick it up.
    var onPaired: ((StoredPair) -> Void)?

    func tapScan() {
        // AVCaptureDevice.authorizationStatus(for:) returns the cached
        // value; requestAccess prompts (or completes synchronously if
        // already granted/denied).
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized:
            phase = .scanning
        case .notDetermined:
            phase = .requestingCameraPermission
            AVCaptureDevice.requestAccess(for: .video) { [weak self] granted in
                Task { @MainActor in
                    guard let self = self else { return }
                    self.phase = granted ? .scanning : .failure("Camera access is required to scan the pairing QR.")
                }
            }
        case .denied, .restricted:
            phase = .failure("Camera access is denied. Enable it for HealthBridge in Settings → Privacy → Camera.")
        @unknown default:
            phase = .failure("Unknown camera authorization state.")
        }
    }

    func handleScanned(_ raw: String) {
        Task { @MainActor in
            self.phase = .decoding
            do {
                let link = try HealthBridgePairing.decodeLink(raw)
                guard let url = URL(string: link.relayURL) else {
                    self.phase = .failure("Pair link contains an invalid relay URL.")
                    return
                }
                let client = RelayClient(baseURL: url, pairID: link.pairID, relaySecret: link.relaySecret ?? "")
                let result = try await HealthBridgePairing.respondPairing(client: client, link: link)
                self.phase = .awaitingConfirmation(result)
            } catch let err as PairingError {
                self.phase = .failure(self.errorMessage(for: err))
            } catch {
                self.phase = .failure("Pairing failed: \(error.localizedDescription)")
            }
        }
    }

    func handleScanError(_ msg: String) {
        phase = .failure(msg)
    }

    func confirmSAS(_ result: PairResult) {
        phase = .persisting
        Task { @MainActor in
            do {
                let stored = StoredPair(from: result)
                try PairStorage.save(stored)
                self.phase = .done(stored)
                self.onPaired?(stored)
            } catch {
                self.phase = .failure("Could not persist the pair: \(error.localizedDescription)")
            }
        }
    }

    func cancelSAS() {
        phase = .idle
    }

    func reset() {
        phase = .idle
    }

    private func errorMessage(for err: PairingError) -> String {
        switch err {
        case .invalidLinkJSON:
            return "That QR doesn't look like a HealthBridge pairing code."
        case .unsupportedProtocolVersion(let v):
            return "This QR is for an unsupported pairing protocol (\(v)). Update HealthBridge or the CLI."
        case .missingLinkFields:
            return "The pairing QR is missing required fields."
        case .badCLIPubHex, .wrongPubkeySize:
            return "The pairing QR is malformed (bad public key)."
        case .relayMissingAuthToken:
            return "The relay did not finalise the pairing. Try again."
        }
    }
}

struct PairingView: View {
    @StateObject var model: PairingFlowModel

    @MainActor
    init(model: PairingFlowModel) {
        _model = StateObject(wrappedValue: model)
    }

    var body: some View {
        VStack(spacing: 20) {
            switch model.phase {
            case .idle:
                idleView
            case .requestingCameraPermission:
                ProgressView("Requesting camera access…")
            case .scanning:
                scanningView
            case .decoding:
                ProgressView("Talking to the relay…")
            case .awaitingConfirmation(let r):
                confirmationView(result: r)
            case .persisting:
                ProgressView("Saving pair…")
            case .done(let pair):
                doneView(pair: pair)
            case .failure(let msg):
                failureView(message: msg)
            }
        }
        .padding()
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    private var idleView: some View {
        VStack(spacing: 16) {
            Image(systemName: "qrcode.viewfinder")
                .resizable()
                .scaledToFit()
                .frame(width: 100, height: 100)
                .foregroundStyle(.secondary)
            Text("Pair with your Mac")
                .font(.title2).bold()
            Text("Run `healthbridge pair` on your Mac. It will print a QR code in the terminal — point your iPhone at the screen.")
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
                .padding(.horizontal)
            Button(action: { model.tapScan() }) {
                Label("Scan QR", systemImage: "camera.viewfinder")
                    .padding(.horizontal, 20)
                    .padding(.vertical, 10)
            }
            .buttonStyle(.borderedProminent)
        }
    }

    private var scanningView: some View {
        VStack(spacing: 12) {
            QRScannerView(
                onScanned: { model.handleScanned($0) },
                onError: { model.handleScanError($0) }
            )
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .clipShape(RoundedRectangle(cornerRadius: 12))
            Text("Aim the camera at the QR code your Mac is showing.")
                .font(.footnote)
                .foregroundStyle(.secondary)
            Button("Cancel", action: { model.reset() })
        }
    }

    private func confirmationView(result: PairResult) -> some View {
        VStack(spacing: 16) {
            Text("Confirm pairing").font(.title2).bold()
            Text("Make sure your Mac shows the same six-digit code:")
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
            Text(result.sas)
                .font(.system(size: 48, weight: .bold, design: .monospaced))
                .padding(.vertical, 8)
            Text("Pair ID: \(result.pairID)")
                .font(.caption.monospaced())
                .foregroundStyle(.secondary)
            HStack(spacing: 16) {
                Button("Cancel", role: .cancel) { model.cancelSAS() }
                Button("Codes Match") { model.confirmSAS(result) }
                    .buttonStyle(.borderedProminent)
            }
        }
    }

    private func doneView(pair: StoredPair) -> some View {
        VStack(spacing: 12) {
            Image(systemName: "checkmark.seal.fill")
                .resizable().scaledToFit()
                .frame(width: 80, height: 80)
                .foregroundStyle(.green)
            Text("Paired").font(.title2).bold()
            Text("Your Mac can now read and write Health data via the relay.")
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
            Text("Pair ID: \(pair.pairID)")
                .font(.caption.monospaced())
                .foregroundStyle(.secondary)
        }
    }

    private func failureView(message: String) -> some View {
        VStack(spacing: 12) {
            Image(systemName: "exclamationmark.triangle.fill")
                .resizable().scaledToFit()
                .frame(width: 60, height: 60)
                .foregroundStyle(.orange)
            Text("Pairing failed").font(.headline)
            Text(message)
                .multilineTextAlignment(.center)
                .foregroundStyle(.secondary)
                .padding(.horizontal)
            Button("Try again") { model.reset() }
                .buttonStyle(.borderedProminent)
        }
    }
}

#endif
