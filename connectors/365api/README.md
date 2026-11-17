# 365 API connector

`ListModels` reads at most 50 model IDs and the total count from [365 API's documented GET /v1/models API](https://www.hby365.studio/developers). The service is operated by 深圳市赫比亚网络科技有限公司. A model list is scoped to the platform API Key; listing does not request inference.

Open [365 API](https://www.hby365.studio), sign in, then create an API Key in the platform console. Copy the secret into Dex Web Connections. Blank prevents the request. Leave timeout and maxResponseBytes blank for their displayed defaults. The Query returns only model identifiers, never the Key or other provider metadata.

The checked-in [runnable model catalog Flow](go/examples/list-models/) uses `ConnectionName = "hby365"`. Run the worker with `go run .` from its directory, then start `HBY365ModelCatalog` with `{}`. Strict FDG 2.0 instructions and a Dex mock integration test are alongside the Flow.

Provider contract: [official developer documentation](https://www.hby365.studio/developers). Real provider authorization is unverified without a customer-controlled Key.

The [Python package](python/README.md) implements `listModels` with a runnable Dex Flow and Go-recorded provider conformance scenarios. Import it as `dex_connectors._365api`; the leading underscore makes the numeric directory name a valid Python module.

The [TypeScript package and runnable Flow](typescript/README.md) replay the Go-recorded provider contract and exercise the Flow against a real local Dex Server.
