# Dimleap connector

`ListModels` reads at most 50 model IDs and the total count from [Dimleap's documented GET /v1/models API](https://www.dimleap.cn/docs). The service is operated by 深圳市跃迁维度科技有限公司. A model list is scoped to the platform API Key ; listing does not request inference.

Open [Dimleap](https://www.dimleap.cn), sign in, then create an API Key in the platform console. Copy the secret into Dex Web Connections. Blank prevents the request. Leave timeout and maxResponseBytes blank for their displayed defaults. The Query returns only model identifiers, never the Key or other provider metadata.

The checked-in [runnable model catalog Flow](go/examples/list-models/) uses `ConnectionName = "dimleap"`. Run the worker with `go run .` from its directory, then start `DimleapModelCatalog` with `{}`. Strict FDG 2.0 instructions and a Dex mock integration test are alongside the Flow.

Provider contract: [official developer documentation](https://www.dimleap.cn/docs). Real provider authorization is unverified without a customer-controlled Key.

## Python

The [Python package](python/README.md) implements `listModels` and includes a [runnable Dex Flow](python/examples/model_catalog/README.md). Its 17 provider scenarios replay Go-recorded requests, outputs, and retry decisions. No live Dimleap Key has been authorized for this port; the catalog is dynamic.
