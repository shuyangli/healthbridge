// Activity log view — shows the append-only audit trail of every job the
// drain loop has executed. Each row shows the job kind, sample type, outcome,
// and a relative timestamp. Entries appear newest-first.

#if os(iOS)
import SwiftUI
import HealthBridgeKit

struct ActivityLogView: View {
    let entries: [AuditEntry]

    var body: some View {
        if entries.isEmpty {
            ContentUnavailableView(
                "No Activity Yet",
                systemImage: "list.bullet",
                description: Text("Jobs will appear here as they are processed.")
            )
        } else {
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(entries.reversed(), id: \.id) { entry in
                        ActivityLogRow(entry: entry)
                        Divider()
                            .padding(.leading, 52)
                    }
                }
            }
        }
    }
}

private struct ActivityLogRow: View {
    let entry: AuditEntry

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: iconName)
                .foregroundStyle(iconColor)
                .font(.title3)
                .frame(width: 28, alignment: .center)

            VStack(alignment: .leading, spacing: 2) {
                if let type = entry.sampleType {
                    Text(Self.displayName(for: type))
                        .font(.subheadline)
                        .lineLimit(1)
                }
                HStack(spacing: 4) {
                    Text(entry.kind.rawValue.capitalized)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                    if let code = entry.errorCode {
                        Text("· \(code)")
                            .font(.caption)
                            .foregroundStyle(.red)
                    }
                }
            }

            Spacer()

            VStack(alignment: .trailing, spacing: 4) {
                Image(systemName: outcomeIcon)
                    .foregroundStyle(outcomeColor)
                    .font(.subheadline)
                Text(Self.formatter.localizedString(for: entry.timestamp, relativeTo: .now))
                    .font(.caption2)
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(.vertical, 10)
        .padding(.horizontal, 16)
    }

    // MARK: - Appearance helpers

    private var iconName: String {
        switch entry.kind {
        case .read:  "arrow.down.circle.fill"
        case .write: "arrow.up.circle.fill"
        }
    }

    private var iconColor: Color {
        switch entry.kind {
        case .read:  .blue
        case .write: .orange
        }
    }

    private var outcomeIcon: String {
        entry.outcome == .done ? "checkmark.circle.fill" : "xmark.circle.fill"
    }

    private var outcomeColor: Color {
        entry.outcome == .done ? .green : .red
    }

    private static let formatter: RelativeDateTimeFormatter = {
        let f = RelativeDateTimeFormatter()
        f.unitsStyle = .abbreviated
        return f
    }()

    /// Turn a wire-format sample type ("dietary_energy_consumed") into a
    /// human-friendly label ("Dietary Energy Consumed").
    static func displayName(for wireType: String) -> String {
        wireType
            .split(separator: "_")
            .map { $0.prefix(1).uppercased() + $0.dropFirst() }
            .joined(separator: " ")
    }
}
#endif
