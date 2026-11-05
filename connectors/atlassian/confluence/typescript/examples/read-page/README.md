# Read a Confluence page

`ReadConfluencePageFlow` reads one page with the static `confluence-policy`
connection. The Worker invokes `getPage` inside a Dex Step and the Flow returns
only the page ID, title, and current version. It does not persist the page body.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address.
Create the private `confluence-policy` connection in Dex Web using the OAuth
setup described in the [connector README](../../README.md). Set
`DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 project `connections.json` file.
From `connectors/atlassian/confluence/typescript`, run:

```sh
npm ci
npm run example:read
```

In a second terminal with the same project configuration, edit
`examples/read-page/input.example.json` to name a page the connection can
view, then start the Flow:

```sh
npm run example:read:start -- examples/read-page/input.example.json
```

The starter prints the safe page summary. No OAuth token belongs in the input
file. The local integration test uses a Confluence stand-in bound to an OS
assigned port and a real Dex Worker and Client:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:38100 npm run test:integration
```

The test assumes a `dexcli dev` server already listens at that address. The
example also supplies the FDG 2.0 Step explanations and group annotations;
from this package directory, analyze `examples/read-page/flow/workflow.ts`
with `dexcli visualize --language typescript --schema-version 2.0 --lint app`.
Dex CLI v1.33.0 reports `dynamic_step_target` for the generated connector
QueryStep registered through its operation factory, followed by
`unknown_step_target` for the incoming `goTo`. The Worker and Client
integration test exercises this route successfully.
The stand-in test does not verify live Atlassian OAuth or page behavior.
