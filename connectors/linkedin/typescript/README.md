# LinkedIn TypeScript connector

`@superdurable/connector-linkedin` reads the authenticated member's bounded OpenID Connect UserInfo profile through `getAuthenticatedProfile` and polls the connected member's own profile for `profileChanged`. The query example uses `linkedin-signup`; the [Trigger example](examples/profile-changed/README.md) uses `linkedin-profile-watcher`.

Create or select an app at [LinkedIn Developers](https://www.linkedin.com/developers/apps). In Products, request **Sign in with LinkedIn using OpenID Connect**. In Auth, register the exact Dex Web redirect URI under Authorized redirect URLs, then copy the Client ID and Client Secret into the Dex Web connection. Complete consent for `openid`, `profile`, and `email`. LinkedIn issues programmatic refresh tokens only to approved products; other grants require interactive reauthorization when they expire. Dex stores tokens in the connection, outside Flow input and Attributes.

The UserInfo URL, 15 second HTTP timeout, and 1 MiB response limit come from the connector manifest. Override them only for a compatible endpoint or a different operational bound. The URL may use HTTP only for loopback testing.

## profileChanged Trigger

`defineProfileChangedTriggerBinding` declares a fieldless binding: it watches the member who authorized its connection. `newProfileTriggerRunner` uses a supplied cursor or memory; `newProjectProfileTriggerRunner` uses the project cursor and inbox. The first poll records a baseline without an event. Later polls compare name, given name, family name, email, email verification, picture address, and locale. A renewed picture signature does not count as a change. A different connected member or invalid cursor records a new baseline. A provider 429 pauses the connection using Retry-After when supplied. A failed delivery stays in the durable inbox and replays after restart.

The connection's **Poll interval** defaults to the manifest value and must be at least one minute. Each poll sends a UserInfo request, so prefer a longer interval for members who rarely edit their profile. The binding has no user-supplied fields. The existing `openid profile email` grant supplies UserInfo access.

Build and run the [profile-changed Worker](examples/profile-changed/README.md) with `npm run example:profile-changed` after configuring `linkedin-profile-watcher` in Dex Web. It starts a `LinkedInProfileChangeTypeScript` Flow per changed profile using the stable Trigger event ID.

Start `dexcli dev`, configure the connection in Dex Web, then run `npm run example:profile` for the Worker. In another terminal, run `npm run example:profile:start -- examples/authenticated-profile/input.example.json` to start the Flow. Set `DEX_FLOW_SERVICE_ADDRESS` for a nondefault Dex endpoint. The [example Flow](examples/authenticated-profile/flow/workflow.ts) wires only `profileLoaded`; all other branches fail it.

Run `npm run check` for package tests and static checks. With a local Dex dev server, run `DEX_FLOW_SERVICE_ADDRESS=<host:port> npm run test:integration` to exercise real Workers and Clients against local LinkedIn stand-ins. Go conformance fixtures replay provider Query outcomes and the 20 Go-recorded profile fetcher scenarios. Live LinkedIn authorization and API behavior have not been verified by this package test.
