# Review PagerDuty incidents

`IncidentReviewFlow` and `IncidentLookupFlow` use the static Dex Web connection name `pagerduty`. The Worker calls PagerDuty inside the generated `newListIncidentsStep` or `newGetIncidentStep`, then persists the returned page or detail as a Flow Attribute. Its input and output contain no credential.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. Configure the `pagerduty` connection following the [connector authorization guide](../../../README.md#authorization), then set `DEX_CONNECTOR_CONFIG_FILE` to the local Dex Web `connections.json` file. From `connectors/pagerduty/typescript`, run:

```sh
npm run example:list
```

In a second terminal with the same environment, run:

```sh
npm run example:list:start -- examples/incident-review/input.example.json
npm run example:list:start -- examples/incident-review/incident.example.json
```

For a local real Dex integration check with a credential-safe provider fixture, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dex-port> npm run test:integration`. The fixture binds an available local port and covers a successful page, a successful detail lookup, a rate-limit retry, and unwired provider rejection. Provider authorization and the live API have not been verified.

Generate the strict Flow Definition Graph 2.0 with `dexcli visualize examples/incident-review/flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false` after building the package.
