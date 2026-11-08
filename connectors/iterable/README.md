# Iterable connector

The Go, Python, and TypeScript `getUser` Queries read one user's email and optional `userId` from an email-based or hybrid Iterable project. Arbitrary profile data fields are excluded from Dex Results.

## Connect

Sign in to the correct [Iterable project](https://app.iterable.com). An org admin or member with **Manage Integrations** permission opens **Integrations > API Keys > New API Key**, names the key, chooses **Server-side**, and copies its complete value when shown. Save it only in the Dex Web connection's secret `api_key` field. The connector sends it in the `Api-Key` header. Disable or delete the key later under **Integrations > API Keys**. See [Iterable's API key guide](https://support.iterable.com/hc/en-us/articles/360043464871-API-Keys).

Leave `baseUrl` blank for a US project. For an EU project set `https://api.eu.iterable.com/api`; the key and URL must belong to the same data center. `timeout` is a per-request Go duration and `maxResponseBytes` is a byte limit; blank uses each displayed default. Find the user's email in **Audience > Users** and pass it as `email`; blank is invalid. This operation supports email-based and hybrid projects. See [Iterable's user endpoint](https://support.iterable.com/hc/en-us/articles/204780579-Iterable-API-Endpoints-and-Sample-Payloads).

## Examples and verification

Run the [Go](go/examples/user-inspection/README.md), [Python](python/examples/user_inspection/README.md), or [TypeScript](typescript/examples/user-inspection/README.md) user inspection Flow with the static `iterable` connection. Go-recorded conformance checks requests, branches, retries, and outputs across all three languages. Local integration tests exercise real Dex with fixture providers. Real Iterable authorization and user retrieval have not been verified locally.
