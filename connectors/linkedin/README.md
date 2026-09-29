# LinkedIn Connector

The LinkedIn connector reads the authenticated member's bounded OpenID Connect
UserInfo claims for Customer Onboarding. It does not scrape LinkedIn pages and
does not infer employment history, title, company, or a real-world identity.

The trusted host owns authorization redirect handling, callback validation,
issuer/audience/state/nonce/PKCE checks, token exchange, and credential
persistence. This connector receives only the access token needed for its
provider Query. Superverse keeps refresh material in its credential broker;
the application never receives it.

Required scopes are exactly `openid profile email`. The manifest exposes the
LinkedIn issuer, discovery, authorization, token, and UserInfo endpoints so a
trusted host can render configuration and run the OIDC callback safely.

Ordinary **Sign in with LinkedIn using OpenID Connect** applications may
receive an access token without a refresh token and must reauthorize when it
expires. LinkedIn documents [programmatic refresh
tokens](https://learn.microsoft.com/en-us/linkedin/shared/authentication/programmatic-refresh-tokens)
for approved products, including approved Marketing Developer Platform
partners. When LinkedIn returns `refresh_token`, Dex refreshes within five
minutes of access-token expiry, persists any replacement token atomically, and
retries one UserInfo request after a 401. Expired or rejected refresh material
changes the connection to `reauthorization_required`.

The generated factory preserves actionable authorization and not-found
branches. Other conclusive LinkedIn API refusals use `providerRejected`, while
malformed or oversized responses use `invalidResponse`. Invalid local input or
connection configuration uses the standard `defect` branch.

## Example

[`examples/authenticated-profile`](examples/authenticated-profile) is a
runnable Dex Web **Start Flow** example that loads the authorized member's
verified UserInfo profile.

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

An opt-in live test is available with a dedicated test member:

```bash
GOWORK=off LINKEDIN_CONNECTOR_TEST_TOKEN=... go test -tags=live ./...
```
