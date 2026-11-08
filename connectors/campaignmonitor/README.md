# Campaign Monitor connector

The Go and Python `listClients` Queries returns client IDs and names visible to the connected account. It does not return the client API keys or billing details. The client IDs are the resource entry point for future subscriber-list and subscriber operations.

To connect an account you control, open [Account settings > API keys](https://help.campaignmonitor.com/articles/Knowledge/api-keys), show or generate the account API key, and save it in the Dex Web secret `api_key` field. The connector uses the key as the HTTP Basic username with an empty password. Leave `baseUrl` blank for the displayed API v3.3 default. Campaign Monitor recommends OAuth for public integrations. See [API authentication](https://www.campaignmonitor.com/api/v3-3/getting-started/) and [account API](https://www.campaignmonitor.com/api/v3-3/account/).

The Query retries transport failures, HTTP 429, timeout, and server errors. A definite provider rejection and a malformed or oversized response select optional branches. The [Go client discovery Flow](go/examples/client-discovery/README.md) and [Python client discovery Flow](python/examples/client_discovery/README.md) are runnable consumers. The Python package replays the Go-recorded provider scenarios. Live Campaign Monitor authorization remains unverified.
