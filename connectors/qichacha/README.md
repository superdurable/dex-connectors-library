# Qichacha enterprise basic details

The Go connector reads [API 410 企业工商信息](https://openapi.qcc.com/dataApi/410) with an exact mainland enterprise name or unified social credit code. It returns only `Name`, `CreditCode`, and registration `Status`; it discards the provider's personnel, address, and other returned fields. Qichacha documents `CreditCode` as a different identifier for Hong Kong entities, so this connector rejects non-mainland entity types.

The provider requires enterprise real-name verification, an approved application scenario, and API 410 entitlement. Qichacha lists ¥0.20 per lookup and a 20-call trial. Each retry may incur another charge, so the connector makes at most two attempts for transport failures, HTTP 429, and HTTP 5xx. The AppKey and SecretKey are held as Dex credentials. The connector sends the AppKey in the required HTTPS query parameter and an uppercase MD5 signature in the `Token` header; neither appears in a result or error.

See the [runnable Dex Flow](go/examples/enterprise-basic-details/README.md). Mock tests do not verify live authorization, billing, provider status codes, or response behavior.
