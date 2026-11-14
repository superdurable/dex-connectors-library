# Jiguang JPush connector

The Go `listApplicationTags` Query calls the [JPush Device API `GET /v3/tags/`](https://docs.jiguang.cn/jpush/server/push/rest_api_v3_device) to read the configured application's tag names. The provider returns at most 100 tags per platform. This operation does not read device identifiers or send pushes.

## Connect

Sign in at the [Jiguang console](https://www.jiguang.cn), open **Service Center → Developer Platform → Application Management**, and create or select a JPush application. On **App Settings**, copy its AppKey to `appKey` and Master Secret to `masterSecret` in Dex Web Connections. The AppKey is the Basic Authentication username; the Master Secret is the password and remains in a secret credential. Blank values prevent calls. Regenerate a compromised secret on the same page. Leave `baseUrl`, `timeout` and `maxResponseBytes` blank for their displayed defaults.

The [REST API overview](https://docs.jiguang.cn/jpush/server/push/server_overview) defines Basic Authentication and HTTP 429 rate limits. HTTP 429, HTTP 5xx and transport failures retry; definite 4xx responses select optional `providerRejected`; malformed or oversized JSON selects optional `invalidResponse`. The example leaves optional branches unwired, so these outcomes fail the Flow. A provider `tags: null` becomes an empty list.

The runnable example is in [`go/examples/tag-read`](go/examples/tag-read). Provider tests use a local HTTP server and the documentation's Basic auth example. Real JPush authorization and live tag discovery remain unverified without a user-controlled app.
