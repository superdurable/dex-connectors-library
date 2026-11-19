# Pipedrive Python connector

`dex-connector-pipedrive` exposes the six manifest operations: `searchObjects`, `getObject`, `listObjects`, `upsertObject`, `createObject`, and `updateObject`. It requires `dex-connectors-sdk>=0.12.0,<1` and `dex-python-sdk>=1.28.0`. The package has no Triggers.

Load a named Pipedrive connection from Dex project configuration with `new_project_connection(project, "pipedrive-crm")`. The generated connection binds Personal API token credentials or Pipedrive OAuth credentials and handles refresh through the generated manifest driver. Provider calls resolve credentials at dispatch time; never put a token in Flow input or an Attribute.

See the runnable [deal lookup example](examples/deal_lookup/README.md) for a Flow, Worker, start command, and a real Dex integration test. `getObject` returns a complete record; search returns partial records, which can be hydrated through `getObject`. Non-happy branches are optional and fail an example Flow unless explicitly wired for a business reaction. The source of truth for authorization and configuration is the connector's [`connector.yaml`](../connector.yaml).

From 0.1.1, `createObject` and the create path of `upsertObject` resend an unknown outcome, such as a 5xx, 409, 402, redirect, unlisted 4xx, or lost response, while possible sends stay below the Step's send cap, and select `uncertain` at the cap; `upsertObject` searches the identity before every resend. A definite refusal (400, 401, 403, 404, 411, 413, 414, 415, 422, or the daily token budget 429) selects `providerRejected` only on a first send. `updateObject` is repeat-safe and retries any status that does not prove a refusal.

`listObjects` accepts the Go JSON timestamp spellings for `updatedSince` and `updatedUntil`, including comma fractions and offsets through 24:60, and sends the filters as whole-second UTC times.

Open-ended `fields` and `customFields` values retain their JSON number spelling when Python can encode it unchanged. A number that would round or change spelling makes a read select `invalidResponse`; an already accepted write can instead be `uncertain`.

Run the package checks from this directory with `uv sync` and `uv run pytest`. Run provider replay from the repository root with `make test-connector-conformance CONNECTOR_DIRECTORY=connectors/pipedrive`. Live Pipedrive authorization and API behavior are unverified.
