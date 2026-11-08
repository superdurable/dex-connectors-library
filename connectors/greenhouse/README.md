# Greenhouse connector

The Go `getCandidate` Query uses the current [Harvest v3 candidates list API](https://harvestdocs.greenhouse.io/reference/get_v3-candidates) with `ids`, `per_page=1`, and a field projection. It returns only candidate ID, first name, and last name. An empty result selects `notFound`; 429, 408, 5xx, and transport failures retry. Python and TypeScript packages are pending.

Create a Harvest V3 (OAuth) custom integration at [Greenhouse](https://app.greenhouse.io/) > Configure > Dev Center > API Credential Management > Create new API credentials. Enable `harvest:candidates:list` under Manage permissions or scopes and ensure the integration service user has Site Admin access for list endpoints. Copy the Client ID and one-time Client Secret into Dex Web Connections. Dex exchanges them at `https://auth.greenhouse.io/token` and renews the access token; leave `access_token` blank. No redirect URI or consent screen is used for this client credentials flow. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

Pass the positive integer `candidateId` from a Greenhouse candidate page or API result. The [Go example](go/examples/candidate-inspection/README.md) uses the static `greenhouse` connection. Live Greenhouse authorization and candidate retrieval remain unverified.
