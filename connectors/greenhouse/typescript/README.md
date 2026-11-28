# Greenhouse TypeScript connector

Reads individual candidates and pages of candidate IDs and names through Harvest v3. Go-recorded conformance checks exact requests, branches, retries, and outputs. Configure Harvest v3 client credentials and the `harvest:candidates:list` scope using the [connection guide](../README.md). See the [runnable Dex Flow](examples/candidate-inspection/README.md). Live Greenhouse authorization remains unverified.

The [pagination Flow](examples/candidate-inspection/list-flow.ts) accepts a page size from 1 to 500. Use zero for the provider default. To resume a page, pass its `nextCursor` and omit the size. An empty cursor in the result marks the last page.
