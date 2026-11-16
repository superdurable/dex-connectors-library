# Alibaba Cloud Python connector

`dex-connector-alibaba` exposes the read-only ECS `describeRegions` operation as a Dex Query Step. It uses [Alibaba Cloud OpenAPI signature V3](https://help.aliyun.com/en/sdk/product-overview/v3-request-structure-and-signature) with a fresh UTC date and nonce for each request. Configure a RAM AccessKey pair in Dex Web Connections. The provider operation is [DescribeRegions](https://help.aliyun.com/en/ecs/developer-reference/api-ecs-2014-05-26-describeregions).

See [the runnable Flow](examples/regions/) for the Worker and connection setup. Real RAM authorization and ECS response behavior remain unverified locally.
