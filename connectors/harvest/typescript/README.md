# Harvest TypeScript connector

`@superdurable/connector-harvest` provides the `listProjects` and `listTimeEntries` Dex Query Steps. The package resolves the secret Harvest API key for each call and sends it only in the Authorization header. The nonsecret decimal `accountId` is sent in `Harvest-Account-Id`.

Create a Dex Web connection named `harvest`. At [Harvest ID > Developers > Personal Access Tokens](https://id.getharvest.com/developers), create a personal access token and copy its secret value into `api_key`. Copy the decimal account ID shown with that token into `accountId`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

## Example

The [Harvest lookup example](examples/harvest-lookup/README.md) reads a project page, then a time entry page. Its Worker binds the static `harvest` connection. The integration check runs the same Flow through a real Dex Worker and Client with the manifest generated mock.

## Behavior

Both Queries accept `cursor` and `perPage`; blank cursor selects the first page and zero `perPage` requests 100 records. `perPage` may be 1 through 2000. The result includes `nextCursor` when Harvest supplies a valid next page URL. A definitive provider rejection selects `providerRejected`, malformed or oversized JSON selects `invalidResponse`, and invalid local input or unavailable credentials selects `defect`. Rate limits, 408, 409, 5xx, and transport failures Retry under the default one hour Step budget. Provider response bodies and credentials are excluded from failures and Flow state.

The conformance suite replays Go recorded provider scenarios. Authorization and response behavior against a live Harvest account have not been verified.
