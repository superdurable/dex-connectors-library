# Jira issue lookup Flow

This Python Flow reads one Jira Cloud issue through the static
`jira-issue-lookup` connection. Configure OAuth and `cloudId` in Dex Web
Connections as described in the [connector README](../../../README.md). The
Worker loads the project-managed connection; no token enters Flow input or
persistence.

From `connectors/atlassian/jira/python`, install the package and start the
Worker with the owner-only connection configuration written by Dex Web:

```sh
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40580 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.issue_lookup.main
```

Generate strict FDG 2.0 output from this checked-in Flow before starting Dex
Web:

```sh
dexcli visualize ./examples/issue_lookup/flow.py --schema-version 2.0 --json --out ./build/issue-lookup
```

Start `dexcli dev -dex-port 40580 -web-port 40581 -flow-rendering-dir ./build/issue-lookup -open=false`,
then submit the included input with a unique Flow ID:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40580 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.issue_lookup.start jira-issue-001 examples/issue_lookup/sample-input.json
```

Only `found` is wired. Missing issues and other definite optional outcomes
fail the Flow; transient provider errors use Dex retry. The checked-in
`integration_test.py` exercises a real Dex Worker and Client with a local Jira
stand-in on an OS-assigned port.
