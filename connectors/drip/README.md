# Drip connector

The Go and Python connectors list accessible account IDs and names with `listAccounts`, then reads one subscriber's ID, email, name, and status with `getSubscriber`. Address, custom fields, tags, and tracking data remain outside Flow results. Use an account ID from `listAccounts` and a subscriber ID or email in `getSubscriber`.

For a private integration, open [your Drip user profile](https://www.getdrip.com/user/edit), copy the personal API Token, and save it in the Dex Web secret `api_key` field. The connector sends it as the HTTP Basic username with an empty password. Leave `baseUrl` blank for the Drip API v2 default. Public integrations use OAuth. See the [Drip API reference](https://developer.drip.com/) for authentication and endpoint details.

Both queries retry transport failures, HTTP 429, timeout, and server failures. Definite rejections, malformed or oversized JSON, invalid input, and a missing subscriber select optional branches. The [Go account discovery Flow](go/examples/account-discovery/README.md) and [Python account discovery Flow](python/examples/account_discovery/README.md) are runnable consumers. The Python package replays Go-recorded provider scenarios. Live Drip authorization remains unverified.
