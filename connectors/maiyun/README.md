# Maiyun IP location connector

The [Maiyun IP 定位 API](https://lts.maiyun.net/service/ip) accepts an IPv4 address through `POST /api/service/ip` and returns country, province, city, and district. Maiyun is operated by [柏软科技（成都）有限公司](https://lts.maiyun.net/service/ip). The connector returns only these coarse location fields and excludes the provider response and credential.

Sign in at [Maiyun LTS](https://lts.maiyun.net/), enable IP 定位, then open **控制台 > Token** to create an authorized Bearer Token. Store it in Dex Web Connections. Queries may consume provider quota. The token is sent only in the `Authorization` header.

Run the [example Flow](go/examples/lookup-ip-location/) with its generated mock or a user-controlled connection. Pass only public IPv4 addresses you are authorized to query. Provider errors, malformed data, oversized bodies, and retryable transport failures have separate outcomes. Real provider authorization and billing have not been verified locally.
