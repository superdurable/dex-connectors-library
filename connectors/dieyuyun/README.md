# Wuliang AI (无量AI) connector

The operator is [Dieyuyun Technology (Shenzhen) Co., Ltd.](https://open.dieyuyun.com/legal/terms.html). This Go connector calls the official [`GET https://open.dieyuyun.com/v1/balance`](https://open.dieyuyun.com/guide/api-reference.html) endpoint. The Query returns only available tenant balance in CNY yuan, preserving the exact JSON number text. It filters API Key ID, name, daily and monthly budgets, and other account details.

## Connection

Sign in to the [Wuliang AI console](https://open-console.dieyuyun.com/), open **API Keys > Key Management**, click **Create API Key**, select an application, and copy the one-time-shown secret into the Dex `api_key` credential. The [official key guide](https://open.dieyuyun.com/guide/manage-api-key.html) describes permission and budget controls. Dex sends the key only in the Authorization Bearer header. Leave base URL, timeout, and response limit blank for their displayed defaults. Disable or delete a compromised key in Key Management and create a replacement.

## Go

The [account balance Flow](go/examples/account-balance/) is a runnable Worker with static Dex Web connection name `dieyuyun`. `fetched` completes the Flow; optional branches fail it unless an application wires a business response. Read-only transient failures retry safely. The local mock integration test uses synthetic balance data and a real Dex Worker and Client. Live authorization and balance values remain unverified without a user-controlled connection.
