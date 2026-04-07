// QRScannerView is a thin SwiftUI wrapper around AVFoundation that
// streams QR-code metadata strings out via the `onScanned` closure. It
// is the only place in the iOS app that touches AVFoundation.
//
// We use AVCaptureMetadataOutput rather than the newer DataScanner API
// because:
//   1. DataScanner only works on devices with the Apple Neural Engine
//      (iPhone XS and newer); QR scanning is a one-time pairing flow,
//      and we'd rather support the same hardware list as HealthKit.
//   2. The metadata output API has been stable since iOS 7 and the
//      App Review story for it is well-understood.
//
// The scanner stops itself the first time it sees a QR string, so the
// caller's `onScanned` closure runs at most once per presented view.

#if os(iOS)
import SwiftUI
import AVFoundation

struct QRScannerView: UIViewControllerRepresentable {
    /// Called on the main actor with the decoded QR string. Fires once.
    let onScanned: (String) -> Void
    /// Called when the camera could not be initialised (no permission,
    /// no camera, simulator, etc.). UI shows a fallback.
    let onError: (String) -> Void

    func makeUIViewController(context: Context) -> ScannerViewController {
        let vc = ScannerViewController()
        vc.onScanned = { code in
            // Hop to the main actor before calling SwiftUI state mutators.
            DispatchQueue.main.async { onScanned(code) }
        }
        vc.onError = { msg in
            DispatchQueue.main.async { onError(msg) }
        }
        return vc
    }

    func updateUIViewController(_ uiViewController: ScannerViewController, context: Context) {}
}

final class ScannerViewController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    var onScanned: ((String) -> Void)?
    var onError: ((String) -> Void)?

    private let session = AVCaptureSession()
    private var previewLayer: AVCaptureVideoPreviewLayer?
    private var didReport = false

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black
        configureSession()
    }

    override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        if !session.isRunning {
            // Camera I/O blocks the main thread; AVFoundation expects
            // startRunning to happen off-main on modern iOS.
            DispatchQueue.global(qos: .userInitiated).async { [weak self] in
                self?.session.startRunning()
            }
        }
    }

    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        if session.isRunning {
            DispatchQueue.global(qos: .userInitiated).async { [weak self] in
                self?.session.stopRunning()
            }
        }
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        previewLayer?.frame = view.bounds
    }

    private func configureSession() {
        guard let device = AVCaptureDevice.default(for: .video) else {
            onError?("No camera available on this device.")
            return
        }
        do {
            let input = try AVCaptureDeviceInput(device: device)
            if session.canAddInput(input) {
                session.addInput(input)
            } else {
                onError?("Could not add camera input.")
                return
            }
        } catch {
            onError?("Camera error: \(error.localizedDescription)")
            return
        }

        let metadataOutput = AVCaptureMetadataOutput()
        if session.canAddOutput(metadataOutput) {
            session.addOutput(metadataOutput)
            metadataOutput.setMetadataObjectsDelegate(self, queue: .main)
            metadataOutput.metadataObjectTypes = [.qr]
        } else {
            onError?("Could not add metadata output.")
            return
        }

        let preview = AVCaptureVideoPreviewLayer(session: session)
        preview.videoGravity = .resizeAspectFill
        preview.frame = view.bounds
        view.layer.addSublayer(preview)
        self.previewLayer = preview
    }

    // MARK: - AVCaptureMetadataOutputObjectsDelegate

    func metadataOutput(
        _ output: AVCaptureMetadataOutput,
        didOutput metadataObjects: [AVMetadataObject],
        from connection: AVCaptureConnection
    ) {
        guard !didReport else { return }
        for object in metadataObjects {
            if let machineReadable = object as? AVMetadataMachineReadableCodeObject,
               machineReadable.type == .qr,
               let value = machineReadable.stringValue {
                didReport = true
                onScanned?(value)
                return
            }
        }
    }
}

#endif
