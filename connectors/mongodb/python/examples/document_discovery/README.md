# MongoDB Python document discovery

This runnable Flow reads one collection through the static `mongodb`
connection. Configure the read-only URI and database in Dex Web, then run
`uv run python -m examples.document_discovery.worker` from `python/` with
`DEX_PROJECT_*` and `DEX_FLOW_SERVICE_ADDRESS` set. Start
`MongoDBPythonDocumentDiscovery` with input such as:

```json
{"collection":"orders","filter":{"status":"active"}}
```

Generate strict FDG 2.0 with
`dexcli visualize examples/document_discovery/flow.py --schema-version 2.0 --json --python .venv/bin/python --out /tmp/mongodb-python-fdg --lint app`.
The integration test runs this same Flow through a real Dex Worker and Client
with the generated mock. Live MongoDB access remains unverified.
