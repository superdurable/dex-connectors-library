# Easemob IM connector

The Go `getUserDetail` Query calls the current [single-user REST API](https://doc.easemob.com/document/server-side/account_detail_obtain_single.html) and returns selected account fields: user ID, UUID, creation and modification times in Unix milliseconds, active state, and push nickname. It discards other response fields, including push tokens. The Query does not modify the account.

## Configure Dex Web Connections

At the [Easemob Console](https://console.easemob.com), open **Application Management > Application List** and select a test or production App Key. In **Application Overview**, copy the REST host assigned to that app's data center and the App Key (`orgname#appname`), Client ID, and Client Secret from **Development Configuration**. Enter the host root as `baseUrl` and the three credentials in their matching fields. The Client Secret is stored as a Dex secret. The connector obtains an [App Token](https://doc.easemob.com/document/server-side/easemob_app_token.html), caches it until shortly before expiry, and refreshes it once if the user endpoint returns HTTP 401. Leave `timeout` and `maxResponseBytes` blank for the displayed defaults.

The Query selects optional `defect` for invalid local inputs, `providerRejected` for definite provider errors, and `invalidResponse` for malformed or oversized JSON. HTTP 429, HTTP 5xx and transport failures retry. The example leaves optional branches unwired, so those outcomes fail the Flow.

See the runnable [`go/examples/user-read`](go/examples/user-read). Tests use a local HTTP provider and a real Dex server with the generated mock. Live Easemob authorization and user retrieval remain unverified.
