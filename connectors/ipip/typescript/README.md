# IPIP TypeScript connector

`@superdurable/connector-ipip` implements the read-only `lookupIPLocation` Query. It accepts an IPv4 or IPv6 address and returns the provider’s default country, region, city, and country code fields. The token is resolved from Dex Connections and sent only to IPIP over HTTPS; it never enters Flow input or state.

Create or obtain an online query token through [IPIP’s v2 API instructions](https://www.ipip.net/document/api_v2.html) and the [IPIP account](https://user.ipip.net/). The public API guide does not name an exact token page; ask [IPIP support](https://www.ipip.net/contact.html) if it is absent from your account. Configure the secret token in Dex Connections. Leave timeout and maxResponseBytes blank for manifest defaults.

Temporary transport and HTTP 408/429/5xx responses retry. Provider refusals select `providerRejected`; malformed or oversized responses select `invalidResponse`; invalid input or credentials select `defect`. Optional branches fail the Flow unless wired to a required business reaction.

`examples/basic/` contains a runnable Flow, Worker, and Client. `npm run check` replays the Go provider scenarios. Run `npm run test:integration` against a real `dexcli dev -open=false` server to verify retry and persistence. Live IPIP authorization is unverified without a user-controlled provider connection.
