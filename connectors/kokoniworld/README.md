# Kokoni World connector

`ListModels` reads at most 50 model IDs and the total count from [Kokoni World's documented GET /v1/models API](https://api.kokoniworld.com/docs-local.html). The service is operated by 魔芯（湖州）科技有限公司. A model list is scoped to the platform API Key and its model group; listing does not request inference.

Open [Kokoni World](https://api.kokoniworld.com), sign in, then create an API Key in the platform console. Copy the secret into Dex Web Connections. Blank prevents the request. Leave timeout and maxResponseBytes blank for their displayed defaults. The Query returns only model identifiers, never the Key or other provider metadata.

The checked-in [runnable model catalog Flow](go/examples/list-models/) uses `ConnectionName = "kokoniworld"`. Run the worker with `go run .` from its directory, then start `KokoniWorldModelCatalog` with `{}`. Strict FDG 2.0 instructions and a Dex mock integration test are alongside the Flow.

Provider contract: [official developer documentation](https://api.kokoniworld.com/docs-local.html). Real provider authorization is unverified without a customer-controlled Key.
# TypeScript

The TypeScript package `@superdurable/connector-kokoniworld` implements `listModels`. Its runnable Flow is in [`typescript/examples/basic`](typescript/examples/basic); conformance tests replay Go-recorded provider responses. Live Kokoni World authorization remains unverified.

The [Python package](python/) mirrors the Go Query and includes a [runnable Dex Worker/Client Flow](python/examples/list_models/) and Go scenario replay. Real provider credentials and live API behavior remain unverified.
