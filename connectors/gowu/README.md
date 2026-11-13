# Gowu application information

`GetAccessClient` reads the current app's remark and cumulative API call count from `GET https://api.gowu.net/v1/access-clients/me`. The [official API reference](https://www.gowu.net/open/docs/api/access) defines the response and the [quick start](https://www.gowu.net/open/docs/rest) defines v2 HMAC-SHA256 signing: each request uses a fresh nonce and Unix timestamp, a canonical eight-part string, and the app's signing secret. The App Key and secret never enter results, errors, or receipts.

The output contains only `remark` and `apiCallCount`; the provider's App Key and creation timestamp are discarded. HTTP 429, 5xx, and transport failures retry. Definite rejections take the optional `providerRejected` branch, which fails the example Flow unless deliberately wired. Responses are bounded to 256 KiB.

The [runnable Dex Flow](go/examples/access-client/README.md) demonstrates a static connection, durable result, strict FDG, and a mock backed by a real Dex Worker and Client. Live provider authorization and production responses remain unverified. The logo is the [official Gowu site icon](https://www.gowu.net/icon.svg).
