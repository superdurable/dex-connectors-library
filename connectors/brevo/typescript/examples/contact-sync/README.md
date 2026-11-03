# Brevo contact sync

`ContactSyncFlow` creates or updates a contact by email, reads back its Brevo
identity, and returns the ID and email. It uses the static Dex Web connection
name `brevo-contacts`; no credential belongs in Flow input or persisted state.
Only `saved` and `found` are wired. An optional branch fails the Flow.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address.
Configure the private Dex Web connection `brevo-contacts` with the API key from
[Brevo API keys](https://app.brevo.com/settings/keys/api). Set
`DEX_CONNECTOR_CONFIG_FILE` to the local Dex Web `connections.json` file.
From `connectors/brevo/typescript`, run the Worker:

```sh
npm run example:sync
```

In another terminal, start the Flow with the checked-in input:

```sh
npm run example:sync:start -- examples/contact-sync/input.example.json
```

Run the fixture-backed real Dex integration test with a local dev server:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21065 npm run test:integration
```

For a Flow Definition Graph 2.0 check, build the package, expose it to the
example's module resolution, and run the Dex CLI analyzer:

```sh
npm run build
npm install --prefix examples/contact-sync --no-save --no-package-lock --ignore-scripts .
cd examples/contact-sync
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Brevo API key setup and real-provider calls have not been verified.
