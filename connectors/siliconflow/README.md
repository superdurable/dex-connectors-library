# SiliconFlow connector

The Go connector lists model IDs through SiliconFlow China's read-only
`GET /v1/models` API. It exposes the `ListModels` Query Step and a runnable
[`model-catalog` Flow](go/examples/model-catalog/README.md).

Install `github.com/superdurable/dex-connectors-library/connectors/siliconflow/go@v0.1.0`
after the first release. Create a secret API Key on the
[SiliconFlow China API 密钥 page](https://cloud.siliconflow.cn/account/ak).
The example uses the static `siliconflow` connection. Its only required
user-supplied value is `api_key`, which stays in the connection credential and
is sent as a Bearer header. Provider error bodies and transport URLs are never
copied into Dex results or logs.

The `listed` branch returns the current provider model IDs, including an empty
list. Catalog membership alone does not guarantee inference access or pricing.
HTTP 408, 429, 5xx and transport failures retry under Dex's default one-hour
budget. HTTP 401, 403, and other non-success responses select
`providerRejected`; malformed or oversized replies select `invalidResponse`.
Missing credentials select `defect`. Mock model IDs are sample data and do not
claim live availability.

## Verification

```bash
cd connectors/siliconflow/go
GOWORK=off go test ./...
GOWORK=off go vet ./...
```

The provider tests use a local HTTP server. The example's integration test uses
a real Dex Worker and Client with a generated mock. Live SiliconFlow behavior
requires a user-configured key in Dex Web Connections and remains unverified
without it.

Provider references: [SiliconFlow quick start](https://docs.siliconflow.cn/docs/userguide/quickstart),
[API key setup](https://cloud.siliconflow.cn/account/ak), and
[model retrieval in the SiliconFlow guide](https://docs.siliconflow.cn/docs/usercases/use-siliconcloud-in-yan).
