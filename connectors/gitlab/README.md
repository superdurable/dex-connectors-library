# GitLab connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/gitlab/go@v0.2.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/gitlab`.

The `gitlab` Go module exposes typed Dex Steps for GitLab. Its [runnable lookup Flow](go/examples/gitlab-lookup/README.md) uses the static connection name `gitlab`.

## Operations

| Factory | Endpoint | Input | Output | Required branch | Optional branches |
| --- | --- | --- | --- | --- | --- |
| NewGetProjectStep | GET /api/v4/projects/{id} | numeric or namespace `ProjectID` | `Project` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect |
| NewListIssuesStep | GET /api/v4/projects/{id}/issues | `ProjectID` | `IssuesPage.Issues` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect |
| NewUpdateIssueLabelsStep | PUT /api/v4/projects/{id}/issues/{issue_iid} | `ProjectID`, positive `IssueIID`, complete `Labels` list | updated `Issue` | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect |

`updateIssueLabels` sends the `labels` form field as a comma-separated absolute replacement. An empty list sends an empty field and clears all labels. Repeating the same set is safe; label names containing commas are rejected locally. The list operation returns GitLab’s first default page of 20 issues.

## Authorization

Create a personal access token in [User Settings > Access Tokens](https://gitlab.com/-/user_settings/personal_access_tokens). Use `read_api` for read-only operations or `api` for `updateIssueLabels`. The connector sends it in the `PRIVATE-TOKEN` header. Set `baseUrl` to a self-managed instance’s HTTPS `/api/v4` URL when needed.

`baseUrl`, `timeout`, and `maxResponseBytes` have manifest defaults. The connection form gives the provider URL, setup path, units, and blank behavior for every field. Secrets are resolved for each call and are never copied into Flow input, Results, Failures, receipts, or logs.

## Failure behavior

| Response | Outcome |
| --- | --- |
| HTTP 200 with a valid provider result | Required success branch |
| HTTP 404 for a named resource | `notFound` when declared, otherwise `providerRejected` |
| Other conclusive 4xx or redirects | `providerRejected` |
| HTTP 408, 409, 429, 5xx, or transport failure | Retry under the default one-hour policy |
| Malformed or oversized response | `invalidResponse` |
| Invalid local input or credentials | `defect` before sending |

HTTP redirects are not followed. Provider error bodies are never copied into failures. Unwired optional branches fail the Flow.

## Mocks

The generated `gitlabmock` package installs these default answers:

- `getProject`: Meridian Web project 42.
- `listIssues`: issue 7 titled Review release notes.
- `updateIssueLabels`: issue 7 with `review` and `ready` labels.

The [mock-backed example test](go/examples/gitlab-lookup/flow/workflow_mock_integration_test.go) runs the first default through a real Dex Worker and Client in CI.

## Official API references

- [https://docs.gitlab.com/api/projects/](https://docs.gitlab.com/api/projects/)
- [https://docs.gitlab.com/api/issues/](https://docs.gitlab.com/api/issues/)
- [https://docs.gitlab.com/api/rest/authentication/](https://docs.gitlab.com/api/rest/authentication/)
- [https://docs.gitlab.com/user/profile/personal_access_tokens/](https://docs.gitlab.com/user/profile/personal_access_tokens/)

## Verification limits

The provider calls have not been exercised with live account credentials. Real Dex execution, release compatibility, and provider authorization remain unverified locally. Unlisted response fields and undocumented status details are not asserted.
