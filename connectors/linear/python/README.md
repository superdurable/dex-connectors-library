# Linear Python connector

`dex-connector-linear` provides seven operation-specific Dex Steps for Linear:
`searchIssues`, `getIssue`, `createIssue`, `updateIssue`, `addComment`,
`findUserByEmail`, and `listWorkflowStates`. It also provides the signed
`issueEventReceived` Trigger. Python requires `dex-connectors-sdk>=0.12.0,<1`
and Dex Python SDK 1.28.0 or newer.

The generated project connection supports the manifest's personal API key and
Linear OAuth methods. OAuth access tokens refresh before expiry and once after
an HTTP 401 through the generated connection and SDK credential source. Shared
connections do not expose the client secret or refresh token to applications.
Configure credentials in Dex Web **Connections**; never place them in Flow
input or persisted values. See the [Linear setup guide](../README.md#linear-setup)
for provider application pages, scopes, and credential fields.

The [workflow-state example](examples/list_workflow_states) and
[Issue event example](examples/issue_event/README.md) are runnable consuming
applications. The workflow-state example uses the static connection `linear`
and only wires the `listed` branch. Other branches fail the Flow. Its real Dex integration
test exercises a 429 retry, result persistence, and an unwired rejection with an
inert provider stand-in. The 54 provider replay scenarios compare Python
requests and outcomes to Go recordings. Live Linear behavior and OAuth consent
were not verified for this Python port.

```sh
uv sync --locked
uv run pytest
uv run ruff check src tests examples
uv run mypy src/dex_connectors/linear
```

The Issue event example serves a signed ASGI webhook, stores each delivery before acknowledgment, and starts a real Dex Flow. Its integration test verifies replay after a Trigger restart and stable Flow identity on redelivery. Unit tests cover signing, timestamp freshness, filtering, size limits, missing secrets, and restart. Live Linear webhook delivery remains unverified.
