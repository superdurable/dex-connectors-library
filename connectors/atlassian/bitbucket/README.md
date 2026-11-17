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

Each operation has optional `notFound`, `providerRejected`, `invalidResponse`, and `defect` branches. Unwired optional branches fail the Flow. All operations use async Execute durability and the default one-hour retry budget. Reads and absolute title replacement are safe to repeat. Only Go implements the three new operations.

Workspace access tokens require Bitbucket Premium; grant Repositories > Read and Pull requests > Read, plus Pull requests > Write for title updates. [Atlassian explains token creation](https://support.atlassian.com/bitbucket-cloud/docs/create-a-workspace-access-token/) and [workspace token permissions](https://support.atlassian.com/bitbucket-cloud/docs/workspace-access-token-permissions/).

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
