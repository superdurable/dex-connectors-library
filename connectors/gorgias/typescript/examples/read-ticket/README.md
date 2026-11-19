# Read one Gorgias ticket

`ReadTicketFlow` uses the static `gorgias-helpdesk` connection and returns one
ticket with its latest messages. The `found` branch completes the Flow; other
optional branches fail it. No credential enters Flow input or output.

Start a Dex development stack and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC
address. Configure the private Dex project connection named
`gorgias-helpdesk` with `domain`, the Gorgias user email, and that user's API
key as described in the [connector setup](../../../README.md#gorgias-setup).
Set `DEX_CONNECTOR_CONFIG_FILE` to the private connections file. From
`connectors/gorgias/typescript`, run the Worker:

```sh
npm run example:read-ticket
```

In another terminal, provide a real ticket ID in `input.example.json` and
start the Flow:

```sh
npm run example:read-ticket:start -- examples/read-ticket/input.example.json
```

The starter prints the ticket details. To verify against real Dex with a local
Gorgias-compatible stand-in, run `DEX_FLOW_SERVICE_ADDRESS=<dex-address>
npm run test:integration`.

Generate strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or newer:

```sh
npm run build
npm install --prefix examples/read-ticket --no-save --no-package-lock --ignore-scripts .
cd examples/read-ticket
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Gorgias access has not been exercised.
