import XCTest
@testable import HealthBridgeKit

/// Mock URLProtocol that lets us script HTTP responses for the RelayClient
/// without spinning up a real server. We register handlers per (method, path)
/// pair; the test asserts on what RelayClient sends and decodes what comes
/// back.
final class MockURLProtocol: URLProtocol {
    typealias Handler = (URLRequest) throws -> (HTTPURLResponse, Data)
    nonisolated(unsafe) static var handler: Handler?
    nonisolated(unsafe) static var lastRequest: URLRequest?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        guard let h = Self.handler else {
            client?.urlProtocol(self, didFailWithError: URLError(.unknown))
            return
        }
        Self.lastRequest = request
        do {
            let (response, data) = try h(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() {}
}

final class RelayClientTests: XCTestCase {

    private func newClient() -> RelayClient {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [MockURLProtocol.self]
        let session = URLSession(configuration: cfg)
        return RelayClient(
            baseURL: URL(string: "https://relay.example.com")!,
            pairID: "01J9ZX0PAIR000000000000001",
            session: session
        )
    }

    override func tearDown() {
        super.tearDown()
        MockURLProtocol.handler = nil
        MockURLProtocol.lastRequest = nil
    }

    func testEnqueueJobSendsExpectedBodyAndDecodesResponse() async throws {
        MockURLProtocol.handler = { req in
            XCTAssertEqual(req.httpMethod, "POST")
            XCTAssertTrue(req.url!.absoluteString.contains("/v1/jobs"))
            XCTAssertTrue(req.url!.absoluteString.contains("pair=01J9ZX0PAIR000000000000001"))

            let body = try JSONSerialization.jsonObject(with: req.bodyStreamData() ?? Data()) as! [String: Any]
            XCTAssertEqual(body["job_id"] as? String, "job-1")
            XCTAssertEqual(body["blob"] as? String, "ciphertext")

            let payload: [String: Any] = [
                "job_id": "job-1",
                "seq": 1,
                "enqueued_at": 1000,
                "expires_at": 9000,
            ]
            let data = try JSONSerialization.data(withJSONObject: payload)
            return (HTTPURLResponse(url: req.url!, statusCode: 201, httpVersion: nil, headerFields: nil)!, data)
        }

        let client = newClient()
        let ack = try await client.enqueueJob(jobID: "job-1", blob: "ciphertext")
        XCTAssertEqual(ack.jobID, "job-1")
        XCTAssertEqual(ack.seq, 1)
    }

    func testPollJobsDecodesPage() async throws {
        MockURLProtocol.handler = { req in
            let url = req.url!
            XCTAssertTrue(url.query!.contains("since=0"))
            XCTAssertTrue(url.query!.contains("wait_ms=0"))
            let payload: [String: Any] = [
                "jobs": [
                    [
                        "seq": 1,
                        "job_id": "j1",
                        "blob": "blob-1",
                        "enqueued_at": 1000,
                        "expires_at": 9000,
                    ]
                ],
                "next_cursor": 1,
            ]
            let data = try JSONSerialization.data(withJSONObject: payload)
            return (HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil, headerFields: nil)!, data)
        }

        let client = newClient()
        let page = try await client.pollJobs(since: 0, waitMs: 0)
        XCTAssertEqual(page.jobs.count, 1)
        XCTAssertEqual(page.jobs[0].jobID, "j1")
        XCTAssertEqual(page.nextCursor, 1)
    }

    func testAuthTokenIsAttachedAsBearerHeader() async throws {
        var capturedAuth: String?
        MockURLProtocol.handler = { req in
            capturedAuth = req.value(forHTTPHeaderField: "authorization")
            let payload: [String: Any] = ["job_id": "j", "seq": 1, "enqueued_at": 1, "expires_at": 2]
            let data = try JSONSerialization.data(withJSONObject: payload)
            return (HTTPURLResponse(url: req.url!, statusCode: 201, httpVersion: nil, headerFields: nil)!, data)
        }
        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [MockURLProtocol.self]
        let session = URLSession(configuration: cfg)
        let client = RelayClient(
            baseURL: URL(string: "https://relay.example.com")!,
            pairID: "01J9ZX0PAIR000000000000001",
            authToken: "deadbeef-token",
            session: session
        )
        _ = try await client.enqueueJob(jobID: "j", blob: "b")
        XCTAssertEqual(capturedAuth, "Bearer deadbeef-token")
    }

    func testNoAuthTokenOmitsHeader() async throws {
        var capturedAuth: String?
        MockURLProtocol.handler = { req in
            capturedAuth = req.value(forHTTPHeaderField: "authorization")
            let payload: [String: Any] = ["jobs": [], "next_cursor": 0]
            let data = try JSONSerialization.data(withJSONObject: payload)
            return (HTTPURLResponse(url: req.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!, data)
        }
        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [MockURLProtocol.self]
        let session = URLSession(configuration: cfg)
        // Pairing-time client has no token yet.
        let client = RelayClient(
            baseURL: URL(string: "https://relay.example.com")!,
            pairID: "01J9ZX0PAIR000000000000001",
            session: session
        )
        _ = try await client.pollJobs(since: 0, waitMs: 0)
        XCTAssertNil(capturedAuth)
    }

    func testRelayErrorIsThrown() async {
        MockURLProtocol.handler = { req in
            let payload: [String: Any] = ["code": "mailbox_full", "message": "x"]
            let data = try JSONSerialization.data(withJSONObject: payload)
            return (HTTPURLResponse(url: req.url!, statusCode: 429, httpVersion: nil, headerFields: nil)!, data)
        }

        let client = newClient()
        do {
            _ = try await client.enqueueJob(jobID: "j", blob: "b")
            XCTFail("expected throw")
        } catch let err as RelayClient.RelayError {
            XCTAssertEqual(err.code, "mailbox_full")
            XCTAssertEqual(err.httpStatus, 429)
        } catch {
            XCTFail("unexpected error: \(error)")
        }
    }
}

extension URLRequest {
    /// URLProtocol stubs receive request bodies via httpBodyStream rather
    /// than httpBody. This helper drains the stream into Data. Shared with
    /// PairingTests; keep it internal so other test files can use it too.
    func bodyStreamData() -> Data? {
        if let body = httpBody { return body }
        guard let stream = httpBodyStream else { return nil }
        stream.open()
        defer { stream.close() }
        var data = Data()
        let bufSize = 1024
        let buf = UnsafeMutablePointer<UInt8>.allocate(capacity: bufSize)
        defer { buf.deallocate() }
        while stream.hasBytesAvailable {
            let read = stream.read(buf, maxLength: bufSize)
            if read <= 0 { break }
            data.append(buf, count: read)
        }
        return data
    }
}
