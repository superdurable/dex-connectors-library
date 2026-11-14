# PingCAP TiDB Cloud connector

The Go connector lists one page of organization projects through the [TiDB Cloud v1-beta API](https://docs.pingcap.com/tidbcloud/api/v1beta/). It exposes the `ListProjects` Query Step and a runnable [`project-list` Flow](go/examples/project-list/README.md). The Flow input accepts one-based `page` and `pageSize`; omitted values default to the API's first page and ten items. The output includes `items`, `total`, `page`, and `pageSize`.

Create a scoped public/private API key pair under **Organization Settings > API Keys** in the [TiDB Cloud console](https://tidbcloud.com/). Dex stores both as secret credentials. The connector uses HTTP Digest Authentication, with a single 401 challenge and signed retry to the same URL. It supports MD5 and SHA-256 with `qop=auth`, including their `-sess` variants. It rejects malformed, stale, or unsupported challenges and disables redirects. The private key is never sent; provider bodies, URL-bearing transport errors, and credentials are never copied into results or failures.

The Query retries HTTP 408, 429, 5xx, and transport failures under Dex's default retry budget. Terminal authentication, permission, and request errors select `providerRejected`; malformed or oversized project responses select `invalidResponse`; invalid input or credentials select `defect`.

Install `github.com/superdurable/dex-connectors-library/connectors/pingcap/go@v0.1.0` after release. Provider tests use an HTTP test server. The mock-backed example integration test uses a real Dex Worker and Client. Live API-key authorization and actual organization project visibility remain unverified without a user-configured account.
