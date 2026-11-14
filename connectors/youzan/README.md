# Youzan connector

The Go `getShopBasic` Query reads the shop bound to a Youzan Cloud access token through the [official `youzan.shop.basic.get.3.0.0` API](https://doc.youzanyun.com/v2/doc/cloud/token/DIQ0wlF7picLWQkP1ATci1PUn4b.md). It returns the shop ID (`kdt_id`), name, logo, homepage, organization role, and business line. The query has no input. Youzan marks this API as metered and requires a shop information sync capability package.

## Connect

At the [Youzan Cloud console](https://diy.youzanyun.com/application/list), create or select a self-use application, authorize the intended shop, and apply under **Application Center → Category Capabilities → Category Management** for **店铺管理 → 单店信息同步** or **连锁店铺信息同步**. See the [token guide](https://doc.youzanyun.com/v2/doc/cloud/token/IO94wN9p3icgDgkxJbfcap5XnQd.md) for the `silent` grant with the authorized shop's `kdt_id`. Enter only the resulting opaque `access_token` in the Dex Web `accessToken` secret field. Tokens normally expire after seven days; replace the field when expired. Keep the application `client_secret` out of the Dex connection. Leave the transport settings blank for their displayed defaults.

HTTP 429, HTTP 5xx, the documented system exception code `161400100`, and transport failures retry with a five-attempt, five-minute budget because the API is metered. Definite provider rejections select optional `providerRejected`; malformed or oversized responses select optional `invalidResponse`. The example leaves optional branches unwired, so those outcomes fail the Flow. The token is sent only to the documented HTTPS endpoint and never included in results, receipts, or failure messages.

The runnable example is in [`go/examples/shop-read`](go/examples/shop-read). Provider tests use a local HTTP server. Live Youzan authorization and the production shop response remain unverified without a user-controlled application.
