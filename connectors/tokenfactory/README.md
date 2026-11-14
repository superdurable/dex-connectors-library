# TokenFactory

The Go connector reads a snapshot of a TokenFactory account's general balance in Chinese yuan and whether the authenticated API Key's account is active. `GetBalance` has no input and makes no model call. The [official balance API reference](https://docs.tokenfactory.cn/api-reference/usage/account-balance-openai) states that this endpoint does not charge, change API Key state, or write usage records. A small negative balance is possible while calls are in flight.

Sign in to the [TokenFactory console](https://console.tokenfactory.cn/), complete real-name verification, and open **API Keys** to create or reveal a Key. Enter it in the secret Dex Web `api_key` field without `Bearer`. The connector sends `GET https://api.tokenfactory.cn/v1/user/balance` with the Key in an Authorization header. See the [official quick start](https://docs.tokenfactory.cn/guide/quickstart) and [key management guide](https://docs.tokenfactory.cn/enterprise/security). TokenFactory is operated by [无锡伟瑞达科技有限公司](https://docs.tokenfactory.cn/legal/about).

The [runnable Dex Flow](go/examples/balance-check/README.md) shows connection setup and output. The logo is the unmodified [official website SVG](https://tokenfactory.cn/favicon.svg). No live account authorization has been verified.
