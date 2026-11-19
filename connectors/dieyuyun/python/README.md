# Wuliang AI Python connector

`dex-connector-dieyuyun` provides the `getAccountBalance` Query. It sends `GET /v1/balance` with no query or body and returns only `availableCNY`, the exact JSON decimal text in CNY yuan. The API Key ID, name, budget details, and provider error text do not enter the result or failure.

## Connection

Sign in to the [Wuliang AI console](https://open-console.dieyuyun.com/), open **API Keys > Key Management**, click **Create API Key**, select an application, and copy the one-time-shown secret `sk-` Key into Dex Connections as `api_key`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for the displayed defaults. The API origin must be an HTTPS host root unless a trusted test server or proxy is approved; timeout is a Go duration and response limit is bytes. There are no operation fields. Read-only transient failures retry under the Dex Step policy.

See `examples/account_balance/` for a runnable Dex Flow and Worker. Run `uv run pytest tests` to replay Go-recorded HTTP scenarios. Live Wuliang AI authorization and actual balance were not verified without a user-controlled connection.
