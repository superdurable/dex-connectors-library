# Volcengine ECS regions

This connector reads one page from the official [ECS DescribeRegions](https://docs.volcengine.com/docs/ecs/describeregions) API. `MaxResults` accepts 1–20; zero uses the provider default of 10. `NextToken` continues from a previous page. The output contains only region IDs and the next token, with no instance or account data.

Requests use [Volcengine's HMAC-SHA256 signature](https://docs.volcengine.com/docs/api-signature-call-guide/signature-method) in the Authorization header. The Access Key ID and Secret Access Key stay in Dex credentials; the UTC X-Date and signature are generated per request. The default endpoint and signing region are for ECS Beijing. An override must match a [documented ECS regional endpoint](https://docs.volcengine.com/docs/ecs/service-address). The [runnable Dex Flow](go/examples/region-list/README.md) demonstrates connection setup and local verification.

No live Volcengine authorization or ECS response was exercised by the mock tests.
