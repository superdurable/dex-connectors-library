# GitHub Connector

The GitHub connector is an independent Go module for signup profile evidence
and public repository change research. It exposes generated Dex Query Step
factories for:

- `GetAuthenticatedProfile`: authenticated account plus primary verified email;
- `ListPublicRepositories`: recent-first, deduplicated, bounded public
  repositories owned by one login;
- `ListMergedPullRequests`: one page of pull requests merged into a repository
  within a time window;
- `ListPullRequestFiles`: one page of files changed by a pull request;
- `ListReleases`: one page of release IDs, tags, publication dates, bounded notes,
  draft/prerelease flags and a next-page number;
- `ListCommits`: one page of commits within a time window, optionally from one
  ref and path.

The OAuth connection requests `read:user user:email offline_access`.
`offline_access` opts GitHub.com into an eight-hour access token and a rotating
six-month refresh token without expanding repository access. Credentials stay in
project storage and are read before every call. Within five minutes of the
recorded expiry, the connector refreshes them and project storage atomically
persists both rotated tokens. A `bad_refresh_token` response marks the
connection as requiring reauthorization. GitHub Enterprise Server may
ignore `offline_access` and issue a non-expiring access token without refresh
material; the connector keeps using that token while no expiry is present.

The connector never
requests `repo`, `public_repo`, organization, or write scopes. Without `repo`,
GitHub serves only public repository data. Every operation also checks
GitHub's `X-OAuth-Scopes` response header and selects `insufficientScope` for a
grant that differs from those two scopes, including a broader token, so the
queries cannot return private repository data. Repository listing never
returns private repositories, source, README, events, or issues. The change
queries return bounded pull request metadata and bodies, changed-file patches,
and commit messages, never raw GitHub responses, commit email addresses, or
provider error bodies.

GitHub documents [`read:user` and `user:email` as the profile and email OAuth
scopes](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/scopes-for-oauth-apps).
A rate-limited Query retries after the delay that GitHub's [rate-limit
guidance](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api)
gives: `Retry-After` seconds when present, the `X-RateLimit-Reset` time when
`X-RateLimit-Remaining` is `0`, and otherwise one minute. Dex waits for that
delay only while it fits in the Step's remaining retry budget.

The default repository limit is 100 and the hard cap is 500. Provider pages use
at most 100 items. Results report truncation and retain only bounded profile and
repository metadata.

## Repository change queries

`listReleases` calls GitHub's [List releases API](https://docs.github.com/en/rest/releases/releases#list-releases).
It accepts `Owner`, `Repository`, `PageSize` (default 30, maximum 100), `Page`
(default 1), and `MaxBodyCharacters` (default 4000, maximum 16384). The result
contains `Releases` and `NextPage`; zero means no next page. Notes truncated by
the character bound are marked `BodyTruncated`. Overall response bytes remain
bounded by the connection's existing `maxResponseBytes`.

There is no provider-side publication-date filter. Use `PublishedAt`, not
`CreatedAt`, for publication windows, and explicitly select drafts/prereleases
as required by the application. Provider order is not guaranteed to be
publication order: an old item is not evidence that later pages can be omitted.
An application that imposes a page budget must report incomplete coverage.
Regular Git tags with no GitHub release are not included. Authentication,
scope checks, query retries and optional failure branches match the existing
repository queries; credentials never enter the returned result.

The [release page example](examples/repository-releases/README.md)
uses the generated `NewListReleasesStep` factory and retains a typed result.

Each change query makes one GitHub request for one page. Its happy-path branch
is `listed`. `NextPage` comes from GitHub's `Link` header `rel="next"` and is
zero when no page follows. An application reads another page with another Step
execution. Pages are one-based. `PageSize` accepts 1 through 100.

