# GitCode Python connector

`dex-connector-gitcode` provides the `getRepository` Query. It reads one repository by the owner and repository segments from its GitCode URL and returns its ID, full name, visibility, and default branch. The personal access token remains in connection credentials, is sent in an HTTPS Bearer header, and never enters the Flow input or result.

## Connection

Sign in at [GitCode](https://gitcode.com), open **avatar → 个人设置 → 访问令牌 → 新建访问令牌**, and generate a token with project read access and an expiry. Enter the complete one-time token in Dex Connections as secret `access_token`; blank prevents requests. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed production defaults; override only for an approved proxy or local test. The `owner` and `repo` inputs are single URL path segments, not complete URLs.

The optional `notFound`, `providerRejected`, `invalidResponse`, and `defect` branches fail the Flow unless the application wires a reaction. Temporary HTTP and transport failures retry under the Step policy. See `examples/repository_lookup/` for a runnable Flow, Worker, and real Dex integration test. Run `uv run pytest tests` to replay Go-recorded provider scenarios. Live GitCode authorization has not been verified.
