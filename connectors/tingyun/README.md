# Tingyun connector

This connector reads the monitored mobile App list through the [official Tingyun App API](https://wukongdoc.tingyun.com/app/api/basic.html). It calls `GET <platform origin>/app-api/app/list` and returns only each application's numeric ID and name. The provider response includes `appToken`; the connector excludes it from durable results and receipts.

The [official API overview](https://wukongdoc.tingyun.com/app/api/) documents an HTTPS platform host such as `https://wukong2.tingyun.com`, and the `authorization: token <accessToken>` header. Sign in to your Tingyun platform, hover over the lower-left account name, open **账户管理 > API**, and generate an accessToken. Enter its platform origin and the secret token in Dex Web Connections. The connector accepts only HTTPS `*.tingyun.com` hosts and disables redirects; private hosts are outside this version.

The [runnable Flow](go/examples/list-applications/README.md) uses the static `tingyun` connection. Real accessToken authorization and Dex Web form interaction remain unverified.
