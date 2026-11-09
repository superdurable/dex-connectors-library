# MongoDB document discovery

This Flow reads matching `orders` documents and completes with canonical
Extended JSON. It uses the static Dex connection name `mongodb`.

From `connectors/mongodb/go`, generate and lint the strict FDG 2.0 source:

```bash
dexcli visualize ./examples/document-discovery/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/mongodb-document-discovery --lint app
```

Run a Dex development server (`dexcli dev --open=false`) and register the
connector release or a local release override as described in
`docs/versioning-and-releases.md`. In Dex Web **Connections**, save a ready
`mongodb / mongodb` connection with the database name and a secret connection
URI. Start the Worker with the corresponding `DEX_PROJECT_*` configuration:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 go run ./examples/document-discovery
```

In **Start Flow**, choose `MongoDBDocumentDiscovery` and a unique Flow ID.
For example:

```json
{"collection":"orders","filter":{"status":"active"}}
```

The `found` branch completes with `documents`; an empty array means there
were no matches. Optional branches fail the Flow when selected. The mock
integration test verifies one result through a real Dex Worker and Client:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  GOWORK=off go test -tags=integration ./examples/document-discovery/flow \
  -run TestDocumentDiscoveryUsesDefaultMockWithRealDex -count=1
```

Live MongoDB access is optional and remains unverified until a user-controlled
read-only connection is available.
