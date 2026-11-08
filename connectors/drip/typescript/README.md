# Drip TypeScript connector

This package implements `listAccounts` and `getSubscriber` for Dex TypeScript Flows. Both operations replay the Go reference's provider requests, branches, retries, and exact outputs through shared conformance scenarios. Private account settings and subscriber custom fields remain outside Flow results.

Open [your Drip user profile](https://www.getdrip.com/user/edit), copy the personal API Token, and save it in the Dex Web secret `api_key` field. Leave `baseUrl` blank for the API v2 default. The token is sent as the HTTP Basic username with an empty password. The [connector README](../README.md) describes operation inputs and defaults; [account discovery](examples/account-discovery/README.md) is the runnable Flow.

Live Drip authorization and responses have not been verified locally. Package release is independent of the Go and Python releases.