| Operation | GitHub REST API | Default page size |
| --- | --- | --- |
| `listMergedPullRequests` | [`GET /search/issues`](https://docs.github.com/en/rest/search/search#search-issues-and-pull-requests) with `repo:OWNER/REPO is:pr is:merged merged:AFTER..BEFORE` | 30 |
| `listPullRequestFiles` | [`GET /repos/{owner}/{repo}/pulls/{pull_number}/files`](https://docs.github.com/en/rest/pulls/pulls#list-pull-requests-files) | 50 |
| `listCommits` | [`GET /repos/{owner}/{repo}/commits`](https://docs.github.com/en/rest/commits/commits#list-commits) with `since`, `until`, and optional `path` and `sha` | 30 |

`listMergedPullRequests` uses search because the pull request list endpoint
cannot filter by merge time. The window is inclusive at both ends, uses UTC,
and has second precision, as GitHub search ranges define it. Search cannot sort
by merge time, so results are ordered by pull request creation time, newest
first. Creation time never changes, which keeps page boundaries stable while
later comments update a pull request. `TotalCount` and `IncompleteResults`
come from GitHub. GitHub search serves only the first 1000 matches, so a page
that starts after them selects `defect` before GitHub is called. Narrow the
window when `TotalCount` exceeds 1000. Search results do not include the base
branch. GitHub answers a search of a missing or inaccessible repository with
`422`, which selects `providerRejected`.

`listCommits` passes `Since` and `Until` to GitHub as `since` and `until`, and
`Ref` as `sha`; an empty `Ref` reads the default branch. GitHub answers `409`
for an empty repository. That repository has no commits in any window, so the
operation selects `listed` with no commits and records `repositoryEmpty` in the
receipt metadata. A missing ref or repository selects `notFound`.

Primary and secondary rate limits retry. A rate limit is any `429`, or a `403`
with `Retry-After`, with `X-RateLimit-Remaining: 0`, or with an error message
that says a rate limit was exceeded. GitHub can send a secondary rate limit
without either header, so the connector reads the bounded error body's
`message` field for this check only and never returns it. The retry waits for
`Retry-After` seconds, for `X-RateLimit-Reset` when `X-RateLimit-Remaining` is
`0`, and otherwise for one minute, as GitHub's rate-limit guidance says.

The change queries allow five Execute attempts within 65 minutes, so Dex can
wait out GitHub's hourly primary rate-limit window. The Step fails when a delay
does not fit in the remaining budget or the attempts run out, and then the
Flow fails unless `StepOptionsOverride` sets `dex.ProceedToOnExecuteFailure`.
When a delay is longer than the Step's async local execution can wait, Dex
sends the first retry at once as it moves the Step to regular execution; later
retries wait for GitHub's delay. To fail sooner, give `StepOptionsOverride` a
complete `ExecuteRetry` policy with a shorter `TotalDuration`.

A `5xx` response or a transport failure retries with the policy's backoff.
`401` selects `authorizationRevoked`, any other `403` selects
`insufficientScope`, and `404` selects `notFound`. The connector does not
follow redirects, so a renamed or transferred repository selects
`providerRejected`; use its current owner and name. Malformed JSON, an item
without required fields, or a next link that does not advance selects
`invalidResponse`. A response over `maxResponseBytes` selects
`invalidResponse` with a response-size failure; request a smaller page.

These configuration fields bound text in Results. Each counts Unicode
characters, keeps the beginning, and sets the matching truncation flag:

| Field | Default | Bounds |
| --- | --- | --- |
| `maxPullRequestBodyCharacters` | 4000 | `MergedPullRequest.Body`, `BodyTruncated` |
| `maxPatchCharacters` | 4000 | `PullRequestFile.Patch`, `PatchTruncated` |
| `maxCommitMessageCharacters` | 4000 | `CommitSummary.Message`, `MessageTruncated` |

GitHub omits the patch for a binary or very large file, which leaves `Patch`
empty with `PatchTruncated` false.

The [repository changes example](examples/repository-changes/README.md) runs
all three queries in one Flow from Dex Web **Start Flow**.

## Install and verify

Install the published module:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/github@v0.21.0
```

Verify it independently:

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

An opt-in live check uses a dedicated test account token and never prints it:

```bash
GITHUB_CONNECTOR_TEST_TOKEN=... GOWORK=off go test -tags=live -run TestLive ./...
```

The dedicated token must grant exactly the documented signup scopes. The live
test reads at most five public repositories, and at most five merged pull
requests, changed files, and commits from the last 30 days of
`superdurable/dex`. It never logs the token.

Applications own the OAuth start/callback, state, PKCE verifier, token exchange,
one-use token deletion, Result Attributes, and branch behavior. Provider calls
occur only inside Dex Step `Execute` through the generated factories.

The generated factories preserve actionable authorization and not-found
branches. Other conclusive GitHub API refusals use `providerRejected`, while
malformed or oversized responses use `invalidResponse`. Invalid local input or
connection configuration uses the standard `defect` branch.

During startup, load the project configuration that Dex Web or Superverse
Studio writes and open the connection by name, as
[`examples/repository-changes/main.go`](examples/repository-changes/main.go)
does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := github.NewProjectConnection(project, repositorychanges.ConnectionName)
if err != nil {
	return err
}
```

`LoadFromEnvironment` reads the `DEX_PROJECT_*` environment described in
[project configuration](../../sdkgo/projectconfig/README.md). Use the same
static connection name as `ConnectionName` in each generated GitHub Step
config. The latest credential state is read before every provider call, so
refresh and reauthorization do not require restarting the application.
