// Onboarding screen shown when no device is paired.

#if os(iOS)
import SwiftUI

struct OnboardingView: View {
    let onPairDevice: () -> Void

    var body: some View {
        VStack(spacing: 0) {
            Spacer()

            // Title
            Text("Securely Share\nYour Health Data")
                .font(.system(size: 34, weight: .bold))
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 24)

            // Subtitle
            Text("Give your AI agent permission to securely query and log your Apple Health data.")
                .font(.body)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.top, 12)
                .padding(.horizontal, 24)

            Spacer()

            // Primary CTA
            Button(action: onPairDevice) {
                HStack(spacing: 8) {
                    Text("Pair Device")
                        .fontWeight(.semibold)
                    Image(systemName: "arrow.right")
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 16)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .tint(Color(red: 0.22, green: 0.38, blue: 0.85))
            .padding(.horizontal, 24)

            // Footer
            HStack(spacing: 6) {
                Text("HealthBridge queries from your agents are end-to-end encrypted.")
                    .font(.footnote)
            }
            .foregroundStyle(.tertiary)
            .multilineTextAlignment(.center)
            .padding(.horizontal, 32)
            .padding(.top, 24)
            .padding(.bottom, 16)
        }
    }
}

#endif
