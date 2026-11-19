# Easemob IM Python connector

`dex-connector-easemob` implements the read-only `getUserDetail` Query for Dex Flows. It follows the [single-user API](https://doc.easemob.com/document/server-side/account_detail_obtain_single.html), returning user ID, UUID, creation and modification times in Unix milliseconds, active state, and optional nickname. Push tokens and other provider fields are discarded.

## Connection

Create a static `easemob` connection in Dex Web. In the [Easemob Console](https://console.easemob.com), open **Application Management > Application List**, select the app, and copy its data-center REST HTTPS host into `baseUrl` (host root only). Under **Application Overview > Development Configuration**, copy the App Key in `orgname#appname` form to `appKey`, the Client ID to `clientId`, and the private Client Secret to secret `clientSecret`. Blank required fields prevent requests. Leave `timeout` and `maxResponseBytes` blank for displayed defaults; Python timeout is in seconds and response bound in bytes. The Query input `userId` is that app's registered lowercase user ID.

The connector obtains an [App Token](https://doc.easemob.com/document/server-side/easemob_app_token.html) through `POST /{org}/{app}/token` with `grant_type=client_credentials`. It caches the token until shortly before `expires_in` and forces one refresh when a user GET receives HTTP 401. It never stores the token, Client Secret, or push token in Flow data. HTTP 429/5xx and transport errors retry. Definite provider rejection selects optional `providerRejected`; malformed or oversized responses select `invalidResponse`; invalid local input or credentials select `defect`.

The [runnable user read Flow](examples/user_read/README.md) stores only the selected nonsecret account fields in a Dex Attribute. Its integration test uses inert credentials and a fake provider with a real Dex Worker and Client. Live Easemob authorization and production user retrieval remain unverified without customer-controlled credentials.
