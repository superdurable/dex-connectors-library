# Lever connector

The Go, Python, and TypeScript `getOpportunity` Query reads one Lever opportunity's ID, candidate name, and current stage UID. Contact details, resumes, notes, and other profile fields are excluded from Dex Results.

## Connect

In [Lever](https://hire.lever.co), open **Settings > Integrations and API > API Credentials** and create an API key with opportunity read access. Copy the complete opaque secret into Dex Web `api_key`; blank is invalid. The connector uses HTTP Basic authentication with the key as username and an empty password. Revoke or rotate it on the same page. See [Lever API authentication](https://hire.lever.co/developer/documentation).

Leave `baseUrl` blank for `https://api.lever.co/v1`. `timeout` is a per-request Go duration and `maxResponseBytes` is a byte limit; blank uses their displayed defaults. Find the non-secret opportunity UID in Lever or the list opportunities response and pass it as `opportunityId`; blank is invalid. See [Retrieve a single opportunity](https://hire.lever.co/developer/documentation).

## Examples and verification

Run the [Go](go/examples/opportunity-inspection/README.md), [Python](python/examples/opportunity_inspection/README.md), or [TypeScript](typescript/examples/opportunity-inspection/README.md) opportunity inspection Flow with the static `lever` connection. Go-recorded conformance checks exact requests, branches, retries, and outputs across all three languages. Local integration tests exercise real Dex with fixture providers. Real Lever authorization and retrieval remain unverified.
