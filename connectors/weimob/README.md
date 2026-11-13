# Weimob connector

The Go `getStoreBasic` Query reads the WOS store bound to a Weimob Cloud self-use application through the [official `bos/info/get` API](https://doc.weimobcloud.com/detail?childMenuId=1&id=2350&isold=2&menuId=19&tag=2253). It returns the BOS store ID, name, logo, and description. The query has no input.

## Connect

At the [Weimob Cloud console](https://console.weimobcloud.com), create or select a self-use product application and authorize the WOS store. Under **Open Capabilities → Capability List**, select WOS Product, search for **查询bos基本信息**, and apply for access. The [official API debugging guide](https://doc.weimobcloud.com/word?childMenuId=54&isold=2&menuId=53&tag=3481) shows this process.

Under **Overview → Development Configuration**, copy `client_id` into `clientID` and secret `client_secret` into `clientSecret` in Dex Web Connections. For a multi-store application, copy the WOS store ID from **Authorization Management → Authorized Stores** into `shopID`; blank lets Weimob select the single bound store. The connector obtains and caches a client-credentials token, renewing it shortly before expiry. The provider's [self-use authorization guide](https://doc.weimobcloud.com/word?childMenuId=54&isold=1&menuId=53&tag=3693) documents this grant and the store selection parameters.

HTTP 429, HTTP 5xx, and transport failures retry. Definite token or API rejections select optional `providerRejected`; malformed or oversized responses select optional `invalidResponse`. The example leaves optional branches unwired, so those outcomes fail the Flow. Credentials and tokens never enter Flow data, receipts, or failure messages. Weimob's public page does not state a fee for this API; review pricing in the console.

The runnable example is in [`go/examples/store-read`](go/examples/store-read). Provider tests use a local HTTP server. Live Weimob authorization and the production store response remain unverified without a user-controlled application.
