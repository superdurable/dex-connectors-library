# Gitee Python connector

`dex-connector-gitee` provides the `getRepository` Query. It reads one repository by the owner and repository segments from its Gitee URL and returns public metadata. The personal access token remains in the connection credentials and never enters the Flow input or result. Gitee requires the token in the HTTPS query parameter, so the connector suppresses provider URLs and response bodies from failures and disables redirects.

## Connection

Sign in to [Gitee Personal Access Tokens](https://gitee.com/profile/personal_access_tokens), open **avatar → Settings → Personal Access Tokens**, and generate a token with repository read access. Enter the complete token in Dex Connections as the secret `access_token`. Blank prevents requests. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed production defaults; override only for an approved proxy or local test. The `owner` and `repo` inputs are single URL path segments, not complete URLs.

The optional `notFound`, `providerRejected`, `invalidResponse`, and `defect` branches fail the Flow unless the application wires a reaction. Temporary HTTP and transport failures retry under the Step policy. See `examples/repository_lookup/` for a runnable Flow, Worker, and real Dex integration test. Run `uv run pytest tests` to replay Go-recorded provider scenarios. Live Gitee authorization has not been verified.
