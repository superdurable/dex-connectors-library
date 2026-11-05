# Figma TypeScript connector

`@superdurable/connector-figma` v0.1.0 provides the `getFile` and
`getFileComments` Dex Query operations. Import `newGetFileStep` and
`newGetFileCommentsStep` from the package and bind them to a static `figma`
connection. The generated models, branches, definitions, connection helpers,
and mock answers come from `connector.yaml`.

## Connection and behavior

In Dex Web Connections, create a Figma connection named `figma`. Open
[Figma settings](https://www.figma.com/settings), then Account menu > Settings
> Security > Personal access tokens > Generate new token. Select
`file_content:read` and `file_comments:read`, then paste the token into the
secret `api_key` field. Leave `baseUrl`, `timeout`, and `maxResponseBytes`
blank for their displayed defaults. The client resolves the secret for each
call and keeps it out of Flow state and errors.

Both reads Retry 408, 409, 429, 5xx, and interrupted transport. A 404 selects
`notFound`; other final 4xx responses select `providerRejected`; malformed or
oversized JSON selects `invalidResponse`. Invalid file keys and unavailable
credentials select `defect` before a provider request.

## Example and verification

The [Figma lookup example](examples/figma-lookup/README.md) contains the
Worker entrypoint, typed two-read Flow, and a real Dex Worker and Client
integration check. From this directory, run `npm ci`, then `npm run check`.
The provider replay cases under `conformance/connectors/figma/scenarios.json`
are Go-recorded and compared by the TypeScript package test suite.

Live Figma token authorization, account permissions, and provider responses
have not been verified.
