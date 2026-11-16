# ALAPI Python connector

`dex-connector-alapi` exposes `lookupIPLocation` as a Dex Query Step. The API is documented by [ALAPI](https://www.alapi.cn/api/23/api_document). Use a public IPv4 address and configure the secret token in Dex Web Connections. The connector returns only coarse location fields.

See [the runnable Flow](examples/ip_location/) for Worker and connection setup. Real ALAPI authorization and quota behavior have not been verified locally.
