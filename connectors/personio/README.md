# Personio connector

The Go connector v0.2.0 provides `getPerson` and `listPersons` Queries. `listPersons` accepts a cursor, a page limit up to 50, exact first and last names, and ACTIVE or INACTIVE status. It returns only IDs and names plus the next cursor. The Python and TypeScript v0.1.0 packages still provide `getPerson` only. Both Go reads omit email and other personnel fields. A missing person selects `notFound`; HTTP 429 and temporary service failures request a retry.

## Connection

In Personio, open **Marketplace → Connected integrations → Create custom integration**, grant Persons read access (`personio:persons:read`), and copy the Client ID and Client Secret from the integration's Credentials page. In Dex Web **Connections**, create a `personio` connection with those values. Leave `access_token`, `baseUrl`, `timeout`, and `maxResponseBytes` blank. Dex exchanges and renews the token through Personio's client credentials endpoint. No browser redirect URI is needed. Person create and update require `personio:persons:write`, which this connection does not request; adding that scope would break existing connections.

The [runnable Go](go/examples/person-inspection/README.md), [Python](python/examples/person_inspection/README.md), and [TypeScript](typescript/examples/person-inspection/README.md) Flows use this static connection name. Optional branches are unwired and fail the Flow when selected. Real Personio authorization and retrieval have not been verified live.

The operations follow Personio's [Retrieve a person](https://developer.personio.de/reference/get_v2-persons-id), [List persons](https://developer.personio.de/reference/get_v2-persons), [Obtain Access Token](https://developer.personio.de/reference/post_v2-auth-token), and [Getting started](https://developer.personio.de/docs/getting-started-with-the-personio-api) contracts.
