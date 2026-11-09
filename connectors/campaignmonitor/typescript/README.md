# Campaign Monitor TypeScript connector

This package implements `listClients` for Dex TypeScript Flows. It replays the Go reference's provider requests, branches, retries, and exact outputs through shared conformance scenarios. Only client IDs and names enter Flow results.

For an account you control, sign in at [Campaign Monitor](https://login.createsend.com/l), open Profile image > Account settings > API keys ([guide](https://help.campaignmonitor.com/articles/Knowledge/api-keys)), and save the full key in Dex secret `api_key`. Leave `baseUrl` blank for the API v3.3 default. The package uses the key as the HTTP Basic username with an empty password. Public integrations should use OAuth. See the [connector README](../README.md) and [runnable example](examples/client-discovery/README.md). Live provider authorization remains unverified.
