# Atlassian Confluence TypeScript connector

`@superdurable/connector-atlassian-confluence` provides Dex Query and Mutation
operations for Confluence Cloud pages and footer comments. Its five operations
are `searchPages`, `getPage`, `createPage`, `updatePage`, and `addComment`.
The public models, branch definitions, Step factories, connection helpers, and
mock cases are generated from `../connector.yaml`; OAuth credential refresh
uses the generated manifest declaration. The provider client and operation
behavior follow the Go connector, including page title and version read-back
for writes and an at-most-once checkpoint for footer comments.

A consuming application is checked in at
[examples/read-page](examples/read-page/README.md). It loads the private Dex
project connection `confluence-policy`, reads one page inside a Dex Step, and
returns only its ID, title, and version number. Run its real Dex integration
test with `DEX_FLOW_SERVICE_ADDRESS` set to the local `dexcli dev` gRPC address.

Run `npm ci && npm run check` to build and test the package. The conformance
suite replays the recorded Go provider scenarios under
`../../../../conformance/connectors/atlassian/confluence/scenarios.json`.
Live Atlassian authorization, site selection, writes, and comment delivery
remain unverified.
