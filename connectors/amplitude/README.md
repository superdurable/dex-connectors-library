# Amplitude connector

The Go, Python, and TypeScript `searchUsers` Query searches project users by Amplitude ID, device ID, user ID, or user ID prefix. It returns the matched `userIDs` in provider order and Amplitude's `matchType`. The operation uses the read-only [Dashboard REST API User Search](https://amplitude.com/docs/apis/analytics/dashboard-rest#user-search) endpoint.

## Connection

In [Amplitude](https://amplitude.com), open **Organization settings > API Keys**, select the project, and generate or select an API key. Generate a secret key for the same project; copy its complete value when created, since the Key ID is not the secret. Enter both as secrets in Dex Web Connections. A project Manager or Admin can create keys. See [Amplitude's key guide](https://www.amplitude.com/docs/admin/account-management/manage-your-api-keys-and-secret-keys).

Leave `baseUrl` blank for the displayed US-region default, or set `https://analytics.eu.amplitude.com` for an EU project. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults. Find a user ID in Amplitude User Lookup, then pass its full value or prefix as `user`. Blank is invalid. Amplitude documents a 360-query-per-hour project limit for User Search and returns 429 when exceeded; the Query retries transient failures and honors `Retry-After`.

The [Go](go/examples/user-search/README.md), [Python](python/examples/user_search/README.md), and [TypeScript](typescript/examples/user-search/README.md) examples run the same operation. Go-recorded conformance covers requests, outputs, branches, and retries; local Dex integration tests use fixtures. Live Amplitude authorization and search remain unverified.
