# Ashby connector

The Go, Python, and TypeScript `getCandidate` Query reads one candidate's ID, name, and creation/update timestamps. Email, phone, resumes, and custom fields are excluded from Dex Results.

## Connect

In [Ashby](https://app.ashbyhq.com), an administrator opens **Admin > API Keys** and creates an API key with **Candidates read** permission. Copy the complete opaque secret into the Dex Web connection's `api_key` field. The connector sends it as the HTTP Basic username with an empty password. Revoke or rotate the key from the same page. Confidential candidates require the additional confidential jobs/projects permission. See [Ashby authentication](https://developers.ashbyhq.com/reference/authentication).

Leave `baseUrl` blank for `https://api.ashbyhq.com`. `timeout` is a per-request Go duration, and `maxResponseBytes` is a byte limit; blank uses the displayed defaults. Find the non-secret candidate UUID in Ashby or from `candidate.list` and pass it as `candidateId`; blank is invalid. See [candidate.info](https://developers.ashbyhq.com/reference/candidateinfo) and [API response conventions](https://developers.ashbyhq.com/reference/responses).

## Examples and verification

Run the [Go](go/examples/candidate-inspection/README.md), [Python](python/examples/candidate_inspection/README.md), or [TypeScript](typescript/examples/candidate-inspection/README.md) candidate inspection Flow with the static `ashby` connection. Go-recorded conformance checks exact requests, branches, retries, and outputs across all three languages. Local integration tests exercise real Dex with fixture providers. Real Ashby authorization and candidate retrieval remain unverified.
