# GitLab connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/gitlab/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/gitlab`.

The `gitlab` Go module exposes typed Dex Steps for GitLab. Its [runnable lookup Flow](go/examples/gitlab-lookup/README.md) uses the static connection name `gitlab`.

## Operations

| Factory | Endpoint | Input | Output | Required branch | Optional branches |
| --- | --- | --- | --- | --- | --- |
| NewGetProjectStep | GET /api/v4/projects/{id} | numeric or namespace `ProjectID` | `Project` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect |
| NewListIssuesStep | GET /api/v4/projects/{id}/issues | `ProjectID` | `IssuesPage.Issues` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect |
| NewUpdateIssueLabelsStep | PUT /api/v4/projects/{id}/issues/{issue_iid} | `ProjectID`, positive `IssueIID`, complete `Labels` list | updated `Issue` | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect |
| NewCreateIssueStep | POST /api/v4/projects/{id}/issues | project, title, optional description | `Issue` | `created` | `providerRejected`, `defect`, `uncertain` |
| NewCreateIssueCommentStep | POST /api/v4/projects/{id}/issues/{iid}/notes | project, IID, body | `IssueComment` | `created` | `providerRejected`, `defect`, `uncertain` |
| NewGetIssueStep | GET /api/v4/projects/{id}/issues/{iid} | project, IID | `Issue` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewEditIssueStep | PUT /api/v4/projects/{id}/issues/{iid} | project, IID, selected absolute fields | `Issue` | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewLockIssueStep | PUT /api/v4/projects/{id}/issues/{iid} | project, IID | `Issue` | `locked` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewDeleteIssueStep | DELETE /api/v4/projects/{id}/issues/{iid} | project, IID | `DeleteIssueOutput` | `deleted` | `notFound`, `providerRejected`, `defect` |
| NewCreateReleaseStep | POST /api/v4/projects/{id}/releases | project, tag, optional title, notes, ref | `Release` | `created` | `providerRejected`, `defect`, `uncertain` |
| NewGetReleaseStep | GET /api/v4/projects/{id}/releases/{tag} | project, tag | `Release` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewListReleasesStep | GET /api/v4/projects/{id}/releases | project, page, page size | `ReleasesPage` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewUpdateReleaseStep | PUT /api/v4/projects/{id}/releases/{tag} | project, tag, selected absolute fields | `Release` | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewDeleteReleaseStep | DELETE /api/v4/projects/{id}/releases/{tag} | project, tag | `DeleteReleaseOutput` | `deleted` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewListRepositoryTreeStep | GET /api/v4/projects/{id}/repository/tree | project, path, ref, page | `RepositoryTreePage` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewListUserProjectsStep | GET /api/v4/users/{user}/projects | user, page, page size | `UserProjectsPage` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`updateIssueLabels` sends the `labels` form field as a comma-separated absolute replacement. An empty list sends an empty field and clears all labels. Repeating the same set is safe; label names containing commas are rejected locally. The list operation returns GitLab’s first default page of 20 issues.

The Python package is [`dex-connector-gitlab`](python/README.md), imported as
`dex_connectors.gitlab`.

## Authorization

Create a personal access token in [User Settings > Access Tokens](https://gitlab.com/-/user_settings/personal_access_tokens). Use `read_api` for read-only operations or `api` for issue and release writes. The connector sends it in the `PRIVATE-TOKEN` header. Set `baseUrl` to a self-managed instance’s HTTPS `/api/v4` URL when needed.

`baseUrl`, `timeout`, and `maxResponseBytes` have manifest defaults. The connection form gives the provider URL, setup path, units, and blank behavior for every field. Secrets are resolved for each call and are never copied into Flow input, Results, Failures, receipts, or logs.

## Failure behavior

| Response | Outcome |
| --- | --- |
| HTTP 200 with a valid provider result | Required success branch |
| HTTP 404 for a named resource | `notFound` when declared, otherwise `providerRejected` |
| Other conclusive 4xx or redirects | `providerRejected` |
| HTTP 429, 503, or transport failure | Retry under the default one-hour policy |
| HTTP 500 or 408 | Retry; limited creates stop after three possible sends and select `uncertain` when read-back cannot confirm |
| Malformed or oversized response | `invalidResponse` |
| Invalid local input or credentials | `defect` before sending |

HTTP redirects are not followed. Provider error bodies are never copied into failures. Unwired optional branches fail the Flow.

## Mocks

The generated `gitlabmock` package installs these default answers:

- `getProject`: Meridian Web project 42.
- `listIssues`: issue 7 titled Review release notes.
- `updateIssueLabels`: issue 7 with `review` and `ready` labels.

The [mock-backed example test](go/examples/gitlab-lookup/flow/workflow_mock_integration_test.go) runs the first default through a real Dex Worker and Client in CI.

Issue and release creates use bounded `limited` dispatch with at most three possible sends. Release creation reads back by unique tag before resending. Issue and comment creation read back a provider ID when one was recorded. Queries and absolute updates/deletes use `safe` repeat behavior.

## Official API references

- [https://docs.gitlab.com/api/projects/](https://docs.gitlab.com/api/projects/)
- [https://docs.gitlab.com/api/issues/](https://docs.gitlab.com/api/issues/)
- [https://docs.gitlab.com/api/notes/](https://docs.gitlab.com/api/notes/)
- [https://docs.gitlab.com/api/releases/](https://docs.gitlab.com/api/releases/)
- [https://docs.gitlab.com/api/repositories/](https://docs.gitlab.com/api/repositories/)
- [https://docs.gitlab.com/api/rest/authentication/](https://docs.gitlab.com/api/rest/authentication/)
- [https://docs.gitlab.com/user/profile/personal_access_tokens/](https://docs.gitlab.com/user/profile/personal_access_tokens/)

## Verification limits

The provider calls have not been exercised with live account credentials. Real Dex execution, release compatibility, and provider authorization remain unverified locally. Unlisted response fields and undocumented status details are not asserted.

## TypeScript

The [`typescript/`](typescript/) package provides `getProject`, `listIssues`, and `updateIssueLabels` at `@superdurable/connector-gitlab` version `0.1.0`. Its [runnable project lookup](typescript/examples/get-project/README.md) binds the static `gitlab` connection and can be verified through `dexcli dev`. Authorization uses a GitLab personal access token with `read_api` for queries or `api` for label updates.
