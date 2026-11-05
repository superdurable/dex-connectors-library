# Calendly Python connector

Calendly scheduled events, invitees, cancellation, scheduling links, and webhook subscriptions for Dex Flows.

The package implements six operations: `listScheduledEvents`, `getScheduledEvent`, `listEventInvitees`, `cancelScheduledEvent`, `createSchedulingLink`, and `createWebhookSubscription`. OAuth and personal access token decoding, refresh, and project connection wiring are manifest-generated. The invitee webhook Trigger remains Go-only.

See [the scheduled event example](examples/scheduled_event/README.md) for the static `calendly-scheduling` connection and Worker entrypoint. The connector source defines every operation's request and result type; the generated Step factories expose the stable branches. The `createSchedulingLink` Mutation uses Dex's single-dispatch fence and returns `uncertain` when a possibly accepted write cannot be confirmed.
