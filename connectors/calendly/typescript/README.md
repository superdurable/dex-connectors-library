# Calendly TypeScript connector

`@superdurable/connector-calendly` implements `listScheduledEvents`, `getScheduledEvent`, `listEventInvitees`, `cancelScheduledEvent`, `createSchedulingLink`, and `createWebhookSubscription` for Dex Flows. The signed `inviteeEventReceived` Trigger remains Go-only.

Configure the `calendly-scheduling` example connection in Dex Web. A personal access token comes from [Calendly Integrations > API & Webhooks](https://calendly.com/integrations/api_webhooks). For OAuth, create a Web app at the [Calendly developer console](https://developer.calendly.com/console/apps), register Dex Web's displayed redirect URI, and enable `users:read`, `event_types:read`, `scheduled_events:write`, `scheduling_links:write`, and `webhooks:write`. Dex holds the token, client secret, and refresh token in the connection; the generated project credential provider rotates OAuth credentials before use. The `webhook_signing_key` is needed only to create a subscription with a personal access token and to verify deliveries in a Go Trigger application.

With `dexcli dev` running, use `npm run example:event` for the [Worker](examples/scheduled-event/main.ts), then `npm run example:event:start -- examples/scheduled-event/input.example.json` to start one Flow. Set `DEX_FLOW_SERVICE_ADDRESS` if Dex runs on a different address. The Flow stores only the provider-safe scheduled event. The example has a real Dex Worker and Client integration test with a local Calendly stand-in.

Run `npm run check` for conformance replay, package tests, typecheck, and lint. Run `DEX_FLOW_SERVICE_ADDRESS=<host:port> npm run test:integration` against a running Dex dev server. Live Calendly authorization, token refresh, API calls, and webhook registration remain unverified.
