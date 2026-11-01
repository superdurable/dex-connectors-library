# Sentry connector for Python

Install with `pip install dex-connector-sentry==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest operations `listIssues` and `getIssue` (Queries) and
`updateIssueStatus` (Mutation) with the same branches, defaults, and failure
classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listIssues` | `new_list_issues_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `getIssue` | `new_get_issue_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `updateIssueStatus` | `new_update_issue_status_step` | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

Every operation needs the organization slug. `listIssues` passes an optional
Sentry search `query` and returns the first result page. `updateIssueStatus`
sets `resolved`, `unresolved`, or `ignored`; the absolute replacement makes a
repeated attempt safe. 408, 429, 5xx, and transport failures retry; other
non-success responses select `providerRejected`, and HTTP 404 selects
`notFound` where the operation declares it.

## Authorization

Create a Dex Web connection named `sentry` as described in the
[connector README](../README.md#authorization). The auth token travels only in
the `Authorization: Bearer` header and is resolved for every attempt.

## Example

[`examples/issue_review`](examples/issue_review/flow.py) checks the
organization slug, lists issues with `new_list_issues_step`, and records the
page. Run the Worker with `uv run python -m examples.issue_review.worker` after
`dexcli dev` has loaded a project containing the `sentry` connection.

## Tests

`sentrymock.Mock` scripts all three operations and defaults to the manifest
mock answers. Run `uv run pytest tests` for provider parity and manifest
contract checks.
