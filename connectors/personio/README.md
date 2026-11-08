# Personio connector

The Go `getPerson` Query reads a Personio v2 person by its API ID and returns only the ID, first name, and last name. It omits email and other personnel fields. A missing person selects `notFound`; HTTP 429 and temporary service failures request a retry.

## Connection

In Personio, open **Marketplace → Connected integrations → Create custom integration**, grant Persons read access (`personio:persons:read`), and copy the Client ID and Client Secret from the integration's Credentials page. In Dex Web **Connections**, create a `personio` connection with those values. Leave `access_token`, `baseUrl`, `timeout`, and `maxResponseBytes` blank. Dex exchanges and renews the token through Personio's client credentials endpoint. No browser redirect URI is needed.

The [runnable Go Flow](go/examples/person-inspection/README.md) uses this static connection name. Optional branches are unwired and fail the Flow when selected. Real Personio authorization and retrieval have not been verified live.

This operation follows Personio's [Retrieve a person](https://developer.personio.de/reference/get_v2-persons-id), [Obtain Access Token](https://developer.personio.de/reference/post_v2-auth-token), and [Getting started](https://developer.personio.de/docs/getting-started-with-the-personio-api) contracts.
