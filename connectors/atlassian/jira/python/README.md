# Atlassian Jira Python connector

This package adds Jira Cloud search, issue lookup, creation, transitions, and
comments to Python Dex Flows, and from 0.2.0 the `issueChanged` poll Trigger,
which starts a Flow when an issue is created or updated. It uses the connector
manifest's OAuth 2.0 refresh policy and requires `dex-connectors-sdk>=0.12.0`,
whose poll Trigger kit the Trigger is built on.

Install it with `pip install dex-connector-atlassian-jira==0.2.1`. Configure
the static connection in Dex Web using the [Atlassian setup guide](../README.md#atlassian-setup).
The project connection loader reads `cloudId`, `endpoint`,
`maxResponseBytes`, and `pollInterval` (at least 5 seconds) from the manifest
configuration and keeps OAuth credentials
in project storage.

| Operation | Kind | Happy branch |
| --- | --- | --- |
| `searchIssues` | Query | `searched` |
| `getIssue` | Query | `found` |
| `createIssue` | Mutation | `created` |
| `transitionIssue` | Mutation | `transitioned` |
| `addComment` | Mutation | `added` |

Other definite outcomes select the optional branches declared in
`connector.yaml`. Safe transient failures retry through Dex. Create and
comment operations fence dispatch and reconcile ambiguous outcomes before
another attempt.

The runnable [issue lookup Flow](examples/issue_lookup/README.md) shows a
static `jira-issue-lookup` connection, the `getIssue` Step, and FDG 2.0
generation. Its integration test runs a real Dex Worker and Client with a
local provider stand-in. The checked-in conformance scenarios replay Go
provider behavior for all five operations. Live Jira authorization and API
behavior have not been verified for this Python package.

## issueChanged Trigger

`issueChanged` polls Jira's enhanced search on the `updated` time at parity with
the Go connector, on `dex_connectors.sdk.pollingtrigger`; the
[connector README](../README.md#issuechanged-trigger) describes the query, the
two-minute lookback, paging, change kinds, resets, and rate limits. The cursor
documents match the Go connector's byte for byte, which the Go-generated
fixtures in `tests/fixtures/go-issue-change-cases.json` check, so a Go and a
Python replica can share one binding's cursor and inbox.

- `new_project_issue_trigger_runner(project, connection_name, routes)` opens
  the connection and each route's saved binding and returns an
  `IssueTriggerRunner`. Every change is recorded in the binding's project
  Trigger inbox before its project poll cursor commits, so a restarted Worker
  neither misses nor repeats a change. An unsaved binding watches every project
  the account can browse. Await `run(context)` for the life of the Worker and
  `aclose()` afterwards; `wake_poller(binding_name)` polls now.
- `new_issue_trigger_runner(IssueTriggerRunnerConfig(...))` takes targets and
  cursor stores directly and returns the kit's `PollRunner`; a route without a
  cursor store keeps it in memory and records a new baseline on every start, as
  `new_issue_changed_trigger` always does.
- Each `IssueChangedEvent` carries the site, the derived `change_kind`
  (`ISSUE_CHANGE_CREATED` or `ISSUE_CHANGE_UPDATED`), and the issue's standard
  fields. The event ID hashes the site, issue ID, and `updated` instant, so
  `new_dex_flow_trigger_target` starts one Flow per issue version.
- `define_issue_changed_trigger_binding` declares a static binding from
  `get_connector_trigger_bindings`. Dex CLI v1.47.3 reads the binding but not a
  `ConnectorConfigurationUI` object passed as `configuration_ui`, so
  `dexcli visualize` warns `connector_configuration_ui` and Dex Web shows the
  binding without the project picker; the Go example's binding shows it.

The runnable [issue-changed Flow](examples/issue_changed/README.md) starts one
Flow per change and reads the issue back. Its integration tests run a real Dex
Worker and Client against a local Jira search stand-in: the baseline starts no
Flow, each issue version starts one Flow across a Trigger restart, a 429 with
`Retry-After` pauses polling without losing a change, and a changed project
records a new baseline. Live Jira search has not been verified.
