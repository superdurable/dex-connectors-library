# Bilibili connector

The Go `getAuthorizedUser` Query reads the authorized Bilibili account's nickname and application-scoped `open_id` from the [official Open Platform endpoint](https://open.bilibili.com/doc/4/feb66f99-7d87-c206-00e7-d84164cd701c). It does not request arbitrary user IDs or video data. The API response includes an avatar URL, which Bilibili says must be cached before use; this connector omits it.

## Connect

At the [Bilibili Open Platform](https://open.bilibili.com), complete developer registration and open **管理中心 → 应用管理**. Create or select an application, copy `client_id` and secret `app_secret`, and request the `USER_INFO` capability. The account owner must authorize that capability. Enter the resulting OAuth2 access token in Dex Web Connections as a secret; refresh it through the application's authorization flow before expiry. Bilibili's [account authorization docs](https://open.bilibili.com/doc/4) explain the flow and permissions. Leave transport settings blank for their displayed defaults.

The [signature contract](https://open.bilibili.com/doc/4/8673959e-f7bb-56e6-6e68-d225f971b81b) requires HMAC-SHA256 over six ordered `x-bili-*` headers, a per-request nonce, and an MD5 of the empty GET body. The connector signs each request and sends the token only over HTTPS. HTTP 429, HTTP 5xx, and documented busy/rate-limit codes retry. Definite authorization or provider errors select optional `providerRejected`; malformed or oversized responses select optional `invalidResponse`. The example leaves optional branches unwired, so those outcomes fail the Flow. The official docs list rate-limit codes but no universal per-app numeric quota; check the application's approved limits in the portal.

The runnable Flow is in [`go/examples/authorized-user`](go/examples/authorized-user). Provider tests cover signing and response classifications. Live USER_INFO approval, OAuth2 authorization, and production account data remain unverified without user-controlled credentials.

The [Python package](python/README.md) implements the same Query with Go-recorded conformance and a [runnable Flow](python/examples/authorized_user/README.md).
