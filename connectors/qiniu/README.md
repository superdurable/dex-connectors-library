# Qiniu Kodo connector

The Go connector lists storage bucket names through Qiniu's [Bucket list API](https://developer.qiniu.com/kodo/api/get-service). It exposes the `ListBuckets` Query Step and a runnable [`bucket-list` Flow](go/examples/bucket-list/README.md). The result includes only buckets owned by the signing AccessKey account; Qiniu does not include buckets delegated by another owner.

The connection name in the example is `qiniu`. Create or select an AccessKey/SecretKey pair in [Qiniu Key Management](https://portal.qiniu.com/user/key), then enter both values as secret credentials in Dex Web Connections. Keep the pair matched. The SecretKey is used only to sign the request, including its `X-Qiniu-Date` timestamp header, and is never sent to Qiniu. Keep the Worker clock within 15 minutes of real time; Qiniu rejects older or newer signatures with HTTP 403. The connector does not copy credentials, provider error bodies, or transport URLs into results or logs.

The Query retries HTTP 408, 429, and 5xx responses and transport failures under Dex's default retry budget. Other non-200 statuses select `providerRejected`; malformed or oversized responses select `invalidResponse`; missing or malformed credentials select `defect`. A successful empty array is a valid result.

Install `github.com/superdurable/dex-connectors-library/connectors/qiniu/go@v0.1.0` after the first release. Provider tests run against an HTTP test server. The mock-backed example integration test requires a real `dexcli dev` stack. Live Qiniu authentication and bucket listing require a user-configured key pair and are not exercised by local provider tests.
