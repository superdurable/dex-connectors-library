# Alibaba Cloud ECS regions

The Go connector calls [ECS DescribeRegions](https://help.aliyun.com/en/ecs/developer-reference/api-ecs-2014-05-26-describeregions), a single-page, read-only region catalog. It returns region IDs, local names, public ECS endpoints, and availability status. The default host is the official China Hangzhou public [ECS endpoint](https://help.aliyun.com/en/ecs/developer-reference/api-ecs-2014-05-26-endpoint).

Each call uses the documented [OpenAPI signature V3](https://help.aliyun.com/en/sdk/product-overview/v3-request-structure-and-signature), including fresh UTC date and nonce, canonical headers, SHA-256 request digest, and HMAC-SHA256 authorization. The provider test verifies Alibaba Cloud's published fixed signing vector.

`describeRegions` selects `fetched`, `providerRejected`, `invalidResponse`, or `defect`. Rate limits, transport failures, and temporary provider failures retry. Optional branches fail the example Flow unless wired by an application.

[Run the regions Flow](go/examples/regions/README.md). The checked-in mock is synthetic; real provider authorization and response behavior require a user-controlled RAM AccessKey pair in Dex Web Connections.
