# LianLian Pay payment status

`QueryPaymentStatus` reads a merchant payment order's processing status. It sends a signed JSON request to `POST https://openapi.lianlianpay.com/query/mch/v1/accp/txn/order-query` and returns only the merchant order ID and status. Payer, bank account, and other financial details are not exposed in the typed output or receipt.

The [official payment result query](https://api-doc.lianlianpay.com/openplatform/api-3477863) defines the HTTPS endpoint, request and response fields, and status values. The [official authentication guide](https://api-doc.lianlianpay.com/openplatform/doc-338855) defines the RSA signature: lowercase MD5 hex of the exact compact JSON body is itself signed with MD5withRSA, then Base64 encoded in `Signature-Data`. The provider's published sample signature matches this double-digest recipe. The same guide explains merchant public-key registration. Use a private key specific to your merchant and keep it in Dex's protected connection field.

HTTP 429, 408 and 5xx plus transport failures retry. Definite provider rejections take the optional `providerRejected` branch; malformed or oversized responses take `invalidResponse`. The example leaves these branches unwired so the Flow fails when selected. The response is bounded to 256 KiB.

The [runnable Dex Flow](go/examples/payment-status/README.md) demonstrates a static connection, typed input, durable result, strict FDG, and mock-backed integration through a real Dex Worker and Client. Live merchant authorization and production responses remain unverified. The logo is rendered from [LianLian Pay's official brand resources](https://pdcenter.lianlianpay.com/tool.html).
