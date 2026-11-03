# Gorgias TypeScript connector

Install `@superdurable/connector-gorgias@0.1.0` after publication. The package
provides `searchTickets`, `getTicket`, `findCustomerByEmail`, `createTicket`,
`updateTicket`, and `addNote` Step factories and typed mock cases. It has no
Triggers.

Create a `GorgiasClient` with the helpdesk `domain` and a credential provider
that resolves the current user email and secret API key. Bind it with
`newConnection(client, { provider: 'gorgias', name: ConnectionName })`, or load
the private Dex project configuration with `newProjectConnection`. The runnable
[read-ticket example](examples/read-ticket/README.md) shows the Step factory,
Worker, and Flow starter with the static `gorgias-helpdesk` connection name.

Gorgias has no idempotency key. `createTicket` and `addNote` write a stable
external ID and checkpoint dispatch before a provider write; subsequent
attempts read the resource back and select `uncertain` when it cannot be
confirmed. Ticket read-back skips malformed entries in an otherwise valid
page. A public reply reads its ticket before sending; an invalid response or
ambiguous email route selects `providerRejected` without sending. `updateTicket`
reads current values and applies only requested differences. Unwired optional
branches fail the Flow.

The [Gorgias setup instructions](../README.md#gorgias-setup) describe where an
account admin finds the domain, user email, and API key. The TypeScript package
replays the Go provider conformance scenarios and has run through Dex against a
local provider stand-in. Live Gorgias authorization and behavior remain unverified.
