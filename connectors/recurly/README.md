# Recurly connector

The Go, Python, and TypeScript `getAccount` Queries read one Recurly account's ID and stable code. Contact, billing, and payment details are excluded from Dex Results.

## Connect

Sign in to the [Recurly admin console](https://app.recurly.com) with Integration permission. Open **Integrations > API Credentials**, scroll to **Add Private API Key**, name the integration, and save. Copy the complete unencoded secret key into Dex Web `api_key`. Recurly Private API Keys grant full account access. Revoke or regenerate a key under **Developers > API Keys**. The connector uses HTTP Basic with the key as username and an empty password. See [Recurly's REST API key guide](https://docs.recurly.com/recurly-subscriptions/docs/api-keys).

Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. `baseUrl` is the fixed `https://v3.recurly.com` root. Find the account's non-secret ID in account details or a Recurly API response and pass it as `accountId`. To query by account code, prefix it with `code-`, such as `code-acme`; blank is invalid. Every request pins `Accept: application/vnd.recurly.v2021-02-25+json`. See [the account endpoint](https://docs.recurly.com/recurly-subscriptions/v2021-02-25/reference/get_account) and [versioning guide](https://docs.recurly.com/recurly-subscriptions/v2021-02-25/reference/getting-started-v2021-02-25).

## Examples and verification

Run the [Go](go/examples/account-inspection/README.md), [Python](python/examples/account_inspection/README.md), or [TypeScript](typescript/examples/account-inspection/README.md) account inspection Flow with the static `recurly` connection. Go-recorded conformance checks exact requests, branches, retries, and outputs across all three languages. Local integration tests exercise real Dex with fixture providers. Real Recurly authorization and account retrieval have not been verified locally.
