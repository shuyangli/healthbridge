// Tiny hex codec used by both the app (to read HEALTHBRIDGE_KEY env var)
// and the kit's tests (to load fixed binary fixtures).

import Foundation

public extension Data {
    /// Decode a hex string into Data. Returns nil for any non-hex char or
    /// odd-length input. The decoder is intentionally case-insensitive.
    init?(hexString: String) {
        let chars = Array(hexString)
        guard chars.count % 2 == 0 else { return nil }
        var data = Data(capacity: chars.count / 2)
        var i = 0
        while i < chars.count {
            guard let high = chars[i].hexDigitValue, let low = chars[i + 1].hexDigitValue else {
                return nil
            }
            data.append(UInt8(high * 16 + low))
            i += 2
        }
        self = data
    }

    /// Encode the bytes as a lowercase hex string.
    var hexString: String {
        return map { String(format: "%02x", $0) }.joined()
    }
}
