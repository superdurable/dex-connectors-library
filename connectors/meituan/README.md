# Meituan Delivery order status

`QueryOrderStatus` reads the current status of one delivery order created by the authorized application within the past 90 days. It calls `POST https://peisongopen.meituan.com/api/order/status/query` with a URL-encoded form. The signature is lowercase SHA-1 of the secret followed by the sorted, non-empty parameter names and values. The secret stays in the protected connection and never enters a URL, result, or receipt.

The [current official API document](https://peisong.meituan.com/tscc/doc) specifies the HTTPS endpoint, system parameters, signature vector, order-status request and response, and 10 QPS limit. The [onboarding guide](https://peisong.meituan.com/tscc/guide) describes separate production and test AppKeys and secrets. The output includes only IDs, status code, and status time. Rider names, phone numbers, location, and health data are omitted.

HTTP 429 and 5xx, transport failures, and provider code 11 retry. Definite rejections take the optional `providerRejected` branch, which fails the example Flow unless deliberately wired. The response is bounded to 256 KiB.

The [runnable Dex Flow](go/examples/order-status/README.md) demonstrates a static connection, typed input, durable result, strict FDG, and a mock backed by a real Dex Worker and Client. Live application authorization, production responses, and the provider's 90-day window remain unverified. The logo uses the [official Meituan Delivery page asset](https://p0.meituan.net/paotui/knzsblrx6ig.png).
