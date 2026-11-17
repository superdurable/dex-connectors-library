# Baidu Maps Python connector

`dex-connector-baidu-maps` exposes `geocodeAddress` as a Dex Query Step. The [official Geocoding API v3](https://lbsyun.baidu.com/docs/webapi?title=geocoding%2Fguide%2Fwebservice-geocoding-base) requires a server-side AK in the query string. Configure the secret AK in Dex Web Connections; it never enters Flow state or logs.

See the [runnable Flow](examples/address_lookup/) for a Worker and static connection name. Real provider authorization has not been verified locally.
