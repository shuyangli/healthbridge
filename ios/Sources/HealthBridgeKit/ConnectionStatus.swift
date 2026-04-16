import Foundation

/// The app's connection status, derived from pairing state, drain-loop
/// activity, and network reachability. Defined in the kit so it can be
/// unit-tested without HealthKit or UIKit dependencies.
public enum ConnectionStatus: Equatable, Sendable {
    case notPaired
    case connecting
    case connected
    case backgrounded
    case networkUnavailable
    case relayError(String)
}

/// Maps a drain-loop error to a ``ConnectionStatus``.
///
/// Returns `nil` for cancellation errors (structured-concurrency or
/// URLSession-level) that should be silently ignored.
public func classifyDrainError(_ error: Error) -> ConnectionStatus? {
    if error is CancellationError {
        return nil
    }
    if let urlError = error as? URLError {
        if urlError.code == .cancelled {
            return nil
        }
        if [.notConnectedToInternet, .networkConnectionLost, .dataNotAllowed]
            .contains(urlError.code) {
            return .networkUnavailable
        }
    }
    return .relayError(error.localizedDescription)
}

/// Overlays NWPathMonitor reachability on top of the base connection status.
/// When the network is down and the base status is `.connected` or
/// `.connecting`, returns `.networkUnavailable` for instant UI feedback
/// before the HTTP request times out.
public func effectiveConnectionStatus(
    base: ConnectionStatus,
    networkAvailable: Bool
) -> ConnectionStatus {
    guard !networkAvailable else { return base }
    switch base {
    case .connected, .connecting:
        return .networkUnavailable
    default:
        return base
    }
}
