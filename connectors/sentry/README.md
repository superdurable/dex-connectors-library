# Sentry connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/sentry/go@v0.2.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/sentry`.

This Dex connector calls the public Sentry JSON API. Its
manifest version is `v0.1.0`; the Go module requires connector SDK `v0.33.0`
    and Dex Go SDK `v1.5.0` or newer. The runnable
[`issue-review` example](go/examples/issue-review/README.md) shows a
complete Worker and the generated operation-specific Step factory.

## Operations

| Operation | Kind | Output |
| --- | --- | --- |
| `listIssues` | query | IssuePage |
| `getIssue` | query | Issue |
| `updateIssueStatus` | mutation | Issue |

`listIssues` returns the first provider page (default unresolved query); cursor pagination is not exposed in this initial release. `updateIssueStatus` sends an absolute status in a PUT request. Repeating `resolved`, `unresolved`, or `ignored` sets the same state. Only the submitted status field is changed.

## Authorization

Start at [https://sentry.io/settings/account/api/auth-tokens/](https://sentry.io/settings/account/api/auth-tokens/) and follow
**User Settings > Auth Tokens > Create New Token**. Create an auth token with event:read for reads and event:write for status updates. Copy the opaque secret; blank prevents requests. Paste the token in the
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

The HTTP unit tests verify request shapes and local classification against
an `httptest` server. Provider authorization, live API response variations,
and real Dex integration remain unverified locally. CI runs the mock backed
real Dex test; a user-controlled live connection is still needed to confirm
the provider account and permissions.
