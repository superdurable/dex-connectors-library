# Campaign Monitor Python connector

This package implements `listClients` and replays Go-recorded provider requests, branches, retries, and exact outputs. It returns client IDs and names without client API keys or billing fields.

For an account you control, open [Campaign Monitor API keys](https://help.campaignmonitor.com/articles/Knowledge/api-keys) through Profile image > Account settings > API keys and save the full key in Dex secret `api_key`. Leave `baseUrl` blank for the API v3.3 default. Public integrations should use OAuth. See the [connector README](../README.md) and [runnable example](examples/client_discovery/README.md). Live provider authorization remains unverified.
