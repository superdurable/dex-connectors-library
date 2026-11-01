# GitLab connector for Python

Install with `pip install dex-connector-gitlab==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest operations `getProject` and `listIssues` (Queries) and
`updateIssueLabels` (Mutation) with the same branches, defaults, and failure
classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `getProject` | `new_get_project_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `listIssues` | `new_list_issues_step` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `updateIssueLabels` | `new_update_issue_labels_step` | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

A project is a numeric ID or a namespace path such as `meridian/web`, which is
path-escaped. `listIssues` reads GitLab's default first page of issues.
`updateIssueLabels` replaces the issue's complete label set with a form-encoded
`labels` value (an empty set clears every label), so a retry writes the same
absolute state; labels must be nonblank and cannot contain commas.

All operations retry on 408, 409, 429, 5xx, and transport failures. HTTP 404
selects `notFound`; other statuses select `providerRejected` with
authentication (401), authorization (403), or validation (400, 422) kinds.

## Authorization

Create a Dex Web connection named `gitlab` as described in the
[connector README](../README.md#authorization). The access token travels only
in the `PRIVATE-TOKEN` header and is resolved for every attempt.

## Example

[`examples/gitlab_lookup`](examples/gitlab_lookup/flow.py) reads one project.
Run the Worker with `uv run python -m examples.gitlab_lookup.worker` after
`dexcli dev` has loaded a project containing the `gitlab` connection.

## Tests

`gitlabmock.Mock` scripts every operation and defaults to the manifest
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
