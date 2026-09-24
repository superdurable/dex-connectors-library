# Connector manifest and code generation

Each connector module owns `connector.yaml`. The manifest is versionless;
directory-prefixed Git tags define published versions.

An operation declares:

- stable lower-camel `name` and exported `goName`;
- exported local Go `inputType` and `outputType`;
- Query or Mutation kind and idempotency requirement;
- every stable branch, plus defect and Mutation uncertainty branches;
- Result Attribute requirement and progress capabilities;
- `authorization: required` for operations that require an authorized
  connection, or `none` for public operations;
- Dex Execute timeout, retry, heartbeat, and durability defaults.

`auth.oauth2.protocol` distinguishes plain OAuth 2.0 from OpenID Connect. It
defaults to `oauth2` for existing manifests. An `oidc` manifest must also
declare HTTPS issuer, discovery, and UserInfo endpoints and require a nonce.
The application owns state, PKCE verifier, nonce, callback validation, and
one-time token lifecycle; connector credentials remain typed secret fields.

```yaml
auth:
  type: oauth2
  connectionKind: linkedin-oidc
  oauth2:
    protocol: oidc
    authorizationEndpoint: https://www.linkedin.com/oauth/v2/authorization
    tokenEndpoint: https://www.linkedin.com/oauth/v2/accessToken
    scopes: [openid, profile, email]
    pkce: true
    oidc:
      issuer: https://www.linkedin.com
      discoveryEndpoint: https://www.linkedin.com/oauth/.well-known/openid-configuration
      userInfoEndpoint: https://api.linkedin.com/v2/userinfo
      nonceRequired: true
```

`connectorctl generate` creates the connector-specific Config, Credentials,
Connection, branch constants, definitions, output aliases, config structs, and
factory functions. Applications use those factories directly, such as
`openai.NewCreateResponseStep`, while generic SDK factories remain an advanced
escape hatch.

```bash
go run ./cmd/connectorctl validate connectors/openai/connector.yaml
go run ./cmd/connectorctl generate connectors/openai/connector.yaml
go run ./cmd/connectorctl generate --check connectors/openai/connector.yaml
```

Adding a connector also requires its own `go.mod`, README, generated file, and
tests. Regenerate the static release selector in the same PR:

```bash
go run ./cmd/connectorctl release-workflow connectors .github/workflows/release-connector.yml
```

CI rejects stale generated code or release choices. Connector `go.mod` files
must pin an already-published SDK version and cannot contain `replace`,
pseudo-version, branch, or commit dependencies.
