# Kit TypeScript connector

This package implements `listSubscribers` and `getSubscriber` for Dex TypeScript Flows. Both operations replay the Go reference's provider requests, branches, retries, and exact outputs through shared conformance scenarios. Embedded relationships and custom field values are excluded from Flow results.

Open [Kit Developer settings](https://app.kit.com/account_settings/developer_settings), choose Add a new key under V4 API Keys, and copy the one-time value into the Dex Web connection secret `api_key`. Leave `baseUrl` blank for the API v4 default. The package sends the key in the `X-Kit-Api-Key` header. The [connector README](../README.md) describes operation inputs and defaults; [subscriber inspection](examples/subscriber-inspection/README.md) is the runnable example.

Live Kit authorization and responses have not been verified locally. Package release is independent of the Go and Python releases.
