# GitHub Connector

The GitHub connector is an independent Go module for signup profile evidence.
It exposes generated Dex Query Step factories for:

- `GetAuthenticatedProfile`: authenticated account plus primary verified email;
- `ListPublicRepositories`: recent-first, deduplicated, bounded public
  repositories owned by one login.

The OAuth connection requests only `read:user user:email`. The connector never
requests `repo`, `public_repo`, organization, or write scopes. Repository reads
use only `GET /users/{login}/repos` and never return private repositories,
source, README, events, issues, or raw GitHub responses.

GitHub documents [`read:user` and `user:email` as the profile and email OAuth
scopes](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/scopes-for-oauth-apps).
The connector follows GitHub's authoritative `Retry-After` and
`X-RateLimit-Reset` headers for safe Query retries.

The default repository limit is 100 and the hard cap is 500. Provider pages use
at most 100 items. Results report truncation and retain only bounded profile and
repository metadata.

Install the published module:

```bash
go get github.com/superdurable/dex-connectors-library/connectors/github@v0.6.0
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
test reads at most five public repositories and never logs the token.

Applications own the OAuth start/callback, state, PKCE verifier, token exchange,
one-use token deletion, Result Attributes, and branch behavior. Provider calls
occur only inside Dex Step `Execute` through the generated factories.

The generated factories preserve actionable authorization and not-found
branches. Other conclusive GitHub API refusals use `providerRejected`, while
malformed or oversized responses use `invalidResponse`. Invalid local input or
connection configuration uses the standard `defect` branch.

With local Dex Web, call `localconfig.LoadFromEnvironment` and
`github.NewLocalConnection(store, "reviewer")` during startup. Use the same
static `ConnectionName: "reviewer"` in each generated GitHub Step config.
The access token is reloaded before every provider call, so reauthorization
does not require restarting the application.
