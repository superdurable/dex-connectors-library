# GSH API connector

`ListModels` reads at most 50 model IDs and the total count from [GSH API's documented GET /v1/models API](https://api.gshpay.com/docs.html). The service is operated by 深圳市湘港湾科技有限公司. A model list is scoped to the platform API Key ; listing does not request inference.

Open [GSH API subscription](https://api.gshpay.com/pricing.html) and submit a subscription request to receive an `sk-gsh-` API Key. Copy the secret into Dex Web Connections. Blank prevents the request. Leave timeout and maxResponseBytes blank for their displayed defaults. The Query returns only model identifiers, never the Key or other provider metadata.

The checked-in [runnable model catalog Flow](go/examples/list-models/) uses `ConnectionName = "gshapi"`. Run the worker with `go run .` from its directory, then start `GSHAPIModelCatalog` with `{}`. Strict FDG 2.0 instructions and a Dex mock integration test are alongside the Flow.

Provider contract: [official developer documentation](https://api.gshpay.com/docs.html). Real provider authorization is unverified without a customer-controlled Key.
