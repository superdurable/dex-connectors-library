# Feihan recharge order status

`QueryRechargeOrder` reads the current recharge status of one known merchant order. It calls `POST https://api2.ofpay.com/queryOrderInfo.do` with a URL-encoded form. Feihan requires a project code, the lowercase MD5 of the project password, and an uppercase MD5 signature of the project code, password hash, merchant order ID, and KeyStr. Credentials and signatures stay out of URLs, outputs, errors, and receipts.

The [official API document](https://feihan.cn/apiDocs.html) specifies the production HTTPS endpoint, order query form, status values, and response. The output includes only the merchant order ID and recharge status: 0 processing, 1 successful, or 9 failed. A provider -1 means the order could not be found and requires manual verification. Recharge account and card fields are discarded. The account requires business approval and an IP whitelist; the provider documents a test endpoint over HTTP, which this connector does not use.

HTTP 429 and 5xx and transport failures retry. Definite rejections take the optional `providerRejected` branch, which fails the example Flow unless deliberately wired. Responses are bounded to 256 KiB.

The [runnable Dex Flow](go/examples/recharge-order/README.md) demonstrates a static connection, typed input, durable result, strict FDG, and a mock backed by a real Dex Worker and Client. Live provider authorization and production responses remain unverified. The logo embeds the [official Feihan site asset](https://www.feihan.cn/assets/image/favicon.ico).
