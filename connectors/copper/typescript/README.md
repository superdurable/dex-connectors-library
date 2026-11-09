# Copper TypeScript connector

This package implements `listLeadStatuses` and `getLead` for Dex TypeScript Flows. Both operations replay the Go reference's provider requests, branches, retries, and exact outputs through shared conformance scenarios. Custom fields and email addresses remain outside Flow results.

Sign in as an admin to [Copper](https://app.copper.com/), open **Settings > Integrations > API Keys**, generate a key, and save it in the Dex Web secret `api_key` field. Save the generating user's Copper login email in `user_email`. Leave `baseUrl` blank for the Developer API v1 default. The [connector README](../README.md) describes operation inputs and defaults; [lead status discovery](examples/lead-status-discovery/README.md) is the runnable Flow.

JavaScript numbers cannot represent Copper numeric IDs above the safe integer limit exactly; such provider responses select `invalidResponse`. Live Copper authorization and responses have not been verified locally. Package release is independent of the Go and Python releases.
