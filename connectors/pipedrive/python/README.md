# Pipedrive Python connector

`dex-connector-pipedrive` exposes the six manifest operations: `searchObjects`, `getObject`, `listObjects`, `upsertObject`, `createObject`, and `updateObject`. It requires `dex-connectors-sdk==0.6.0` and `dex-python-sdk>=1.28.0`. The package has no Triggers.

Load a named Pipedrive connection from Dex project configuration with `new_project_connection(project, "pipedrive-crm")`. The generated connection binds Personal API token credentials or Pipedrive OAuth credentials and handles refresh through the generated manifest driver. Provider calls resolve credentials at dispatch time; never put a token in Flow input or an Attribute.

See the runnable [deal lookup example](examples/deal_lookup/README.md) for a Flow, Worker, start command, and a real Dex integration test. `getObject` returns a complete record; search returns partial records, which can be hydrated through `getObject`. Non-happy branches are optional and fail an example Flow unless explicitly wired for a business reaction. The source of truth for authorization and configuration is the connector's [`connector.yaml`](../connector.yaml).

Run the package checks from this directory with `uv sync` and `uv run pytest`. Run provider replay from the repository root with `make test-connector-conformance CONNECTOR_DIRECTORY=connectors/pipedrive`. Live Pipedrive authorization and API behavior are unverified.
