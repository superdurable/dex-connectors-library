# Wave connector

The Go, Python, and TypeScript `getCustomer` Query reads one customer from a Wave business by GraphQL ID. It sends a fixed, read-only query to [Wave's public GraphQL API](https://developer.waveapps.com/hc/en-us/articles/360032911011-Query-Get-customer-by-id) and returns the business ID, customer ID, and name. A missing business or customer selects the optional `notFound` branch. Rate limits, server failures, and transport failures retry.

## Connection and configuration

For an account you own, open the [Wave Developer Portal](https://developer.waveapps.com/) and choose **Manage Applications**. Create or open your own application and generate a **Full Access Token** as described in [Wave's business-owner guide](https://developer.waveapps.com/hc/en-us/articles/360020596571-Permitted-Use-Wave-Business-Owners). Enter the complete token in the secret `accessToken` field of Dex Web Connections. The token grants broad account access; keep it private and rotate it in the portal when needed. Blank is invalid. This connector currently supports owner-managed tokens, not third-party OAuth connections.

Leave `baseUrl` blank for the Wave production GraphQL root. Override it only for a trusted test gateway; do not append `/graphql/public`. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults. In Wave, select the business and customer whose GraphQL IDs you want to read. Pass both IDs as `businessId` and `customerId`; blank IDs are invalid. The [API call guide](https://developer.waveapps.com/hc/en-us/articles/50682329964564-4-Making-API-calls) documents the endpoint and Bearer authorization.

Runnable examples are in [`go/examples/customer-read`](go/examples/customer-read), [`python/examples/customer_read`](python/examples/customer_read), and [`typescript/examples/customer-read`](typescript/examples/customer-read). Provider conformance scenarios are recorded from Go and replayed by Python and TypeScript. Live Wave authorization and provider behavior require a user-controlled token and remain unverified here.
