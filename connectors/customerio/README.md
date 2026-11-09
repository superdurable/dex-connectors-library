# Customer.io connector

The Go, Python, and TypeScript `getCustomersByEmail` Query finds profiles with an exact email address and returns their Customer.io `cio_id` values in provider order. Empty matches return an empty `customerIDs` list. The operation uses the read-only [App API customer lookup](https://docs.customer.io/integrations/api/app/tag/customers/getpeopleemail/) endpoint; the email is percent encoded.

## Connection

In [Customer.io](https://fly.customer.io), open **Settings > API Credentials > App API** and create a private key with profile-read access, or use an existing App API key. Enter the complete `sk_` key or App API key in secret `privateKey` in Dex Web Connections. See the [App API authentication guide](https://docs.customer.io/integrations/api/app/).

Leave `baseUrl` blank for the displayed US-region default, or use `https://api-eu.customer.io` for an EU workspace. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults. Find an email on the workspace People page and pass the full address as `email`; blank is invalid. Customer.io documents a 10-requests-per-second limit and returns 429 with `Retry-After` when it is exceeded.

The [Go](go/examples/customer-lookup/README.md), [Python](python/examples/customer_lookup/README.md), and [TypeScript](typescript/examples/customer-lookup/README.md) examples run the same operation. Go-recorded conformance covers requests, outputs, branches, and retries; local Dex integration tests use fixtures. Live Customer.io authorization and lookup remain unverified.
