# Chargebee connector

The Go, Python, and TypeScript `getSubscription` Query reads a Chargebee subscription's ID, status, and currency code. Customer and payment details stay out of Dex Results.

## Connect

Sign in to your Chargebee Test or Live site at [app.chargebee.com](https://app.chargebee.com). In **Settings > Configure Chargebee > API Keys and Webhooks > API Keys**, add a **Read-Only** API key, then save its complete secret in Dex Web `api_key`. Chargebee uses the key as the HTTP Basic username with an empty password. See [Chargebee API keys](https://www.chargebee.com/docs/billing/2.0/site-configuration/api_keys) and [authentication](https://apidocs.chargebee.com/docs/api/v2/pcv-1/auth).

Set the non-secret `baseUrl` to `https://{site}.chargebee.com/api/v2`, replacing `{site}` with the subdomain of the Chargebee site that issued the key. Test and Live keys are separate. `timeout` is a per-request Go duration and `maxResponseBytes` is a byte limit; blank uses the displayed defaults. Find a subscription ID in Chargebee Subscriptions or an API response and pass it as `subscriptionId`. See [Retrieve a subscription](https://apidocs.chargebee.com/docs/api/subscriptions/retrieve-a-subscription).

## Examples and verification

Run the [Go](go/examples/subscription-inspection/README.md), [Python](python/examples/subscription_inspection/README.md), or [TypeScript](typescript/examples/subscription-inspection/README.md) Flow with the static `chargebee` connection. Go-recorded conformance checks requests, branches, retries, and outputs across all three languages. Local integration tests exercise real Dex with fixture providers. Real Chargebee authorization and retrieval remain unverified.
