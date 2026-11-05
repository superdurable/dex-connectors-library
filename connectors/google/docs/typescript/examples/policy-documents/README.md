# Google Docs policy documents Flow

`PolicyDocumentsFlow` routes `getDocumentText`, `createDocument`,
`replaceDocumentText`, and `appendText` through the static `policy-docs`
connection. Provider requests run only inside connector Steps. Optional
failure branches are unwired, so a rejection fails the Flow.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address.
Configure the Google Docs and Drive APIs, a private Dex Web connection named
`policy-docs`, and `DEX_CONNECTOR_CONFIG_FILE` for the exported mode-0600
connections file. Follow the connector authorization guide for either Google
OAuth or Workspace domain delegation; do not place credentials in Flow input.

From `connectors/google/docs/typescript`, run the Worker:

```sh
npm run example:worker
```

In a second terminal with the same environment, edit the nonsecret example
input and start the Flow:

```sh
npm run example:start -- examples/policy-documents/input.example.json
```

The input `operation` selects one of the four operations, and `request` holds
that operation's typed input. The starter prints the successful result.
`replaceDocumentText` and `appendText` require the revision ID returned by a
read or earlier update.

Run the real Dex integration test against an in-process Google HTTP stand-in:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:45510 npm run test:integration
```

Generate and lint the Flow Definition Graph 2.0 with a released Dex CLI:

```sh
npm run build
npm install --prefix examples/policy-documents --no-save --no-package-lock --ignore-scripts .
cd examples/policy-documents
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Google authorization, document reads, Drive creation, and guarded writes
remain unverified with a real Google account.
