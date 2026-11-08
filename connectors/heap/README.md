# Heap connector

The Go, Python, and TypeScript `setUserProperties` Mutations send absolute string property values for one stable identity to Heap's server-side [Add User Properties API](https://developers.heap.io/reference/add-user-properties). Repeating the same request overwrites those values with themselves; the Result only reports acceptance and never returns identity or properties.

## Connect

Sign in to [Heap](https://heapanalytics.com), open **Projects**, select the **Main Production** environment, and copy its numeric environment app ID into Dex Web `appId`. The ID is not secret; blank prevents calls. Heap's Add User Properties endpoint does not use an authorization token. Leave `baseUrl` blank for the displayed US endpoint; for EU projects enter `https://c.eu.heap-api.com`. Do not append the API path. Leave `timeout` and `maxResponseBytes` blank for the displayed defaults; overrides use a positive Go duration and a positive byte count respectively. See [Heap's regional endpoint guidance](https://developers.heap.io/reference/server-side-apis-overview).

Pass a unique, permanent identity already used by Heap and one to fifty non-secret string properties, with names and values under 1024 bytes. A missing identity creates a user profile without events. Heap blocks `id` as a property name. Do not use a shared identity such as `guest`: merging people under one identity damages analytics. The server-side endpoint permits 30 requests per 30 seconds per identity and app ID. Select the right production or test app ID before starting a Flow.

The runnable [Go](go/examples/user-properties/README.md), [Python](python/examples/user_properties/README.md), and [TypeScript](typescript/examples/user-properties/README.md) examples use the static `heap` connection. Go-recorded conformance scenarios cover request shape, validation, rejection, and retryable statuses across all three languages. Each example's mock integration test runs against a real Dex server. Real Heap ingestion and Dex Web configuration have not been verified with a Heap account.
