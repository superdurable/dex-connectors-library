# List Trello board cards

`ListCardsFlow` lists one bounded page through the static `trello-approved-board` connection. It persists the nonsecret board selection and returned card page. The provider call runs in the connector Step.

Start `dexcli dev` on a free port in 21000–21099, and set `DEX_FLOW_SERVICE_ADDRESS` to its Flow service address. Configure the private connection named `trello-approved-board` with the Trello API key and user token using the [connector setup guide](../../../README.md#authentication). Set `DEX_CONNECTOR_CONFIG_FILE` to the local Dex Web `connections.json` file.

From `connectors/atlassian/trello/typescript`, run the Worker:

```sh
npm run example:run
```

In a second terminal using the same connection, edit `input.example.json` with your board ID and start the Flow:

```sh
npm run example:start -- examples/approved-board-cards/input.example.json
```

The starter prints the card page. No credential belongs in the input file.

Run the integration test with a local Trello HTTP fixture against a running Dex stack:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21001 npm run test:integration
```

The test verifies a completed card page and an unwired provider rejection. Real Trello credentials and provider behavior have not been exercised.

Generate and lint the strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or newer. From `connectors/atlassian/trello/typescript`, make the local package available to the analyzer, then run:

```sh
npm run build
npm install --prefix examples/approved-board-cards --no-save --no-package-lock --ignore-scripts .
cd examples/approved-board-cards
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
