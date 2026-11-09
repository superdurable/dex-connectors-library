# MongoDB TypeScript document discovery

This runnable Flow reads one collection through the static `mongodb` Dex Web
connection. Configure a read-only URI and database, then run
`npm run example:worker` from `typescript/` with `DEX_PROJECT_*` and
`DEX_FLOW_SERVICE_ADDRESS` set. Start `MongoDBTypeScriptDocumentDiscovery`
with a unique Flow ID and input such as:

```json
{"collection":"orders","filter":{"status":"active"}}
```

From `typescript/`, link this unreleased package locally and generate strict
FDG 2.0:

```bash
mkdir -p node_modules/@superdurable
ln -s ../.. node_modules/@superdurable/connector-mongodb
dexcli visualize examples/document-discovery/flow.ts \
  --schema-version 2.0 --json --out /tmp/mongodb-ts-fdg --lint app
```
The Dex integration test runs this Flow with its manifest mock. Live Atlas
authorization remains unverified.
