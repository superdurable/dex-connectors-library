# Sentry connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/sentry/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/sentry`.

This Dex connector calls the public Sentry JSON API. Its
manifest version is `v0.3.0`; the Go module requires connector SDK `v0.38.0`
    and Dex Go SDK `v1.5.0` or newer. The runnable
[`issue-review` example](go/examples/issue-review/README.md) shows a
complete Worker and the generated operation-specific Step factory.
The [`assign-issue` example](go/examples/assign-issue/README.md) exercises
an absolute issue assignment through a real Dex Worker in its mock test.
The [`create-project` example](go/examples/create-project/README.md) exercises
bounded project creation through a real Dex Worker in its mock test.
The [`create-release` example](go/examples/create-release/README.md) exercises
bounded release creation through a real Dex Worker in its mock test.
The [`create-team` example](go/examples/create-team/README.md) exercises
bounded team creation through a real Dex Worker in its mock test.

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
| `createProject` | mutation | Project |
| `createRelease` | mutation | Release |
| `getRelease` | query | Release |
| `listReleases` | query | ReleasePage |
| `updateRelease` | mutation | Release |
| `deleteRelease` | mutation | DeletedRelease |
| `createTeam` | mutation | Team |
| `getTeam` | query | Team |
| `listTeams` | query | TeamPage |
| `updateTeam` | mutation | Team |
| `deleteTeam` | mutation | DeletedTeam |
| `createOrganization` | mutation | Organization |
| `getOrganization` | query | Organization |
| `updateOrganization` | mutation | Organization |
| `updateIssue` | mutation | Issue |
| `updateProject` | mutation | Project |
| `listProjectEvents` | query | IssueEventPage |
| `getProjectEvent` | query | IssueEvent |

The Go queries use the `safe` repeat tier. List operations return one
provider page; pass a Sentry cursor to request a known later page. Sentry
returns continuation cursors in its `Link` header, which these typed outputs
do not yet expose. Python and TypeScript retain their declared issue operations.

The new Go writes use the `safe` repeat tier. Assignment and renaming set
absolute fields; issue and project deletion queue a terminal action. Find issue
IDs under **Issues** and project slugs under **Projects** in Sentry. `assignedTo`
accepts a user ID, primary email, or `team:<team ID>`; blank identifiers and
names are invalid. Deletion can remain pending after an accepted response.
`createProject` uses the `limited` tier with at most three sends. It requires
an explicit project slug, reads it back after an unknown outcome, and selects
optional `uncertain` when the send cap is reached without confirmation. The
organization and team slugs come from Sentry **Settings > Teams**; the name
and project slug are nonempty Flow inputs. Some organizations also require
`org:write` or Team Admin permission for creation.

Releases are identified by version within an organization. `createRelease`
uses the `limited` tier, a three-send cap, and version read-back after an
unknown outcome. `getRelease` and `listReleases` are safe queries.
`updateRelease` sets an absolute `open` or `archived` status and optional
reference and URL; `deleteRelease` permanently removes that version and its
files. Both writes use the `safe` repeat tier. A token needs
`project:releases` or the endpoint-specific project/CI scopes in
[Sentry's release API](https://docs.sentry.io/api/releases/).

`createTeam` requires an explicit slug and uses bounded `limited` resend with
slug and name read-back. `getTeam` and `listTeams` are safe queries.
`updateTeam` replaces its slug and name; `deleteTeam` schedules deletion.
Both writes use the `safe` repeat tier. Tokens need `org:read` for listing,
`team:read` for detail, `team:write` or `org:write` for creation,
`team:write` for updates, and `team:admin` for deletion.

`createOrganization` requires a user token whose account may create Sentry
organizations, an explicit globally unique slug, and `agreeTerms: true` after
the user accepts Sentry's terms and privacy policy. It uses `limited` resend
with three possible sends and slug/name read-back after an unknown result.
Sentry marks this creation endpoint private and user-context-only, so live
availability depends on the account. `getOrganization` is a safe query;
`updateOrganization` replaces its slug and name using the safe tier and needs
`org:write` or `org:admin`. See [organization settings](https://sentry.io/settings/)
for the current slug and name, and [Sentry's organization API](https://docs.sentry.io/api/organizations/)
for read and update scopes.

`updateIssue` accepts any subset of absolute status, assignee, seen,
bookmark, public, and subscription fields. An omitted pointer leaves that
field untouched; `false` is a real value. Status can be `resolved`,
`unresolved`, `ignored`, or `resolvedInNextRelease`. `updateProject` similarly
accepts name, slug, owning team, platform, bookmark, and digest delay fields;
delays are nonnegative seconds. Both use safe repeat, since repeating an
absolute value has the same effect. See [issue update](https://docs.sentry.io/api/events/update-an-issue/)
and [project update](https://docs.sentry.io/api/projects/update-a-project/)
for the fields and token scopes. `listProjectEvents` and `getProjectEvent`
cover project scoped event APIs; the latter requires the event's 32-character
hexadecimal ID. They return selected metadata rather than raw event payloads,
which could contain credentials. See [project event list](https://docs.sentry.io/api/events/list-a-projects-error-events/)
and [event detail](https://docs.sentry.io/api/events/retrieve-an-event-for-a-project/).

`listIssues` returns the first provider page (default unresolved query); cursor pagination is not exposed in this initial release. `updateIssueStatus` sends an absolute status in a PUT request. Repeating `resolved`, `unresolved`, or `ignored` sets the same state. Only the submitted status field is changed.

## Authorization

Start at [https://sentry.io/settings/account/api/auth-tokens/](https://sentry.io/settings/account/api/auth-tokens/) and follow
**User Settings > Auth Tokens > Create New Token**. Create an auth token with
`event:read` and `event:write` for issues, `event:admin` for issue deletion,
`project:read`, `project:write`, and `project:admin` for project operations,
`org:read` for organization reads, and `org:write` for organization updates.
Organization creation also needs user-context permission. Copy
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
- [https://docs.sentry.io/api/projects/create-a-new-project/](https://docs.sentry.io/api/projects/create-a-new-project/)
- [https://docs.sentry.io/api/releases/](https://docs.sentry.io/api/releases/)
- [https://docs.sentry.io/api/teams/](https://docs.sentry.io/api/teams/)

The HTTP unit tests verify request shapes and local classification against
an `httptest` server. The TypeScript example runs against a local HTTP fixture
through a real Dex Worker. Provider authorization, live API response variations,
and Dex Web Connections rendering remain unverified. CI also runs the Go mock
backed real Dex test; a user-controlled live connection is needed to confirm
the provider account and permissions.
