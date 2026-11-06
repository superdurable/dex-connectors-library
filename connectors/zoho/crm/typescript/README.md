# Zoho CRM TypeScript connector

`@superdurable/connector-zoho-crm@0.1.0` implements `findRecords`, `getRecord`, `listModifiedRecords`, `upsertRecord`, `updateRecord`, and `listModuleFields` for Dex TypeScript applications. It pins `@superdurable/connectors-sdk@0.7.0`. There are no Triggers.

The [record lookup Flow](examples/record-lookup/flow.ts) and [Worker entrypoint](examples/record-lookup/main.ts) open the static `zoho-crm-main` connection from Dex project configuration. Configure that connection in Dex Web and select the Zoho data center where the account lives. The [example guide](examples/record-lookup/README.md) shows how to run the Worker and its real Dex integration test.

The [manifest](../connector.yaml) owns OAuth endpoints, scopes, credential mappings, refresh rules, generated models, Step factories, and connection bindings. The generated refresh driver renews the access token and validates the returned `api_domain` against the selected data center. The hand-written provider client sends bounded HTTP requests, and each operation classifies its own result into the Go connector's branches. Provider message text and credentials do not enter Dex results or receipts.

Run `npm ci && npm run check` from this directory. From the repository root, run `make test-connector-conformance CONNECTOR_DIRECTORY=connectors/zoho/crm`; the Go-recorded fixture replays all six TypeScript operations. Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40842 npm run test:integration` while `dexcli dev -dex-port 40842 -web-port 40843 -open=false` serves the local Dex stack. Live Zoho OAuth and provider calls remain unverified.
