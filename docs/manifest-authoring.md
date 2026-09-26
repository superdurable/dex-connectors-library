# Connector manifest and code generation

Each connector module owns `connector.yaml`. Its `metadata.company` and
`metadata.version` fields feed the public Connector catalog and declarative
release workflow. A version change requests a release after merge; leaving it
unchanged explicitly defers release.

An operation declares:

- stable lower-camel `name` and exported `goName`;
- exported local Go `inputType` and `outputType`;
- Query or Mutation kind and idempotency requirement;
- every stable branch, including defect and Mutation uncertainty when a write can be ambiguous;
- `optional: true` on every branch that is not a happy path;
- progress capabilities used to generate typed Stream fields;
- `authorization: required` for operations that require an authorized
  connection, or `none` for public operations;
- Dex Execute timeout, retry, heartbeat, and durability defaults.

`auth.oauth2.protocol` distinguishes plain OAuth 2.0 from OpenID Connect. It
defaults to `oauth2` for existing manifests. An `oidc` manifest must also
declare HTTPS issuer, discovery, and UserInfo endpoints and require a nonce.
The application owns state, PKCE verifier, nonce, callback validation, and
one-time token lifecycle; connector credentials remain typed secret fields.

Providers that issue more than one OAuth token declare `userScopes` and map
token-response JSON paths to credential fields. Fields without a mapping are
entered through a host-owned secret form and never sent to Connector Studio.

```yaml
oauth2:
  scopes: [chat:write]
  userScopes: [channels:history]
  credentialMappings:
    - {credential: bot_token, source: access_token}
    - {credential: user_token, source: authed_user.access_token}
```

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

The generated surface also includes `NewLocalConnection` and a
`ConnectionName` field on every operation factory config. The local helper
decodes every manifest authentication field, including multiple optional
secret fields, without assuming a single-token OAuth response. Keep secret
fields typed as `secretString` so code generation constructs `SecretString`
at the provider boundary.

A Trigger declares a typed provider event and its binding-configuration type.
The application decides whether an event starts a Flow, invokes an RPC, or uses
another target:

```yaml
triggers:
  - name: channelThreadCreated
    goName: ChannelThreadCreated
    eventType: MessageEvent
    configurationType: ChannelThreadCreatedTriggerConfiguration
    description: Receive a matching top-level channel message.
```

Code generation creates direct and local-config Trigger factories. The
provider implements the generated source hook and owns transport acknowledgement,
reconnection, filtering, and event decoding. The application selects the target
and owns Flow ID, start-input, and RPC mapping.

```bash
go run ./cmd/connectorctl validate connectors/openai/connector.yaml
go run ./cmd/connectorctl generate connectors/openai/connector.yaml
go run ./cmd/connectorctl generate --check connectors/openai/connector.yaml
```

Adding a connector also requires its own `go.mod`, README, generated file, and
tests. Add its repository-relative directory to the sorted root registry.
Directories may have any depth below `connectors/`. The first directory is
the company: `metadata.company` must slug to that folder, and the folder
must contain `logo.svg`.

```bash
go run ./cmd/connectorctl catalog --check --registry connectors.yaml
go run ./cmd/connectorctl catalog --registry connectors.yaml --output dist/pages/catalog.yaml
```

CI rejects unregistered, missing, unsafe, or symlinked connector paths.
Connector `go.mod` files
must pin an already-published SDK version and cannot contain `replace`,
pseudo-version, branch, or commit dependencies.

## Studio setup bundle

A connector with a provider-specific setup experience declares `spec.studio`:

```yaml
studio:
  setup:
    entrypoint: index.html
    hostApiRange: ">=0.1.0 <0.2.0"
    backendCapabilities: [oauth.connection.manage]
    mockScenarios: [not-configured, connected, revoked]
    icon: icon.svg
```

The UI build output must contain the entrypoint and icon. Release automation
packages regular files only, enforces file-count and expanded-size limits, and
creates a deterministic tarball. Connector UI runs in a sandbox iframe and may
request only the listed Host API capabilities. It must never receive or render
credential values.
