# Bitbucket connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket/go@v0.3.0` after this release publishes.

Moved from `github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket`.

Typed Query and Mutation Steps plus a verified repository webhook Trigger for Bitbucket. The module is `github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket/go`. See the [runnable operation Flows](go/examples/resource-read/README.md) and [repository event Flow](go/examples/repository-event/README.md).

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
| `CreatePullRequest` | `CreatePullRequestInput` | `PullRequest` | `created` |
| `MergePullRequest` | `PullRequestActionInput` | `PullRequest` | `merged` |
| `DeclinePullRequest` | `PullRequestActionInput` | `PullRequest` | `declined` |
| `ListPullRequestComments` | `PullRequestActionInput` | `PullRequestCommentPage` | `listed` |
| `GetPullRequestComment` | `PullRequestCommentIDInput` | `PullRequestComment` | `found` |
| `CreatePullRequestComment` | `CreatePullRequestCommentInput` | `PullRequestComment` | `created` |
| `UpdatePullRequestComment` | `UpdatePullRequestCommentInput` | `PullRequestComment` | `updated` |
| `DeletePullRequestComment` | `PullRequestCommentIDInput` | `DeletedPullRequestComment` | `deleted` |
| `ResolvePullRequestComment` | `PullRequestCommentIDInput` | `PullRequestComment` | `resolved` |
| `UnresolvePullRequestComment` | `PullRequestCommentIDInput` | `PullRequestComment` | `unresolved` |
| `ListBranches` | `ListBranchesInput` | `BranchPage` | `listed` |
| `GetBranch` | `BranchRefInput` | `Branch` | `found` |
| `CreateBranch` | `CreateBranchInput` | `Branch` | `created` |
| `DeleteBranch` | `BranchRefInput` | `DeletedBranch` | `deleted` |

Operations expose their applicable optional `notFound`, `providerRejected`, `invalidResponse`, `defect`, and `uncertain` branches. Unwired optional branches fail the Flow. Pull request and comment creates use sync durability and a three-send limited tier; other new operations use async durability and the safe tier. Python and TypeScript implement only the two original list operations; the remaining operations are Go-only.

Go repository writes use a stable `workspace/repository` slug path. `CreateRepository` creates a Git repository with an explicit display name and private/public value; a 409 conflict is read back and accepted only when the existing repository matches. `UpdateRepository` replaces name and visibility. `DeleteRepository` removes the repository and returns its workspace and slug. These operations use the safe repeat tier because the target path or replacement state is absolute. Find both slugs in the Bitbucket workspace and repository URLs; blank slugs are invalid. Creating and updating need **Repositories Write/Admin** access; deleting needs **Repositories Delete**. See [Bitbucket repository API](https://developer.atlassian.com/cloud/bitbucket/rest/api-group-repositories/).

`CreatePullRequest` sends a title, source branch, and destination branch for the same repository. Bitbucket offers no request idempotency key; after an unknown outcome, the connector caps possible sends at three and selects `uncertain` if it cannot confirm creation. A title and branch pair is not a unique readback key, so an existing matching PR cannot safely prove which request created it. `MergePullRequest` and `DeclinePullRequest` are absolute state transitions; a conflict is read back and accepted only when the PR has the requested state. Find branch names under **Repository > Branches** and the numeric PR ID in the pull request URL. See [Bitbucket pull requests API](https://developer.atlassian.com/cloud/bitbucket/rest/api-group-pullrequests/).

Pull request comments support one-page list, single-comment read, Markdown create and replacement, delete, resolve, and reopen. `CreatePullRequestComment` uses the limited three-send tier because duplicate comments are possible. Comment text is not a unique readback key; a previously existing identical comment cannot prove the write succeeded. All other comment writes use the safe tier. Find a comment ID in the comment URL anchor or API response and supply non-blank Markdown in `raw`; blank text is invalid. Resolve and reopen use Bitbucket's `/resolve` action and confirm conflicts with a comment read.

Branches support one-page list, single-branch read, create at an exact 40-character hex commit hash, and delete. Branch names may contain `/`, but omit the `refs/heads/` prefix. `CreateBranch` uses the safe tier because its name is a stable repository-local key; a 409 is read back and accepted only when the existing branch points to the requested commit. Bitbucket Cloud has no branch update endpoint in the [refs API](https://developer.atlassian.com/cloud/bitbucket/rest/api-group-refs/); change the branch in Git instead. Create and delete need Repositories Write access.

## Repository event Trigger

`repositoryEventReceived` accepts signed events from all 29 keys currently published by Bitbucket for workspace webhooks, including repository, pull request, issue, pipeline, and project events. The event includes safe resource identifiers and every branch or tag change in a push; it does not persist the entire webhook body. The Trigger event ID combines Bitbucket's `X-Request-UUID` and `X-Event-Key`; repeated delivery with the same request UUID keeps one ID. Bindings can filter supported event keys; an empty filter accepts all. The application should narrow repositories with its Trigger filter when a connection serves multiple repositories.

Create a webhook under **Repository settings > Webhooks > Add webhook** or **Workspace settings > Webhooks** at [Bitbucket](https://bitbucket.org/), select the desired events, point it at the application's public HTTPS endpoint, and set a secret. Workspace webhooks receive events from all repositories in that workspace. Supply the same secret to the Go client with `WithWebhookSecret`; the [repository event example](go/examples/repository-event/README.md) reads it from `BITBUCKET_WEBHOOK_SECRET` and mounts the endpoint with a durable project inbox. The endpoint verifies the `X-Hub-Signature` SHA-256 HMAC before it records an event and answers retryably when the secret is blank or no Trigger is running. The access token and webhook secret remain separate; existing API connections need no new field.

Workspace access tokens require Bitbucket Premium; grant Repositories > Read/Write/Admin/Delete for repository actions and Pull requests > Read/Write for PR actions. [Atlassian explains token creation](https://support.atlassian.com/bitbucket-cloud/docs/create-a-workspace-access-token/) and [workspace token permissions](https://support.atlassian.com/bitbucket-cloud/docs/workspace-access-token-permissions/).

## Authentication and configuration

Follow the authorization guide in `connector.yaml` from [https://support.atlassian.com/bitbucket-cloud/docs/create-a-workspace-access-token/](https://support.atlassian.com/bitbucket-cloud/docs/create-a-workspace-access-token/). The connection resolves credentials for each operation or webhook delivery. Secrets travel in HTTP headers or HMAC verification and never enter results, receipts, failures, or URLs. The `baseUrl` field uses the provider API URL, `timeout` bounds one HTTP request, and `maxResponseBytes` bounds JSON response and webhook body size. The last two fields use a positive Go duration and a positive byte count, respectively; blank uses the displayed defaults.

## Failure handling

| Provider response | Result |
| --- | --- |
| HTTP 200 or 201 with valid JSON, or 204 for deletion | Happy branch |
| HTTP 404 | `notFound` |
| HTTP 401, 403, or other definite 4xx (except 408/429) | `providerRejected` |
| HTTP 408, 429, or 5xx; transport interruption | Retry |
| Malformed or oversized JSON after a safe operation | `invalidResponse` |
| Invalid local input or unavailable credentials | `defect` |

Provider error text is never copied into a Failure. Responses are bounded and redirects are not followed. The title update is an absolute replacement, so repeating it does not add another effect and has no `uncertain` branch.
After a successful but unusable `CreatePullRequest` response, the outcome may be unknown, so the connector retries within its send cap and eventually selects `uncertain`.

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
