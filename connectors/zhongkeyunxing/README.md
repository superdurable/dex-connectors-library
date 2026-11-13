# Zhongke Yunxing connector

`ListModels` reads at most 50 model IDs and the total count from [Zhongke Yunxing's documented GET /v1/models API](https://www.zhongkeyunx.com/brand/guide.html). The service is operated by 大连中科云星技术服务有限公司. A model list is scoped to the platform API Key and its model group; listing does not request inference.

Open [Zhongke Yunxing](https://www.zhongkeyunx.com), sign in, then create an API Key in the platform console. Copy the secret into Dex Web Connections. Blank prevents the request. Leave timeout and maxResponseBytes blank for their displayed defaults. The Query returns only model identifiers, never the Key or other provider metadata.

The checked-in [runnable model catalog Flow](go/examples/list-models/) uses `ConnectionName = "zhongkeyunxing"`. Run the worker with `go run .` from its directory, then start `ZhongkeYunxingModelCatalog` with `{}`. Strict FDG 2.0 instructions and a Dex mock integration test are alongside the Flow.

Provider contract: [official developer documentation](https://www.zhongkeyunx.com/brand/guide.html). Real provider authorization is unverified without a customer-controlled Key.
