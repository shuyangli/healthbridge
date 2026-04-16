// Settings tab — manage connection and unpair.

#if os(iOS)
import SwiftUI

struct SettingsView: View {
    let onUnpair: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Section header
            Text("Settings")
                .font(.title.bold())
                .padding(.horizontal, 24)
                .padding(.top, 8)

            // Manage Connection card
            VStack(spacing: 16) {
                Text("Manage Connection")
                    .font(.headline)

                Text("Disconnecting will permanently remove the link between your device and the AI agent.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)

                Button(action: onUnpair) {
                    HStack(spacing: 8) {
                        Image(systemName: "link.badge.plus")
                            .symbolRenderingMode(.monochrome)
                            .rotationEffect(.degrees(45))
                        Text("Unpair Device")
                            .fontWeight(.semibold)
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 14)
                }
                .buttonStyle(.borderedProminent)
                .tint(.red)

                Text("This will revoke all access for your AI agent.")
                    .font(.caption)
                    .italic()
                    .foregroundStyle(.red.opacity(0.8))
            }
            .padding(24)
            .frame(maxWidth: .infinity)
            .background(Color(.systemGray6))
            .clipShape(RoundedRectangle(cornerRadius: 16))
            .padding(.horizontal, 24)
            .padding(.top, 32)

            Spacer()
        }
    }
}

#endif
