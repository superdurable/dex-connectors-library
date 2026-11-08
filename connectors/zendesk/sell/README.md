# Zendesk Sell connector

The Go, Python, and TypeScript connectors read lead identities through `listLeads` and `getLead`. They exclude custom fields, addresses, activity, and provider links from Flow state. These reads support existing Sell workflows and data migration.

Sign in to [Zendesk Sell](https://app.futuresimple.com/) with account management privileges, then open Integrations > OAuth > Access Tokens. Add a User Token and copy its one-time value into Dex secret `access_token`. The connector sends it in an `Authorization: Bearer` header. The API root defaults to `https://api.getbase.com/v2`. See the [Sell authentication guide](https://developer.zendesk.com/documentation/sales-crm/first-call/) and [Leads API](https://developer.zendesk.com/api-reference/sales-crm/resources/leads/). Runnable consumers: [Go](go/examples/lead-discovery/README.md), [Python](python/examples/lead_discovery/README.md), and [TypeScript](typescript/examples/lead-discovery/README.md).

Both Queries retry transport errors, HTTP 429, request timeout, and server failures. `getLead` selects `notFound` on a definitive 404. Provider rejections and malformed or oversized responses select optional branches. An unwired optional branch fails the Flow. All three examples are verified against a local Dex stack with deterministic provider responses. Live provider authorization has not been exercised.

Zendesk [plans to retire Sell on August 31, 2027](https://support.zendesk.com/hc/en-us/articles/9591462550042-Announcing-the-retiring-of-Zendesk-Sell). Existing customers retain access until then; new integrations should account for migration.
