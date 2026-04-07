// Swift mirror of cli/internal/relay/client.go. URLSession-backed HTTP
// client targeting one (baseURL, pairID) at a time. The iOS app uses one
// of these per paired CLI; the kit is dependency-free so it works in unit
// tests as well as inside the app.

import Foundation

public actor RelayClient {
    public let baseURL: URL
    public let pairID: String
    private let session: URLSession

    public init(baseURL: URL, pairID: String, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.pairID = pairID
        self.session = session
    }

    public struct EnqueuedJob: Codable, Sendable {
        public let jobID: String
        public let seq: Int64
        public let enqueuedAt: Int64
        public let expiresAt: Int64
        enum CodingKeys: String, CodingKey {
            case jobID = "job_id"
            case seq
            case enqueuedAt = "enqueued_at"
            case expiresAt = "expires_at"
        }
    }

    public struct JobBlob: Codable, Sendable {
        public let seq: Int64
        public let jobID: String
        public let blob: String
        public let enqueuedAt: Int64
        public let expiresAt: Int64
        enum CodingKeys: String, CodingKey {
            case seq, blob
            case jobID = "job_id"
            case enqueuedAt = "enqueued_at"
            case expiresAt = "expires_at"
        }
    }

    public struct JobsPage: Codable, Sendable {
        public let jobs: [JobBlob]
        public let nextCursor: Int64
        enum CodingKeys: String, CodingKey {
            case jobs
            case nextCursor = "next_cursor"
        }
    }

    public struct ResultBlob: Codable, Sendable {
        public let jobID: String
        public let pageIndex: Int
        public let blob: String
        public let postedAt: Int64
        public let expiresAt: Int64
        enum CodingKeys: String, CodingKey {
            case blob
            case jobID = "job_id"
            case pageIndex = "page_index"
            case postedAt = "posted_at"
            case expiresAt = "expires_at"
        }
    }

    public struct ResultsResponse: Codable, Sendable {
        public let results: [ResultBlob]
    }

    public struct PostedResult: Codable, Sendable {
        public let jobID: String
        public let pageIndex: Int
        public let postedAt: Int64
        public let expiresAt: Int64
        enum CodingKeys: String, CodingKey {
            case jobID = "job_id"
            case pageIndex = "page_index"
            case postedAt = "posted_at"
            case expiresAt = "expires_at"
        }
    }

    public struct RelayError: Error, Codable, Equatable {
        public let httpStatus: Int
        public let code: String
        public let message: String

        enum CodingKeys: String, CodingKey {
            case code, message
        }

        public init(httpStatus: Int, code: String, message: String) {
            self.httpStatus = httpStatus
            self.code = code
            self.message = message
        }

        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            self.code = try c.decode(String.self, forKey: .code)
            self.message = try c.decode(String.self, forKey: .message)
            self.httpStatus = 0
        }

        public func encode(to encoder: Encoder) throws {
            var c = encoder.container(keyedBy: CodingKeys.self)
            try c.encode(code, forKey: .code)
            try c.encode(message, forKey: .message)
        }
    }

    // MARK: - Endpoints

    public func enqueueJob(jobID: String, blob: String) async throws -> EnqueuedJob {
        let body = EnqueueRequest(jobID: jobID, blob: blob)
        return try await request(method: "POST", path: "/v1/jobs", query: [:], body: body)
    }

    public func pollJobs(since: Int64, waitMs: Int) async throws -> JobsPage {
        return try await request(
            method: "GET",
            path: "/v1/jobs",
            query: ["since": String(since), "wait_ms": String(waitMs)],
            body: Optional<Empty>.none
        )
    }

    public func postResult(jobID: String, pageIndex: Int, blob: String) async throws -> PostedResult {
        let body = PostResultRequest(jobID: jobID, pageIndex: pageIndex, blob: blob)
        return try await request(method: "POST", path: "/v1/results", query: [:], body: body)
    }

    public func pollResults(jobID: String, waitMs: Int) async throws -> ResultsResponse {
        return try await request(
            method: "GET",
            path: "/v1/results",
            query: ["job_id": jobID, "wait_ms": String(waitMs)],
            body: Optional<Empty>.none
        )
    }

    public func revokePair() async throws {
        let _: Empty = try await request(method: "DELETE", path: "/v1/pair", query: [:], body: Optional<Empty>.none)
    }

    // MARK: - Request plumbing

    private struct EnqueueRequest: Encodable {
        let jobID: String
        let blob: String
        enum CodingKeys: String, CodingKey { case jobID = "job_id", blob }
    }

    private struct PostResultRequest: Encodable {
        let jobID: String
        let pageIndex: Int
        let blob: String
        enum CodingKeys: String, CodingKey {
            case jobID = "job_id"
            case pageIndex = "page_index"
            case blob
        }
    }

    private struct Empty: Codable {}

    private func request<Out: Decodable, Body: Encodable>(
        method: String,
        path: String,
        query: [String: String],
        body: Body?
    ) async throws -> Out {
        var components = URLComponents(url: baseURL.appendingPathComponent(path), resolvingAgainstBaseURL: false)!
        var items = [URLQueryItem(name: "pair", value: pairID)]
        for (k, v) in query {
            items.append(URLQueryItem(name: k, value: v))
        }
        components.queryItems = items

        var req = URLRequest(url: components.url!)
        req.httpMethod = method
        if let body = body {
            req.httpBody = try JSONEncoder().encode(body)
            req.setValue("application/json", forHTTPHeaderField: "content-type")
        }
        req.setValue("application/json", forHTTPHeaderField: "accept")

        let (data, response) = try await session.data(for: req)
        guard let http = response as? HTTPURLResponse else {
            throw RelayError(httpStatus: 0, code: "no_http_response", message: "non-HTTP response")
        }
        if http.statusCode >= 400 {
            if let parsed = try? JSONDecoder().decode(RelayError.self, from: data) {
                throw RelayError(httpStatus: http.statusCode, code: parsed.code, message: parsed.message)
            }
            throw RelayError(
                httpStatus: http.statusCode,
                code: "http_error",
                message: String(data: data, encoding: .utf8) ?? "unknown"
            )
        }
        if Out.self == Empty.self {
            return Empty() as! Out
        }
        return try JSONDecoder().decode(Out.self, from: data)
    }
}
