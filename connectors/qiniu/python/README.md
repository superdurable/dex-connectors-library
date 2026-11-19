# Qiniu Kodo Python connector

`dex-connector-qiniu` provides the `listBuckets` Query. It signs one read-only Kodo UC request and returns bucket names. The AccessKey and SecretKey remain in connection credentials; only the signed `Authorization` header is sent to Qiniu, and no key enters Flow input or results.

## Connection

Sign in to [Qiniu Key Management](https://portal.qiniu.com/user/key), open **个人中心 → 密钥管理**, and create or select an active AccessKey/SecretKey pair. Enter both complete values in Dex Connections as secret `access_key` and `secret_key`; they must be paired. Blank prevents requests. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed production defaults; override only for an approved proxy or local test. No operation-specific input fields are required.

The optional `providerRejected`, `invalidResponse`, and `defect` branches fail the Flow unless the application wires a reaction. Temporary HTTP and transport failures retry under the Step policy. See `examples/bucket_list/` for a runnable Flow, Worker, and real Dex integration test. Run `uv run pytest tests` to replay Go-recorded provider scenarios. Live Qiniu authorization has not been verified.
