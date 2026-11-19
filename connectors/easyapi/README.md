# EasyAPI E-Invoice company service configuration

`QueryShop` reads the invoicing service configuration of one company already associated with the key pair. It calls `GET https://fapiao-api.easyapi.com/shop/query` with `appKey`, `appSecret`, and `taxNumber` as required by the [official company API](https://fapiao.easyapi.com/docs/shop/) and [authentication contract](https://fapiao.easyapi.com/docs/auth/). Because the provider puts credentials in the HTTPS query string, this connector disables redirects, never logs the URL, and returns generic errors without provider body or URL text. Results and receipts exclude the credentials and tax number.

The output contains only `shopId`, company `name`, `apiState` (1 normal, 0 expired, -1 not activated), and `endTime` (blank when absent). The provider's address, phone, bank, tax number, staff, certificate, and invoice settings are discarded. HTTP 429, 5xx, and transport failures retry. Definite rejections take the optional `providerRejected` branch, which fails the example Flow unless deliberately wired. Responses are bounded to 256 KiB.

The [runnable Dex Flow](go/examples/shop/README.md) demonstrates a static connection, typed tax number input, durable result, strict FDG, and a mock backed by a real Dex Worker and Client. Live provider authorization and production responses remain unverified. The logo is the [official EasyAPI E-Invoice asset](https://fapiao.easyapi.com/images/logo.svg).

## Python

The [Python package](python/README.md) implements `queryShop` and includes a [runnable Dex Flow](python/examples/shop/README.md). Its 15 Go-recorded scenarios cover private-safe outputs, Retry, provider branches, redirect refusal, and local validation. No live EasyAPI account has been authorized for this port.
