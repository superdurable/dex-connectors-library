# Campaign Monitor Python connector

This package implements `listClients` and replays Go-recorded provider requests, branches, retries, and exact outputs. It returns client IDs and names without client API keys or billing fields.

For an account you control, sign in at [Campaign Monitor](https://login.createsend.com/l), open Profile image > Account settings > API keys ([guide](https://help.campaignmonitor.com/articles/Knowledge/api-keys)), and save the full key in Dex secret `api_key`. Leave `baseUrl` blank for the API v3.3 default. Public integrations should use OAuth. See the [connector README](../README.md) and [runnable example](examples/client_discovery/README.md). Live provider authorization remains unverified.
