# Freshsales connector

The Go, Python, and TypeScript connectors lists contact view IDs and names with `listContactViews` and reads a contact's ID, name, and email with `getContact`. It excludes filter internals, custom fields, and other provider data from Flow results.

Sign in to [Freshsales CRM](https://app.freshworks.com/), open your profile picture > Profile Settings > API Settings, and copy the Bundle alias into `baseUrl` as the full URL ending in `/crm/sales/api`. Copy Your API key into secret `api_key`. Requests send `Authorization: Token token=<key>`. Runnable consumers: [Go](go/examples/contact-view-discovery/README.md), [Python](python/examples/contact_view_discovery/README.md), and [TypeScript](typescript/examples/contact-view-discovery/README.md). See the [Freshsales API reference](https://developers.freshworks.com/crm/api/) for the provider contract.

Both Queries retry transport failures, HTTP 429, request timeout, and server failures. `getContact` selects `notFound` for a definitive 404. Provider rejections and malformed or oversized responses select optional branches. An unwired optional branch fails the Flow. Go, Python, and TypeScript examples have been tested with a real Dex stack and deterministic provider responses. Live provider authorization has not been exercised.
