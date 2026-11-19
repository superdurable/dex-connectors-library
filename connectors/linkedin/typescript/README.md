# LinkedIn TypeScript connector

`@superdurable/connector-linkedin` 0.2.0 reads the authenticated member's bounded OpenID Connect UserInfo profile through `getAuthenticatedProfile`, and its `profileChanged` poll Trigger reports each change of that same member's own profile claims. Install it with `@superdurable/connectors-sdk` 0.12.0, which adds the poll Trigger kit, and Dex TypeScript SDK 1.45.4 or later. The authenticated-profile example uses the static `linkedin-signup` connection.

Create or select an app at [LinkedIn Developers](https://www.linkedin.com/developers/apps). In Products, request **Sign in with LinkedIn using OpenID Connect**. In Auth, register the exact Dex Web redirect URI under Authorized redirect URLs, then copy the Client ID and Client Secret into the Dex Web connection. Complete consent for `openid`, `profile`, and `email`. LinkedIn issues programmatic refresh tokens only to approved products; other grants require interactive reauthorization when they expire. Dex stores tokens in the connection, outside Flow input and Attributes.

The UserInfo URL, 15 second HTTP timeout, and 1 MiB response limit come from the connector manifest. Override them only for a compatible endpoint or a different operational bound. The URL may use HTTP only for loopback testing.

Start `dexcli dev`, configure the connection in Dex Web, then run `npm run example:profile` for the Worker. In another terminal, run `npm run example:profile:start -- examples/authenticated-profile/input.example.json` to start the Flow. Set `DEX_FLOW_SERVICE_ADDRESS` for a nondefault Dex endpoint. The [example Flow](examples/authenticated-profile/flow/workflow.ts) wires only `profileLoaded`; all other branches fail it.

## profileChanged Trigger

`profileChanged` delivers one `ProfileChangedEvent` whenever the connected member's own `name`, `given_name`, `family_name`, `email`, `email_verified`, `picture`, or `locale` claim changes. It watches only the member who authorized the connection, never other members. Each poll sends one `GET https://api.linkedin.com/v2/userinfo` with the existing `openid profile email` grant and compares the claims with the profile committed in the binding's poll cursor. Behavior matches the Go connector, which the [connector README](../README.md#profilechanged-trigger) describes in full: the first poll records a baseline and delivers nothing, a re-signed picture URL is not a change, event IDs hash the `sub`, both profile hashes, and the delivered-change count, an undecodable cursor or another member's profile reseeds with a WARN record, and a 429 pauses the connection for `Retry-After`. The cursor token, profile hash, stored claims, and event IDs are byte-compatible with Go, including Go's JSON escaping, UTF-8 replacement, whitespace trimming, email parsing, and picture URL canonicalization, so Go and TypeScript replicas can share one binding; `test/profile_changed_go_parity.test.ts` replays a Go recording to prove it.

The connection's `pollInterval` setting, `Config.pollIntervalMs` in code, is the delay between polls; `LinkedInClient` rejects one under `MinimumPollIntervalMs`, one minute.

| Function | Use |
| --- | --- |
| `newProjectProfileTriggerRunner(project, connection, routes)` | Durable delivery from a `LoadedProject`: each change is recorded in the binding's project Trigger inbox before its profile commits. |
| `newProfileTriggerRunner(connection, routes)` | One poll `Runner` for `ProfileChangedTriggerRoute` values with your own targets and cursor stores; a route without a cursor keeps it in memory. |
| `newProfileChangedTrigger({ connection, bindingName, target })` | One in-memory `TriggerRunner`, like Go's generated `NewProfileChangedTrigger`; it records a new baseline on every start. |
| `defineProfileChangedTriggerBinding({ connectionName, bindingName })` | Declares the binding in a Flow's `getConnectorTriggerBindings`. Dex Web shows no field for it. |

The [profile-changed example](examples/profile-changed/README.md) wires a Flow target in [`app.ts`](examples/profile-changed/app.ts):

```ts
const runner = newProjectProfileTriggerRunner(project, connection, [{ bindingName: ProfileChangedTriggerBinding, target: newProfileChangeFlowTarget(client, flow, options) }], options.logger ? { logger: options.logger } : {});
```

Run it with `npm run example:profile-changed`.

Run `npm run check` for package tests and static checks. With a local Dex dev server, run `DEX_FLOW_SERVICE_ADDRESS=<host:port> npm run test:integration` to exercise both examples on a real Worker and Client against local LinkedIn stand-ins. Go conformance fixtures replay the provider outcomes. Live LinkedIn authorization, API, and polling behavior have not been verified by this package test, including whether a 429 carries `Retry-After` and whether re-signing a picture changes only its query.
