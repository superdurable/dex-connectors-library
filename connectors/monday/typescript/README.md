# monday.com TypeScript connector

`@superdurable/connector-monday` provides generated Dex Step factories and
mock cases for `listItems`, `getItem`, `createItem`, `updateItemColumnValues`,
and `addUpdate`. The client resolves personal API tokens or host-refreshed OAuth
2.1 credentials at invocation time. OAuth token exchange and credential
serialization use the manifest-generated SDK declaration. No credential enters
Flow input, output, Attributes, or receipts.

The [list-items example](examples/list-items/README.md) is a runnable consuming
application with the static `monday-board` connection. Use its Worker and starter
to read a real board, or run `npm run integration:list` with `dexcli dev` for a
credential-free Worker and Client integration test. `npm run check` runs the
package suite, typecheck, and lint; the package suite replays Go-recorded
provider conformance scenarios.

This TypeScript package has no Trigger implementation. Configure provider
connection fields through Dex Web Connections; the connector's root README
and `connector.yaml` document the personal token and OAuth setup paths.
