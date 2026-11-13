# ShinLinkAI connector

`ListModels` reads the first 50 model IDs and total count from [ShinLinkAI's GET /v1/models API](https://api.shinloo.com/docs/). The [official site](https://api.shinloo.com/) identifies 上海玺芮实业有限公司 as the operator. The documented production base URL is `https://api.shinloo.com/v1`; Keys use the `sk-sl-` prefix.

Register at [the console](https://console.shinloo.com), create an API Key and copy the secret once into Dex Web Connections. A blank Key blocks the request. Leave timeout and maxResponseBytes blank for their displayed defaults. The Query does not invoke inference or return billing data.

The checked-in [runnable model catalog Flow](go/examples/list-models/) uses `ConnectionName = "shinloo"`. Run the worker with `go run .` from its directory, then start `ShinLinkAIModelCatalog` with `{}`. Strict FDG 2.0 instructions and a Dex mock integration test are alongside the Flow.

Provider contract: [API documentation](https://api.shinloo.com/docs/). Real provider authorization is not verified without a customer-controlled Key.
