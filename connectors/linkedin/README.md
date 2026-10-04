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
