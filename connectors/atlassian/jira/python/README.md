# Atlassian Jira Python connector

This package adds Jira Cloud search, issue lookup, creation, transitions, and
comments to Python Dex Flows. It uses the connector manifest's OAuth 2.0
refresh policy and requires `dex-connectors-sdk==0.5.0`.

Install it with `pip install dex-connector-atlassian-jira==0.1.0`. Configure
the static connection in Dex Web using the [Atlassian setup guide](../README.md#atlassian-setup).
The project connection loader reads `cloudId`, `endpoint`, and
`maxResponseBytes` from the manifest configuration and keeps OAuth credentials
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
another attempt. No Trigger is included.

The runnable [issue lookup Flow](examples/issue_lookup/README.md) shows a
static `jira-issue-lookup` connection, the `getIssue` Step, and FDG 2.0
generation. Its integration test runs a real Dex Worker and Client with a
local provider stand-in. The checked-in conformance scenarios replay Go
provider behavior for all five operations. Live Jira authorization and API
behavior have not been verified for this Python package.
