# Twilio Segment connector

Go, Python, and TypeScript implement the `getSource` Query. It reads a Source ID, slug, and display name through Segment Public API. The result excludes Source write keys and configuration.

## Connect

Sign in at [Segment](https://app.segment.com), select the Workspace, and open **Settings > Access Management > Tokens > Create Token**. Create a **Public API** token with Sources read access. Copy the complete secret into the Dex Web connection's `api_key`; blank is invalid. Dex sends it as a Bearer token. Revoke or rotate it on the same Tokens page. A Source write key or Config API token cannot replace this token. See [Segment authentication](https://docs.segmentapis.com/tag/Authentication/).

Leave `baseUrl` blank for a US Workspace. For an EU Workspace, use `https://eu1.api.segmentapis.com`. This is an HTTPS root, without `/sources` or credentials. `timeout` is a positive Go duration per request; `maxResponseBytes` is a positive byte count. Blank uses the displayed defaults. Find the non-secret Source ID in the Workspace Sources page or a [Public API Source response](https://docs.segmentapis.com/tag/Sources/); pass it as `sourceId`. The slug is a separate value. See [regional endpoints](https://docs.segmentapis.com/tag/Getting-Started/).

## Examples and verification

Run the [Go](go/examples/source-inspection/README.md), [Python](python/examples/source_inspection/README.md), or [TypeScript](typescript/examples/source-inspection/README.md) Flow using the static `segment` connection. Go-recorded scenarios check exact requests, branches, retries, and outputs across all languages. Local integration tests exercise Dex against fixture providers. Real Segment authorization and retrieval have not been verified.
