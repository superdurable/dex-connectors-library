# List Typeform forms

`ListFormsFlow` lists one page using the static `typeform-forms` connection. The Worker sends the provider request inside the generated `listForms` Step, then stores the safe page as a Dex Attribute.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. In Dex Web, configure the Typeform connection named `typeform-forms` with a personal token that has `forms:read`. Set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local Dex Web `connections.json` file. From `connectors/typeform/typescript`, run the Worker:

```sh
npm run example:form
```

In another terminal with the same environment, edit `examples/list-forms/input.example.json`, then start a Flow:

```sh
npm run example:form:start -- examples/list-forms/input.example.json
```

The starter prints the returned form page. The input file has no credentials.

The integration test uses a local Typeform HTTP stand-in and a real Dex Worker, Client, and persistence stack:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:35200 npm run test:integration
```

Generate and lint the strict Flow Definition Graph 2.0 from this runnable file using Dex CLI 1.28.0 or newer:

```sh
npm run build
npm install --prefix examples/list-forms --no-save --no-package-lock --ignore-scripts .
cd examples/list-forms
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Typeform authorization and API behavior have not been verified.
