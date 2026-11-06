# LinkedIn TypeScript connector

`@superdurable/connector-linkedin` reads the authenticated member's bounded OpenID Connect UserInfo profile through `getAuthenticatedProfile`. The runnable example uses the static `linkedin-signup` connection.

Create or select an app at [LinkedIn Developers](https://www.linkedin.com/developers/apps). In Products, request **Sign in with LinkedIn using OpenID Connect**. In Auth, register the exact Dex Web redirect URI under Authorized redirect URLs, then copy the Client ID and Client Secret into the Dex Web connection. Complete consent for `openid`, `profile`, and `email`. LinkedIn issues programmatic refresh tokens only to approved products; other grants require interactive reauthorization when they expire. Dex stores tokens in the connection, outside Flow input and Attributes.

The UserInfo URL, 15 second HTTP timeout, and 1 MiB response limit come from the connector manifest. Override them only for a compatible endpoint or a different operational bound. The URL may use HTTP only for loopback testing.

Start `dexcli dev`, configure the connection in Dex Web, then run `npm run example:profile` for the Worker. In another terminal, run `npm run example:profile:start -- examples/authenticated-profile/input.example.json` to start the Flow. Set `DEX_FLOW_SERVICE_ADDRESS` for a nondefault Dex endpoint. The [example Flow](examples/authenticated-profile/flow/workflow.ts) wires only `profileLoaded`; all other branches fail it.

Run `npm run check` for package tests and static checks. With a local Dex dev server, run `DEX_FLOW_SERVICE_ADDRESS=<host:port> npm run test:integration` to exercise a real Worker and Client against an OS assigned local LinkedIn stand-in. Go conformance fixtures replay the provider outcomes. Live LinkedIn authorization and API behavior have not been verified by this package test.
