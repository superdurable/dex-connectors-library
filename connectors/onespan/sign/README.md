# OneSpan Sign connector

The Go, Python, and TypeScript `getPackage` Queries read a transaction package's ID, name, and status from the OneSpan Sign REST API. They exclude signer identities and document contents from the Dex result.

## Connection

Sign in to the OneSpan Sign account as its owner. Open **Admin > API Access > Authentication Settings > API Key and Client Apps**, then reveal the API Key from the **API Key** section. Store its complete opaque value in the Dex Web connection's secret `api_key` field. The connector sends `Authorization: Basic <api-key>`; it does not base64 encode the key. If the key is unavailable, ask OneSpan support to enable access. See OneSpan's [API access guidance](https://docs.onespan.com/docs/creating-and-sending-a-transaction).

Leave `baseUrl` blank for the displayed Sandbox endpoint. For Production, use the `/api` root corresponding to the account's login environment, such as `https://apps.esignlive.com/api`. `timeout` is a per-request Go duration and `maxResponseBytes` is a byte limit; leave both blank for their displayed defaults. Enter the non-secret package ID from a transaction URL or create-package response as `packageId`. See OneSpan's [environment URLs](https://docs.onespan.com/docs/environment-urls-ip-addresses) and [retrieve transaction API](https://docs.onespan.com/docs/managing-transactions).

## Example and verification

Run the [Go](go/examples/package-inspection/README.md), [Python](python/examples/package_inspection/README.md), or [TypeScript](typescript/examples/package-inspection/README.md) package inspection Flow with a static `onespan-sign` connection. Go-recorded conformance covers all three implementations; each has a mock-backed real Dex integration test. Real OneSpan authorization and package retrieval have not been verified locally.
