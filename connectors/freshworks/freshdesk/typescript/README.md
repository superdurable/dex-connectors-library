# Freshdesk TypeScript Connector

`@superdurable/connector-freshdesk` supplies Dex Step factories for five Freshdesk API v2 operations: `searchTickets`, `getTicket`, `createTicket`, `updateTicket`, and `addNote`. The package uses the same connection, operation, and branch identities as the Go connector. It has no Triggers. See the [connector README](../README.md) for Freshdesk setup, statuses, limits, and duplicate safety.

Create a Dex Web connection named `freshdesk-helpdesk`. Set `domain` to the lowercase part before `.freshdesk.com`, and save the agent's API key in the secret `api_key` field. Keep the key out of Flow input. `newProjectConnection` loads that static connection at Worker startup and resolves the current credential for each operation.

The runnable [ticket search example](examples/search-tickets/README.md) uses `newSearchTicketsStep` inside a Flow. Its `searched` branch is wired; the optional `providerRejected`, `invalidResponse`, and `defect` branches fail the Flow if selected. The other generated factories are `newGetTicketStep`, `newCreateTicketStep`, `newUpdateTicketStep`, and `newAddNoteStep`.

Run package checks with `npm ci && npm run check`. The Go-recorded fixture in `../../../../conformance/connectors/freshworks/freshdesk/scenarios.json` covers all five operations, including branches, retry classifications, request shapes, and unkeyed write uncertainty. Run the example's integration test with `DEX_FLOW_SERVICE_ADDRESS` set to a local `dexcli dev` gRPC address; its stand-in provider binds a free loopback port.

Live Freshdesk authorization and provider behavior have not been verified. The local integration test uses a Freshdesk-compatible HTTP stand-in through a real Dex Worker and Client.
