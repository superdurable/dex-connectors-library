# Calendly Connector

> **Verification status: partial live.** Only picker commands with a placeholder token reached Calendly; everything else ran on a real Dex stack against a local stand-in. No live Calendly account was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

The Calendly connector connects a Dex application to the
[Calendly API v2](https://developer.calendly.com/api-docs):

| Name | Kind | What it does | Happy branch |
| --- | --- | --- | --- |
| `listScheduledEvents` | Query | One page of scheduled events that start inside a required window, for a user, an organization, or the connected user | `listed` |
| `getScheduledEvent` | Query | One scheduled event by URI | `found` |
| `listEventInvitees` | Query | One page, 1 to 100 invitees, of a scheduled event | `listed` |
| `cancelScheduledEvent` | Mutation | Cancels a scheduled event; an already-canceled event is `canceled` too | `canceled` |
| `createSchedulingLink` | Mutation | One single-use booking link for an event type | `created` |
| `createWebhookSubscription` | Mutation | Subscribes a callback URL to invitee webhooks, reusing an existing subscription | `subscribed` |
| `inviteeEventReceived` | Trigger | A signed `invitee.created` or `invitee.canceled` webhook | |

Every other branch is optional: `notFound`, `providerRejected`,
`invalidResponse`, and `defect`; `createSchedulingLink` has `uncertain` in
place of `invalidResponse`, and `createWebhookSubscription` adds
`conflictingSubscription`. Every time in a
Result is UTC, and the list windows are echoed back in `ScheduledEventPage`, so
a Flow can see which window it checked.

## Authorization

A connection uses one of two sign-in methods, and both keep the bearer token in
the `access_token` credential:

- **Personal access token** (default, recommended for one account or
  organization). Create it at Calendly
  [Integrations > API & Webhooks](https://calendly.com/integrations/api_webhooks)
  with the scopes `users:read`, `event_types:read`, `scheduled_events:write`,
  `scheduling_links:write`, and `webhooks:write`. Calendly shows it once.
  The token is never refreshed; a `401` selects `providerRejected` with
  `AUTHENTICATION`.
- **Calendly OAuth** (for an app that other Calendly users authorize). Create
  a Web OAuth app at <https://developer.calendly.com/console/apps>, enter the
  Redirect URI Dex Web shows, select the same scopes, and copy the Client ID,
  Client Secret, and Webhook signing key. Dex opens Calendly consent with
  PKCE. Access tokens expire after two hours; `CredentialRefreshDriver`
  refreshes five minutes early at `https://auth.calendly.com/oauth/token`
  through `sdkgo/oauthtoken`, sending the client as HTTP Basic credentials as
  Calendly documents for web apps. Calendly refresh tokens are single-use, so
  each refresh persists the rotated token, and `invalid_grant`,
  `invalid_client`, and `unauthorized_client` require reauthorization and
  select `defect`. Any other refresh failure, such as Calendly's limit of eight
  tokens per user per minute, returns Retry. After a `401` the connector forces
  one refresh and repeats the request once, except `createSchedulingLink`.

`webhook_signing_key` is optional for both methods and needed only by the
Trigger. With a personal access token it is a secret you generate, such as
`openssl rand -hex 32`, that `createWebhookSubscription` registers. An OAuth
app's key is the one Calendly showed when the app was created.

Dex Web `cli-v1.1.0`, the pinned compatibility release, cannot save the
personal access token method of a connector with several sign-in methods
([superdurable/dex#570](https://github.com/superdurable/dex/pull/570), fixed
in `cli-v1.2.0`); the [example README](examples/invitee-recorder/README.md#3-configure-the-connection)
shows the connection record to write by hand.

When an operation names no user or organization, the connector reads
`GET /users/me` in the same call and uses the connected user, so no field asks
for an identity Calendly can supply.

## Operations

### Reads

`listScheduledEvents` requires both `minStartTime` and `maxStartTime` and lists
`start_time:asc` unless told otherwise. With both URIs blank it lists the
connected user's own events; `organizationUri` lists a whole organization and
needs an owner or admin token. `listEventInvitees` lists oldest booking first.
Both return one page and `nextPageToken`; a Flow pages by passing it back.
`pageSize` is 1 to 100, and zero uses Calendly's default of 20.

Results keep a connector-safe subset: conference passwords, internal meeting
notes, guest emails, payment, tracking, and SMS reminder numbers are dropped.

### Mutations and duplicate dispatch

Calendly documents no idempotency header, and Dex can run an async Step a
second time while the first attempt is still waiting. Each Mutation is
therefore safe for its own reason, proved by the real-Dex tests in
`mutation_dispatch_integration_test.go`, whose fake Calendly answers after
nine seconds:

| Mutation | Durability | Why a repeat is safe | Observed with a 9-second provider |
| --- | --- | --- | --- |
| `cancelScheduledEvent` | async | Calendly answers `403` for an event that is already canceled. A `403` reads the event back; a canceled event selects `canceled` with `alreadyCanceled`, whatever the message. A lost answer or a `5xx` returns Retry. | two POSTs, `canceled` |
| `createWebhookSubscription` | async | It lists the scope's subscriptions before creating, reuses one with the same callback URL, and treats Calendly's documented `409` as "list again". | two lists, one create, one subscription |
| `createSchedulingLink` | sync | Nothing makes a repeat safe, so it runs as a regular activity that Dex does not re-dispatch, and a `5xx`, timeout, lost connection, or unreadable `201` after the request was written selects `uncertain` instead of retrying. A `429` or a request that never left the process retries. | one POST, also under an application override to async |

`createSchedulingLink` also keeps Dex from retrying an attempt that timed out
after sending: its POST must finish three seconds before the attempt's
deadline, at most the 30-second Execute timeout, it returns Retry without
sending when less than five seconds would remain, and it does not repeat a
`401` after a token refresh. The same rule keeps an async override single,
because Dex's short local attempt leaves too little time to send. It can still
repeat if the Worker dies after Calendly created the link; the extra link is
unused and single-use. `cancelScheduledEvent` selects `providerRejected` for
an active event Calendly refuses, with `CONFLICT` once the event has started.
`createWebhookSubscription` selects `conflictingSubscription` when Calendly
answers `409` for a callback URL that another scope or user owns.

## Trigger

The `inviteeEventReceived` Trigger serves one `webhooktrigger.Endpoint` per
connection. `Connection.InviteeEventReceivedWebhookHandler` returns it, and
`NewLocalInviteeEventReceivedEndpointRunner` wraps every binding in a durable
inbox. For each delivery the endpoint:

1. accepts only `POST` up to `webhookMaxBodyBytes`, answering `405` or `413`;
2. verifies `Calendly-Webhook-Signature: t=<unix seconds>,v1=<hex>` as the
   [hex HMAC-SHA256](https://developer.calendly.com/api-docs/overview/webhooks/webhook-signatures)
   of `t`, `.`, and the raw body with `webhook_signing_key`, in constant time,
   and rejects a timestamp more than `webhookSignatureTolerance` from the
   server clock either way. A failure answers `400`; a connection without a
   signing key answers `503`, so Calendly retries. An OAuth connection whose
   access token expired while idle is refreshed first, because the local
   connection file returns no credentials for an expired token; one that needs
   reauthorization answers `503` until it is authorized again;
3. decodes `invitee.created` and `invitee.canceled`, acknowledging any other
   event, such as `routing_form_submission.created`, with `200`;
4. records the event for every binding whose `events` and `eventTypeUri`
   accept it, and answers `200` only after every record is on disk.

The event ID is the webhook event followed by the scheduled event and invitee
IDs from the invitee's stable URI, such as
`invitee.created:GBGBDCAADAEDCRZ2:AAAAAAAAAAAAAAAA`, so a redelivery keeps its
ID while the cancellation of the same invitee has its own. A reschedule sends
`invitee.canceled` for the old invitee, with `rescheduled` set, and
`invitee.created` for a new invitee URI.

The checked-in example wires the endpoint like this, from
[`examples/invitee-recorder/main.go`](examples/invitee-recorder/main.go):

```go
func newInviteeEndpointRunner(
	store *localconfig.Store, client *dex.Client, flow *inviteerecorder.Flow, logger *slog.Logger, connectionOptions []calendly.Option,
) (*calendly.InviteeEventReceivedEndpointRunner, error) {
	bindingLogger := logger.With("connector", calendly.ConnectorID, "connection", inviteerecorder.ConnectionName,
		"trigger", calendly.InviteeEventReceivedTriggerDefinition.Trigger.TriggerName, "binding", inviteerecorder.InviteeCreatedTriggerBinding)
	return calendly.NewLocalInviteeEventReceivedEndpointRunner(store, inviteerecorder.ConnectionName, []calendly.LocalInviteeEventReceivedTriggerRoute{{
		BindingName: inviteerecorder.InviteeCreatedTriggerBinding,
		Target: sdkgo.NewDexFlowTriggerTarget(client, flow, inviteerecorder.AcceptBooking, inviteerecorder.ResolveFlowID,
			inviteerecorder.MapToFlowInput, sdkgo.WithTriggerLogger(bindingLogger)),
	}}, append(slices.Clone(connectionOptions), calendly.WithLogger(logger))...)
}
```

One Trigger with an `events` filter, rather than one Trigger per event, follows
the Stripe connector: both events carry the same invitee, Calendly sends them
to one callback URL with one signing key, and one binding that accepts both
delivers a booking before its cancellation, in arrival order.

### Webhook subscription setup

Calendly creates subscriptions only through its API. Run
`createWebhookSubscription` once with the public HTTPS callback URL, or make
the one call in the [example README](examples/invitee-recorder/README.md#4-register-the-webhook-subscription).
Calendly retries a failing delivery for 24 hours and then disables the
subscription; `createWebhookSubscription` reports a disabled one as
`conflictingSubscription`, to delete and create again. Calendly never returns
a subscription's signing key, so reuse matches the callback URL only: after
changing `webhook_signing_key`, delete the subscription and create it again, or
every delivery answers `400`.

## Configuration UI

The `eventTypePicker` Studio unit reads `GET https://api.calendly.com/users/me`
and then `GET https://api.calendly.com/event_types?user=<uri>`, both fixed-host
bearer commands whose responses are JSON objects, and stores the chosen
`eventTypeUri`. A manual URI field covers event types owned by other members.
The connection surface shows status, and **Connect Calendly** only for the
OAuth method; tokens stay in Dex Web's host form and never reach the frame.

## Security boundary

- Tokens, client secrets, refresh tokens, and the signing key are
  `secretString` credentials. They never enter Flow input, Attributes,
  Results, receipts, logs, or HTTP responses.
- The connector sends a token only to `https://api.calendly.com` and
  `https://auth.calendly.com`, never follows a redirect, and reads at most
  `maxResponseBytes`.
- Failure messages are written by the connector. The only Calendly text it
  reads is the documented `Insufficient scope` error title, to say which scope
  is missing.

## Unverified live behavior

These rely on the documentation fetched on 2026-09-30, not on a live account:

- whether Calendly accepts the `client_id` and `client_secret` form fields
  that Dex Web's code exchange sends, since Calendly documents HTTP Basic for
  web apps, and whether the token response's `scope` lists the granted
  scopes, which Dex Web requires;
- whether a retried webhook delivery carries a fresh `t=` timestamp; if it
  does not, a delivery retried after `webhookSignatureTolerance` answers `400`;
- the exact `409` body for a duplicate webhook subscription, and whether
  `signing_key` is ignored or rejected for an OAuth app, which is why the
  connector omits it there;
- personal access token expiry; the connector treats the token as
  non-expiring.

## Verification

```bash
cd connectors/calendly
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1 -v
(cd ui && npm ci && npm test && npm run build)
```

The integration run needs `dexcli dev` and takes about a minute, because the
duplicate-dispatch tests wait for a nine-second provider.
