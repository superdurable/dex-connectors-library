# LinkedIn Connector

The LinkedIn connector reads the authenticated member's bounded OpenID Connect
UserInfo claims for Customer Onboarding. It does not scrape LinkedIn pages and
does not infer employment history, title, company, or a real-world identity.

The Customer Onboarding App owns authorization redirect handling, callback
validation, issuer/audience/state/nonce/PKCE checks, token exchange, and the
one-use credential envelope. This connector receives only the access token
needed for its provider Query.

Required scopes are exactly `openid profile email`. The manifest exposes the
LinkedIn issuer, discovery, authorization, token, and UserInfo endpoints so a
trusted host can render configuration and run the OIDC callback safely.

The generated factory preserves actionable authorization and not-found
branches. Other conclusive LinkedIn API refusals use `providerRejected`, while
malformed or oversized responses use `invalidResponse`. Invalid local input or
connection configuration uses the standard `defect` branch.

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

An opt-in live test is available with a dedicated test member:

```bash
GOWORK=off LINKEDIN_CONNECTOR_TEST_TOKEN=... go test -tags=live ./...
```
