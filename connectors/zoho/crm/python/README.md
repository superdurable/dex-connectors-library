# Zoho CRM Python connector

`dex-connector-zoho-crm==0.1.0` implements `findRecords`, `getRecord`, `listModifiedRecords`, `upsertRecord`, `updateRecord`, and `listModuleFields` for Dex Python applications. It pins `dex-connectors-sdk==0.7.0`. There are no Triggers.

The [record lookup Flow](examples/record_lookup/flow.py) and its [Worker entrypoint](examples/record_lookup/main.py) open the static `zoho-crm-main` connection from Dex project configuration. Configure that connection in Dex Web and choose the Zoho data center matching the account. The [example guide](examples/record_lookup/README.md) includes the real Dex integration command.

The manifest owns OAuth endpoints, scopes, credential mappings, refresh rules, generated models, Step factories, and connection bindings. The hand-written `Client` owns the provider HTTP exchange; each operation invokes it in `operations.py`. The provider errors persisted in Dex contain only safe Zoho code and field identifiers, not provider message text or credentials.

Run `uv run ruff check src tests examples`, `uv run mypy --strict src`, and `uv run pytest tests` from this directory. From the repository root, run `make test-connector-conformance CONNECTOR_DIRECTORY=connectors/zoho/crm`. The Go-recorded fixture checks all six operations against the Python SDK path. Live Zoho OAuth and provider calls remain unverified.
