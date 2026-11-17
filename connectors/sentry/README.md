# Sentry connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/sentry/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/sentry`.

This Dex connector calls the public Sentry JSON API. Its
manifest version is `v0.3.0`; the Go module requires connector SDK `v0.33.0`
    and Dex Go SDK `v1.5.0` or newer. The runnable
[`issue-review` example](go/examples/issue-review/README.md) shows a
complete Worker and the generated operation-specific Step factory.
The [`assign-issue` example](go/examples/assign-issue/README.md) exercises
an absolute issue assignment through a real Dex Worker in its mock test.

The Python package is [`dex-connector-sentry`](python/README.md), imported as
`dex_connectors.sentry`. The TypeScript package is
[`@superdurable/connector-sentry`](typescript/README.md).

## Operations

| Operation | Kind | Output |
| --- | --- | --- |
| `listIssues` | query | IssuePage |
| `getIssue` | query | Issue |
| `updateIssueStatus` | mutation | Issue |
| `listIssueEvents` | query | IssueEventPage |
| `getIssueEvent` | query | IssueEvent |
| `listProjects` | query | ProjectPage |
| `getProject` | query | Project |
| `listOrganizations` | query | OrganizationPage |
| `assignIssue` | mutation | Issue |
| `deleteIssue` | mutation | DeletedIssue |
| `renameProject` | mutation | Project |
| `deleteProject` | mutation | DeletedProject |

The five new Go queries use the `safe` repeat tier. List operations return one
provider page; pass a Sentry cursor to request a known later page. Sentry
returns continuation cursors in its `Link` header, which these typed outputs
do not yet expose. Python and TypeScript retain their declared issue operations.

The new Go writes use the `safe` repeat tier. Assignment and renaming set
absolute fields; issue and project deletion queue a terminal action. Find issue
IDs under **Issues** and project slugs under **Projects** in Sentry. `assignedTo`
accepts a user ID, primary email, or `team:<team ID>`; blank identifiers and
names are invalid. Deletion can remain pending after an accepted response.

`listIssues` returns the first provider page (default unresolved query); cursor pagination is not exposed in this initial release. `updateIssueStatus` sends an absolute status in a PUT request. Repeating `resolved`, `unresolved`, or `ignored` sets the same state. Only the submitted status field is changed.

## Authorization

Start at [https://sentry.io/settings/account/api/auth-tokens/](https://sentry.io/settings/account/api/auth-tokens/) and follow
**User Settings > Auth Tokens > Create New Token**. Create an auth token with
`event:read` and `event:write` for issues, `event:admin` for issue deletion,
`project:read`, `project:write`, and `project:admin` for project operations,
and `org:read` for organization reads. Copy
the opaque secret; blank prevents requests. Paste the token in the
secret `api_key` field in Dex Web Connections.
The key travels only in `Authorization`. Leave `baseUrl`, `timeout`,
and `maxResponseBytes` blank for the displayed public endpoint and safety
defaults; override the base URL only for a compatible gateway or test server.

## Failures and retries

| Condition | Result |
| --- | --- |
| Successful documented response | operation's happy branch |
| Known missing resource on a detail operation | `notFound` |
| Other definite 4xx rejection | `providerRejected` |
| Malformed or oversized successful JSON | `invalidResponse` |
| Invalid local input or credentials | `defect` |
| HTTP 408, 429, 5xx, transport interruption | Retry until the default budget ends, then Flow failure |

Every non-happy branch is optional. The example wires only the happy branch;
an unwired optional outcome fails the Flow. Provider error response text and
credentials never enter a Dex result, receipt, Stream, or log.

## Mocks

`sentrymock.New(t, "mock")` binds the default manifest answers and the
generated `zz_generated_mock_test.go` strictly decodes every fixture. The
checked-in [`workflow_mock_integration_test.go`](go/examples/issue-review/flow/workflow_mock_integration_test.go)
uses the same generated mock against a real Dex Worker in CI. Its start looks
like this runnable source:

```go
mock := sentrymock.New(t, ConnectionName)
flow := NewFlow(mock.Connection())
```

## Provider documentation and verification

- [https://docs.sentry.io/api/events/list-an-organizations-issues/](https://docs.sentry.io/api/events/list-an-organizations-issues/)
- [https://docs.sentry.io/api/events/retrieve-an-issue/](https://docs.sentry.io/api/events/retrieve-an-issue/)
- [https://docs.sentry.io/api/events/update-an-issue/](https://docs.sentry.io/api/events/update-an-issue/)
- [https://docs.sentry.io/api/events/list-an-issues-events/](https://docs.sentry.io/api/events/list-an-issues-events/)
- [https://docs.sentry.io/api/organizations/list-an-organizations-projects/](https://docs.sentry.io/api/organizations/list-an-organizations-projects/)
- [https://docs.sentry.io/api/users/list-your-organizations/](https://docs.sentry.io/api/users/list-your-organizations/)
- [https://docs.sentry.io/api/events/remove-an-issue/](https://docs.sentry.io/api/events/remove-an-issue/)
- [https://docs.sentry.io/api/projects/update-a-project/](https://docs.sentry.io/api/projects/update-a-project/)
- [https://docs.sentry.io/api/projects/delete-a-project/](https://docs.sentry.io/api/projects/delete-a-project/)

The HTTP unit tests verify request shapes and local classification against
an `httptest` server. The TypeScript example runs against a local HTTP fixture
through a real Dex Worker. Provider authorization, live API response variations,
and Dex Web Connections rendering remain unverified. CI also runs the Go mock
backed real Dex test; a user-controlled live connection is needed to confirm
the provider account and permissions.
