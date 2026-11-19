# Bilibili TypeScript connector

`@superdurable/connector-bilibili` provides `getAuthorizedUser`, a read-only Query for the authorized account's public `openID` and `name`. The host owns `clientID`, `appSecret`, and `accessToken`; the connector resolves them per call and signs each provider request. Credentials never enter Flow input or persisted output.

The runnable [authorized user example](examples/authorized-user/README.md) binds the static `bilibili` connection. Configure its credentials in Dex Web Connections using the guide in `connector.yaml`. The package includes Go scenario replay tests, including rejection, rate limiting, transport failure, and signed request bytes. The example integration test exercises a real Dex Worker and Client with a local provider fake. Real Bilibili authorization and API responses remain unverified without a user-owned account.

Install with `npm install @superdurable/connector-bilibili`. Exported `BilibiliClient` validates its origin and limits during construction. `newProjectConnection` loads the named connection from a Dex project. `GetAuthorizedUserOperation` is normally reached through the generated `newGetAuthorizedUserStep` factory. An approved transport or deterministic signing clock and nonce can be injected through `BilibiliClientOptions` for tests.
