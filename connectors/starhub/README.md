# StarHub Ai connector

`ListModels` reads the first 50 model IDs and total count from [StarHub Ai's documented GET /v1/models API](https://www.starhubc.com/docs). The [service terms](https://www.starhubc.com/terms) identify 青硕秦皇岛文化传播有限公司 as the operator and document `https://api.starhubc.com/v1` with Bearer `sk-` authentication.

Register at [StarHub Ai](https://www.starhubc.com), open the console's **API Keys** section, create a Key, and copy the secret once into Dex Web Connections. A blank Key blocks the request. Leave timeout and maxResponseBytes blank for their displayed defaults. The Query does not invoke inference or return billing data.

The checked-in [runnable model catalog Flow](go/examples/list-models/) uses `ConnectionName = "starhub"`. Run the worker with `go run .` from its directory, then start `StarHubModelCatalog` with `{}`. Strict FDG 2.0 instructions and a Dex mock integration test are alongside the Flow.

Provider contract: [developer docs](https://www.starhubc.com/docs), [service terms](https://www.starhubc.com/terms). Real provider authorization is not verified without a customer-controlled Key.
