# Bitbucket connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket/go@v0.3.0` after this release publishes.

Moved from `github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket`.

Typed Query and Mutation Steps for Bitbucket. The module is `github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket/go`. See the [runnable resource-read Flow](go/examples/resource-read/README.md), which also registers a pull request title update Flow.

The Python package is [`dex-connector-atlassian-bitbucket`](python/README.md), imported as
`dex_connectors.atlassian_bitbucket`.

The TypeScript package is [`@superdurable/connector-atlassian-bitbucket`](typescript/), with a [runnable Dex Flow](typescript/examples/resource-read/README.md). It implements both read operations through typed Query Steps.

## Operations

| Factory | Input | Output | Happy branch |
| --- | --- | --- | --- |
| `ListRepositories` | `ListRepositoriesInput` | `RepositoryPage` | `listed` |
| `ListPullRequests` | `ListPullRequestsInput` | `PullRequestPage` | `listed` |
| `GetRepository` | `GetRepositoryInput` | `Repository` | `found` |
| `GetPullRequest` | `GetPullRequestInput` | `PullRequest` | `found` |
| `UpdatePullRequestTitle` | `UpdatePullRequestTitleInput` | `PullRequest` | `updated` |
| `CreateRepository` | `CreateRepositoryInput` | `Repository` | `created` |
| `UpdateRepository` | `UpdateRepositoryInput` | `Repository` | `updated` |
| `DeleteRepository` | `DeleteRepositoryInput` | `DeletedRepository` | `deleted` |

Each operation has optional `notFound`, `providerRejected`, `invalidResponse`, and `defect` branches. Unwired optional branches fail the Flow. All operations use async Execute durability and the default one-hour retry budget. Reads and absolute replacements are safe to repeat. Python and TypeScript implement only the two list operations; the remaining operations are Go-only.

Go repository writes use a stable `workspace/repository` slug path. `CreateRepository` creates a Git repository with an explicit display name and private/public value; a 409 conflict is read back and accepted only when the existing repository matches. `UpdateRepository` replaces name and visibility. `DeleteRepository` removes the repository and returns its workspace and slug. These operations use the safe repeat tier because the target path or replacement state is absolute. Find both slugs in the Bitbucket workspace and repository URLs; blank slugs are invalid. Creating and updating need **Repositories Write/Admin** access; deleting needs **Repositories Delete**. See [Bitbucket repository API](https://developer.atlassian.com/cloud/bitbucket/rest/api-group-repositories/).

Workspace access tokens require Bitbucket Premium; grant Repositories > Read/Write/Admin/Delete for repository actions and Pull requests > Read/Write for PR actions. [Atlassian explains token creation](https://support.atlassian.com/bitbucket-cloud/docs/create-a-workspace-access-token/) and [workspace token permissions](https://support.atlassian.com/bitbucket-cloud/docs/workspace-access-token-permissions/).

## Authentication and configuration

Follow the authorization guide in `connector.yaml` from [https://support.atlassian.com/bitbucket-cloud/docs/create-a-workspace-access-token/](https://support.atlassian.com/bitbucket-cloud/docs/create-a-workspace-access-token/). The connection resolves credentials for each operation. Secrets travel in HTTP headers and never enter results, receipts, failures, or URLs. The `baseUrl` field uses the provider API URL, `timeout` bounds one HTTP request, and `maxResponseBytes` bounds JSON response size. The last two fields use a positive Go duration and a positive byte count, respectively; blank uses the displayed defaults.

## Failure handling

| Provider response | Result |
| --- | --- |
| HTTP 200 with valid JSON | Happy branch |
| HTTP 404 | `notFound` |
| HTTP 401, 403, or other definite 4xx (except 408/429) | `providerRejected` |
| HTTP 408, 429, or 5xx; transport interruption | Retry |
| Malformed or oversized JSON | `invalidResponse` |
| Invalid local input or unavailable credentials | `defect` |

Provider error text is never copied into a Failure. Responses are bounded and redirects are not followed. The title update is an absolute replacement, so repeating it does not add another effect and has no `uncertain` branch.

## Mocks

The generated `bitbucketmock` package returns one realistic default per operation from `connector.yaml`. The [mock-backed real Dex test](go/examples/resource-read/flow/workflow_mock_integration_test.go) executes the example Flow through a Worker and checks its mapped input and branch.

```go
mock := bitbucketmock.New(t, resourceread.ConnectionName)
flow := resourceread.NewFlow(mock.Connection())
```

## Provider documentation

- [https://developer.atlassian.com/cloud/bitbucket/rest/api-group-repositories/](https://developer.atlassian.com/cloud/bitbucket/rest/api-group-repositories/)
- [https://developer.atlassian.com/cloud/bitbucket/rest/api-group-pullrequests/](https://developer.atlassian.com/cloud/bitbucket/rest/api-group-pullrequests/)
- [https://developer.atlassian.com/cloud/bitbucket/rest/](https://developer.atlassian.com/cloud/bitbucket/rest/)

## Unverified live behavior

No real Bitbucket account or Dex Web connection was available in this dispatch. Live credential creation, access scope selection, store configuration, provider data shapes beyond the documented fields, and response behavior remain unverified. GitHub Actions runs real Dex integration coverage; a user-controlled connection is needed for a live provider check.
