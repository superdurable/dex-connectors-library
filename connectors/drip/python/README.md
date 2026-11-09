# Drip Python connector

This package implements `listAccounts` and `getSubscriber`, replaying Go-recorded provider requests, branches, retries, and exact outputs. It returns account IDs and names, and subscriber ID, email, name, and status. Custom fields and tracking data stay outside Flow results.

For a private integration, open [your Drip user profile](https://www.getdrip.com/user/edit), copy the personal API Token, and save it in the Dex secret `api_key` field. Leave `baseUrl` blank for the API v2 default. Public integrations use OAuth. See the [connector README](../README.md) and [runnable example](examples/account_discovery/README.md). Live provider authorization remains unverified.
