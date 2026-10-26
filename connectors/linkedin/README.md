# LinkedIn Connector

The LinkedIn connector reads the authenticated member's bounded OpenID Connect
UserInfo claims for Customer Onboarding. It does not scrape LinkedIn pages and
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

## Mocks

`linkedinmock.New` supplies a typed connection. Its default answers an empty
profile request for the authorized Ada Lovelace member with a verified email.
Named cases cover an unverified email, a grant without email scope, an HTTP 403
UserInfo refusal, revoked authorization, a missing member, and a conclusive
UserInfo rejection. They describe alternate connections; script a case when a
test uses one of those conditions.

The [runnable integration test](examples/authenticated-profile/flow/workflow_mock_integration_test.go)
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

## Example

[`examples/authenticated-profile`](examples/authenticated-profile) is a
runnable Dex Web **Start Flow** example that loads the authorized member's
verified UserInfo profile. Its
[`main.go`](examples/authenticated-profile/main.go) loads the project
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
