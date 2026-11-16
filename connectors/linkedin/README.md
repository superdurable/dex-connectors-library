# LinkedIn Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/linkedin/go@v0.25.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/linkedin`.

The LinkedIn connector reads the authenticated member's bounded OpenID Connect
UserInfo claims for Customer Onboarding, and its `profileChanged` Trigger
reports when that same member's own claims change. It does not scrape LinkedIn pages and
does not infer employment history, title, company, or a real-world identity.

The trusted host owns authorization redirect handling, callback validation,
issuer/audience/state/nonce/PKCE checks, token exchange, and credential
persistence. The connection's credentials stay in project storage; the
connector reads them for each provider Query, and they never enter a Flow.

Required scopes are exactly `openid profile email`. The manifest exposes the
LinkedIn issuer, discovery, authorization, token, and UserInfo endpoints so a
trusted host can render configuration and run the OIDC callback safely.

Ordinary **Sign in with LinkedIn using OpenID Connect** applications may
receive an access token without a refresh token and must reauthorize when it
is within five minutes of expiry. LinkedIn documents [programmatic refresh
tokens](https://learn.microsoft.com/en-us/linkedin/shared/authentication/programmatic-refresh-tokens)
for approved products, including approved Marketing Developer Platform
partners. When LinkedIn returns `refresh_token`, the connector refreshes the
access token within five minutes of its recorded expiry, and project storage
persists any replacement token atomically. After a 401 the connector asks once
for a refresh, which project storage performs only when the recorded expiry has
passed, and then retries the UserInfo request once; otherwise the 401 selects
`authorizationRevoked`. Missing, expired, or rejected refresh material
changes the connection to `reauthorization_required`.

The generated factory preserves actionable authorization and not-found
branches. Other conclusive LinkedIn API refusals use `providerRejected`, while
malformed or oversized responses use `invalidResponse`. Invalid local input or
connection configuration uses the standard `defect` branch.

## profileChanged Trigger

`profileChanged` delivers one event whenever the connected member's own
profile claims change. It watches only the member who authorized the
connection; it cannot watch other members, connections, or companies. LinkedIn
has no profile change feed or webhook for Sign In with LinkedIn using OpenID
Connect, so each poll sends one
`GET https://api.linkedin.com/v2/userinfo` with the connection's existing
`openid profile email` grant and compares the claims with the ones the last
poll committed. No new scope or reauthorization is needed.

The tracked claims are the ones LinkedIn documents in its
[UserInfo response](https://learn.microsoft.com/en-us/linkedin/consumer/integrations/self-serve/sign-in-with-linkedin-v2#response-body-schema):

| `changedFields` entry | UserInfo claim |
| --- | --- |
| `name` | `name` |
| `givenName` | `given_name` |
| `familyName` | `family_name` |
| `email` | `email` (optional) |
| `emailVerified` | `email_verified` (optional) |
| `pictureUrl` | `picture` |
| `locale` | `locale` |

Each `ProfileChangedEvent` carries `subject` (the `sub` claim), the `previous`
and `current` `ProfileClaims`, and `changedFields` in the order above. Unlike
`getAuthenticatedProfile`, `ProfileClaims` keeps an unverified or absent email,
because losing verification is itself a change.

- **Baseline.** The first poll stores the current profile and delivers nothing.
- **Cursor.** The binding's durable poll cursor holds the member's `sub`, a
  hash of the tracked claims, a count of delivered changes, and the last
  profile, so the next event can carry it as `previous`. The profile, email
  included, therefore sits in project storage beside the binding's Trigger
  inbox; credentials never do.
- **Pictures.** Only the picture URL without its query counts, because
  LinkedIn media URLs carry an expiring signature in the query. A re-signed URL
  of the same picture is not a change; the stored profile still keeps the
  latest URL.
- **Event IDs.** Each ID hashes the `sub`, the previous and current hashes, and
  the count of delivered changes, so a refetch after a crash keeps the ID, and
  a profile edited back and forth gets a new ID each time.
  `occurredAt` is the poll time, since UserInfo reports no modification time.
- **Reset.** An undecodable cursor, or a profile whose `sub` differs from the
  stored one (the connection was reauthorized as another member), records a new
  baseline, increments the cursor's `resetCount`, and logs
  `poll cursor invalid; reseeding` at WARN. Differences in that gap are not
  delivered.
- **Failures.** A 429 pauses every binding of the connection for
  `Retry-After`, or backs off exponentially without it. LinkedIn counts calls
  per member and per app per UTC day and returns 429 above them
  ([rate limits](https://learn.microsoft.com/en-us/linkedin/shared/api-guide/concepts/rate-limits));
  your app's limits appear under its **Analytics** tab at
  https://www.linkedin.com/developers/apps. A 401 first asks once for a
  credential refresh, as the Query does; a 401 after that, a 403, a 5xx, or an
  invalid response is a failed poll that backs off per binding, and a
  `reauthorization_required` connection fails each poll until it is reconnected.
- **Poll interval.** The connection's `pollInterval` setting defaults to 15
  minutes and must be at least one minute. Profiles change rarely, so a longer
  interval spends fewer of the daily calls the Query also uses.

`linkedin.NewProjectProfileTriggerRunner` loads saved bindings and records each
change in the binding's project Trigger inbox before the profile commits in its
project poll cursor, as
[`examples/profile-changed/main.go`](go/examples/profile-changed/main.go) does:

```go
triggerRunner, err := linkedin.NewProjectProfileTriggerRunner(project, profilechanged.ConnectionName, linkedin.ProjectProfileTriggerRunnerConfig{
	ProfileChangedRoutes: []linkedin.ProjectProfileChangedTriggerRoute{{
		BindingName: profilechanged.ProfileChangedTriggerBinding,
		Target: sdkgo.NewDexFlowTriggerTarget(client, flow, profilechanged.AcceptEveryProfileChange, profilechanged.ResolveFlowID,
			profilechanged.MapToFlowInput, sdkgo.WithTriggerLogger(logger)),
	}},
}, linkedin.WithLogger(logger))
```

`linkedin.NewProfileTriggerRunner` takes targets and cursor stores directly,
and the generated `linkedin.NewProfileChangedTrigger` keeps the cursor in
memory, so it records a new baseline on every start.
`ProfileChangedTriggerConfiguration` is empty and the binding has no Dex Web
field, because it always watches the connected member's own profile. The
[profile-changed example](go/examples/profile-changed/README.md) reads the
verified profile back with `getAuthenticatedProfile`.

## Mocks

`linkedinmock.New` supplies a typed connection. Its default answers an empty
profile request for the authorized Ada Lovelace member with a verified email.
Named cases cover an unverified email, a grant without email scope, an HTTP 403
UserInfo refusal, revoked authorization, a missing member, and a conclusive
UserInfo rejection. They describe alternate connections; script a case when a
test uses one of those conditions.

The [runnable integration test](go/examples/authenticated-profile/flow/workflow_mock_integration_test.go)
runs the example Flow on a real Dex Worker without LinkedIn:

```go
mock := linkedinmock.New(t, ConnectionName)
mock.GetAuthenticatedProfile().Respond(linkedinmock.GetAuthenticatedProfileVerifiedSignupProfile())
flow := NewFlow(mock.Connection())
```

It checks the Flow output and the operation input:

```go
calls := mock.GetAuthenticatedProfile().Calls()
require.Len(t, calls, 1)
require.Equal(t, flowID, calls[0].FlowID)
require.Equal(t, linkedin.GetAuthenticatedProfileInput{}, calls[0].Input)
```

To script an explicit response, use `mock.GetAuthenticatedProfile().Respond(linkedinmock.GetAuthenticatedProfileVerifiedSignupProfile())`.
See [`sdkgo/connectormock`](../../sdkgo/README.md#connector-mocks) for scripting.

## Examples

[`examples/profile-changed`](go/examples/profile-changed/README.md) starts one
Flow per change of the connected member's own profile through
`profileChanged`.

[`examples/authenticated-profile`](go/examples/authenticated-profile) is a
runnable Dex Web **Start Flow** example that loads the authorized member's
verified UserInfo profile. Its
[`main.go`](go/examples/authenticated-profile/main.go) loads the project
configuration that Dex Web or Superverse Studio writes and opens the
connection by name:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := linkedin.NewProjectConnection(project, authenticatedprofile.ConnectionName)
if err != nil {
	return err
}
```

`LoadFromEnvironment` reads the `DEX_PROJECT_*` environment described in
[project configuration](../../sdkgo/projectconfig/README.md).

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

An opt-in live test is available with a dedicated test member:

```bash
GOWORK=off LINKEDIN_CONNECTOR_TEST_TOKEN=... go test -tags=live ./...
```

## Python package

Install `dex-connector-linkedin==0.1.0` and use the [authenticated profile Python example](python/examples/authenticated_profile/README.md). Its project connection uses generated OAuth credential resolution and refresh. The Query returns only bounded UserInfo claims and a verified email. The Go-recorded provider conformance scenarios and Dex example use inert credentials; live LinkedIn authorization and UserInfo behavior remain unverified.
## TypeScript package

[`@superdurable/connector-linkedin`](typescript/README.md) implements `getAuthenticatedProfile` with manifest-generated OAuth refresh, private and shared credential decoding, and the static `linkedin-signup` example connection. The TypeScript [authenticated-profile Flow](typescript/examples/authenticated-profile/flow/workflow.ts) runs on a real Dex Worker and persists only bounded UserInfo claims. Go-recorded [conformance scenarios](../../conformance/connectors/linkedin/scenarios.json) cover provider branches, retries, transport failure, and missing credentials. Live LinkedIn OAuth and UserInfo behavior remains unverified.
