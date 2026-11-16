# Gorgias TypeScript connector

Install `@superdurable/connector-gorgias@0.2.0` after publication. The package
provides `searchTickets`, `getTicket`, `findCustomerByEmail`, `createTicket`,
`updateTicket`, and `addNote` Step factories and typed mock cases, and the
`ticketChanged` poll Trigger. It requires `@superdurable/connectors-sdk` 0.12.0,
which adds the poll Trigger kit, and `@superdurable/dex` 1.45.4 or newer.

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

## ticketChanged Trigger

`ticketChanged` delivers each Gorgias ticket event recorded after the binding
first started, such as `ticket-created`, `ticket-updated`, or `ticket-closed`,
as a `TicketChangedEvent` with `eventId`, `eventType`, `ticketId`, `occurredAt`,
`userId` (omitted for a rule), and `context`. Each poll reads `GET /api/events`
in `created_datetime` order, and the behavior matches the Go connector, which
the [connector README](../README.md#ticketchanged-trigger) describes in full:
the first poll records a baseline at Gorgias's `Date` header and delivers
nothing, each poll rereads a 15-second overlap and skips events it has seen, it
reads at most five pages of 100 events per poll and stops paging when
`X-Gorgias-Account-Api-Call-Limit` shows three quarters of the window spent, a
429 pauses the connection for `Retry-After`, a 400 backs off and retries from
the same position, and only an undecodable cursor records a new baseline. Event
IDs are `gorgias-event-<id>`. Cursor tokens keep Go's nanosecond times and JSON
encoding, so Go and TypeScript replicas share one binding;
`test/ticket_changed_go_parity.test.ts` replays a recording of the Go fetcher
to prove it.

The connection's `pollInterval`, `pollIntervalMs` on `GorgiasClient`, is the
delay between polls; the client rejects one under 10 seconds.
`newProjectConnection(project, name, options)` passes client options such as a
Trigger `logger`.

| Function | Use |
| --- | --- |
| `newProjectTicketTriggerRunner(project, connection, config)` | Durable delivery from a `LoadedProject`: each route's saved configuration is loaded (an unsaved binding delivers every ticket event type), and each event is recorded in the binding's project Trigger inbox before its cursor commits. |
| `newTicketTriggerRunner(config)` | One poll `Runner` for routes with your own targets and cursor stores; a route without a cursor keeps it in memory. |
| `newTicketChangedTrigger(config)` | One in-memory `TriggerRunner`, like Go's generated `NewTicketChangedTrigger`; it records a new baseline on every start. |
| `defineTicketChangedTriggerBinding(config)` | Declares the binding in a Flow's `getConnectorTriggerBindings`. Dex Web shows no field for it. |

`eventTypes` narrows deliveries to some of `TicketEventTypes`; skipped events
still advance the cursor. The
[ticket-changed example](examples/ticket-changed/README.md) wires a Flow target
in [`app.ts`](examples/ticket-changed/app.ts):

```ts
return newProjectTicketTriggerRunner(project, connection, {
  ticketChangedRoutes: [{ bindingName: TicketChangedTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptEveryTicketChange, resolveFlowId, mapToFlowInput) }],
});
```

Live Gorgias polling remains unverified.
