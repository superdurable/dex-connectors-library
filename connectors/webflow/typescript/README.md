# Webflow connector for TypeScript

`@superdurable/connector-webflow` provides typed `listCollectionItems` and
`getCollectionItem` Query Steps for staged Webflow CMS items. Both use the
static `webflow` connection in the [runnable Flow](examples/list-items/flow.ts).

Configure a Dex project connection with a Webflow `site_token` scoped to
`CMS:read`. The connector manifest supplies the public API endpoint, a 15-second
request timeout, and a 2 MiB response limit; `baseUrl`, `timeout`, and
`maxResponseBytes` may be overridden in the connection configuration.

From this directory, run `npm ci`, then start `dexcli dev` and
`npm run example:list-items`. Start `WebflowListItems` with an input such as
`{"collectionId":"62b720ef280c7a7a3be8cabe","limit":10}`. The Worker
loads the named connection from the Dex project and resolves its credential
for each call. The optional rejection, invalid response, and defect branches
are unwired, so selecting one fails the Flow.

For credential-free integration verification, start `dexcli dev` and run
`npm run integration:list-items`. It executes the same Flow with a generated
mock through a real Dex Worker and Client and checks retry and completion.
The provider replay fixtures additionally verify request and branch parity with Go.

Regenerate from the repository root with
`GOWORK=off go run ./cmd/connectorctl generate connectors/webflow/connector.yaml`.
For strict FDG 2.0 visualization, run
`dexcli visualize examples/list-items/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/webflow-list-items`.
