# Customer.io connector

The Go, Python, and TypeScript `getCustomersByEmail` Query finds profiles with an exact email address and returns their Customer.io `cio_id` values in provider order. Empty matches return an empty `customerIDs` list. The operation uses the read-only [App API customer lookup](https://docs.customer.io/integrations/api/app/tag/customers/getpeopleemail/) endpoint; the email is percent encoded.

Go also implements `getCampaign`, `listCampaigns`, and `getCampaignMetrics` through the [App API automation endpoints](https://docs.customer.io/integrations/api/app/). The first two return campaign ID, name, active flag, and state. Metrics use version 2 with an explicit UTC Unix-second window up to ten years and a resolution of `hours`, `days`, `weeks`, or `months`; the output keeps each provider metric's numeric series. These reads use the existing App API private key. n8n's [Customer.io node](https://docs.n8n.io/integrations/builtin/app-nodes/n8n-nodes-base.customerio/) also has customer, event, and segment writes, which require the separate Track API credential model and are outside this App API connection.

## Connection

In [Customer.io](https://fly.customer.io), open **Settings > API Credentials > App API** and create a private key with profile and campaign read access, or use an existing App API key. Enter the complete `sk_` key or App API key in secret `privateKey` in Dex Web Connections. See the [App API authentication guide](https://docs.customer.io/integrations/api/app/).

Leave `baseUrl` blank for the displayed US-region default, or use `https://api-eu.customer.io` for an EU workspace. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults. Find an email on the workspace People page and pass the full address as `email`; blank is invalid. Customer.io documents a 10-requests-per-second limit and returns 429 with `Retry-After` when it is exceeded.

The [Go](go/examples/customer-lookup/README.md), [Python](python/examples/customer_lookup/README.md), and [TypeScript](typescript/examples/customer-lookup/README.md) examples run the same operation. Go-recorded conformance covers requests, outputs, branches, and retries; local Dex integration tests use fixtures. Live Customer.io authorization and lookup remain unverified.
