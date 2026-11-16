# Apifox connector for Python

Install `dex-connector-apifox==0.1.0`. The `getProjectAPISummary` Query
returns only the title, version, and HTTP endpoint count from an exported
OpenAPI 3.1 document. The document and token are never persisted in Flow state.
The request follows Apifox's [export guidance](https://docs.apifox.com/export-data)
and uses the fixed API version declared in the connector manifest.

Create a Dex Web connection named `apifox` following the
[authorization guide](../README.md). Provide the project ID as a
positive decimal string from Project Settings > Basic Settings.
`providerRejected`, `invalidResponse`, and `defect` are optional branches; an
unwired optional outcome fails the Flow. HTTP 408, 429, 5xx, and transport
failures retry.

[`examples/project_summary`](examples/project_summary/flow.py) is a runnable
Flow. Configure the connection in Dex Web and run `uv run python -m
examples.project_summary.worker` with `dexcli dev`. Run `uv run pytest tests`
for Go-recorded provider conformance. Live Apifox authorization and export
have not been verified with an account.
