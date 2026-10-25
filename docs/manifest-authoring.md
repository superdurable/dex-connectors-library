# Connector manifest and code generation

Each connector module owns `connector.yaml`. Its `metadata.company` and
`metadata.version` fields feed the public Connector catalog and declarative
release workflow. A version change requests a release of that connector only
after merge; leaving it unchanged explicitly defers release. See
[Choosing a connector version](versioning-and-releases.md#choosing-a-connector-version).

An operation declares:

- stable lower-camel `name` and exported `goName`;
- exported local Go `inputType` and `outputType`;
- Query or Mutation kind and idempotency requirement;
- every stable branch, including defect and Mutation uncertainty only when the
  request may have reached the provider, a repeat is unsafe, and read-back
  cannot confirm the outcome;
- `optional: true` on every branch that is not a happy path;
- progress capabilities used to generate typed Stream fields;
- `authorization: required` for operations that require an authorized
  connection, or `none` for public operations;
- Dex Execute timeout, retry, heartbeat, and durability defaults.

Declare a branch for each definite business outcome the application may need
to handle, such as `notFound` or `providerRejected`. Return Retry for transient
failures that are safe to repeat; the Step retries until its budget runs out,
then the Flow fails for platform debugging. `uncertain` is limited to a write
that may have reached the provider, has no safe repeat mechanism (a provider
idempotency key, stable deduplication ID, absolute replacement, upsert, or
delete), and cannot be confirmed by read-back. A request that never left the
process, a 429, and a 503 return Retry. Exhausted retries never become
uncertainty.

`auth.oauth2.protocol` distinguishes plain OAuth 2.0 from OpenID Connect. It
defaults to `oauth2` for existing manifests. An `oidc` manifest must also
declare HTTPS issuer, discovery, and UserInfo endpoints and require a nonce.
The application owns state, PKCE verifier, nonce, callback validation, and
one-time token lifecycle; connector credentials remain typed secret fields.

Providers that issue more than one OAuth token declare `userScopes` and map
token-response JSON paths to credential fields. Fields without a mapping are
entered through a host-owned secret form and never sent to Connector Studio.

Every credentialed connector also declares `auth.guide`. Dex Web presents its
starting URL and ordered provider-console steps before any credential fields.
The steps name the exact page path and creation action; OAuth guides also tell
the author to register the Redirect URI that Dex Web displays and makes
copyable.

```yaml
guide:
  startURL: https://provider.example/settings/keys
  steps:
    - Open Settings > API keys and choose Create key.
    - Copy the secret shown once and store it in the field below.
```

```yaml
oauth2:
  scopes: [chat:write]
  userScopes: [channels:history]
  credentialMappings:
    - {credential: bot_token, source: access_token}
    - {credential: user_token, source: authed_user.access_token}
```

When a required credential is available from a read-only provider identity
endpoint, declare `credentialDerivations` instead of rendering a duplicate
free-text field. Dex Web calls the HTTPS endpoint with the new access token,
extracts the named string claim, and, when `verifiedBy` is present, accepts it
only when that boolean claim is true.

```yaml
credentialDerivations:
  - credential: primary_email
    endpoint: https://openidconnect.googleapis.com/v1/userinfo
    source: email
    verifiedBy: email_verified
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

Do not copy that example's scope names into a Google manifest. A manifest with
`spec.provider: google` names every scope except `openid` by its canonical URI,
such as `https://www.googleapis.com/auth/userinfo.email` and
`https://www.googleapis.com/auth/userinfo.profile`, never the `email` and
`profile` aliases listed in Google's
[scope reference](https://developers.google.com/identity/protocols/oauth2/scopes).
Google's token response reports an alias grant under its canonical URI, and
Dex Web matches granted scopes literally, so an alias request fails the OAuth
callback with `CONNECTOR_OAUTH_SCOPE_INSUFFICIENT`. `connectorctl` tests reject
any registered Google connector that requests an alias in `scopes` or
`userScopes`.

`connectorctl generate` creates the connector-specific Config, Credentials,
Connection, branch constants, definitions, output aliases, config structs, and
factory functions. Applications use those factories directly, such as
`openai.NewCreateResponseStep`, while generic SDK factories remain an advanced
escape hatch.

The generated surface also includes `NewProjectConnection` and a required
`ConnectionName` field on every operation and Trigger factory config.
`NewProjectConnection` decodes every manifest authentication field, including
multiple optional secret fields, without assuming a single-token OAuth
response. Keep secret
fields typed as `secretString` so code generation constructs `SecretString`
at the provider boundary.

### Endpoints and scopes from connection settings

A connector for an OAuth 2.0 or OpenID Connect server that the connection owner
chooses cannot know the server's URLs. `authorizationEndpoint`,
`tokenEndpoint`, the `oidc` `issuer`, `discoveryEndpoint` and
`userInfoEndpoint`, a `credentialDerivations` `endpoint`, and `scopes` may each
name a connection setting instead of a constant:

```yaml
configuration:
  fields:
    - {name: authorizationEndpoint, goName: AuthorizationEndpoint, type: url, description: "...", required: true}
    - {name: tokenEndpoint, goName: TokenEndpoint, type: url, description: "...", required: true}
    - {name: scopes, goName: Scopes, type: stringList, description: "...", required: true}
auth:
  type: oauth2
  oauth2:
    authorizationEndpoint: {fromSetting: authorizationEndpoint}
    tokenEndpoint: {fromSetting: tokenEndpoint}
    scopes: {fromSetting: scopes}
    pkce: true
```

`fromSetting` is the only key of the mapping. It names a `required: true`
configuration field of type `url` for an endpoint and `stringList` for
`scopes`. A single-method `spec.auth` reads `spec.configuration`; a method in
`auth.methods` reads `spec.configuration` and its own `configuration.fields`,
never another method's. The value is a connection setting, so it is never
secret, and the connection stores it under the field name. A field that
supplies an endpoint needs no default: the connection owner enters it.

An endpoint written as a string keeps today's rule: an absolute HTTPS URL. An
endpoint that names a setting carries no scheme rule in the library, because
only the consumer knows where the value comes from. The consumer that reads
the setting decides whether plain HTTP is acceptable, such as for an identity
provider on a private network. Generated `Config.Validate` requires only an
absolute URL for a `url` field.

Release metadata writes a fixed endpoint or scope list as before and a
reference as `{"fromSetting": "<field name>"}`, so a consumer that reads the
metadata must accept both forms for every field above.

### Multiple authentication methods

Use `auth.methods` when one named connection can use more than one provider
authentication model. Every method has a stable lowercase kebab-case ID, such
as `google-oauth`, its own fields, guide, and optional OAuth metadata.
`defaultMethod` must identify the method the setup UI selects first; mark at
most one method `recommended`.

```yaml
auth:
  defaultMethod: google-oauth
  methods:
    - id: google-oauth
      displayName: Google OAuth
      description: Authorize an individual Google or Workspace account.
      recommended: true
      type: oauth2
      connectionKind: gmail-google-oauth
      fields:
        - {name: oauth_client_id, goName: OAuthClientID, type: string, description: OAuth application client ID., required: true}
        - {name: oauth_client_secret, goName: OAuthClientSecret, type: secretString, description: OAuth application client secret., required: true}
        - {name: access_token, goName: AccessToken, type: secretString, description: Short-lived access token produced by OAuth., required: true}
        - {name: refresh_token, goName: RefreshToken, type: secretString, description: Long-lived refresh token produced by OAuth., required: true}
      guide:
        startURL: https://console.cloud.google.com/apis/credentials
        steps: [Create a Web application and add the Redirect URI shown by Dex Web.]
      oauth2:
        authorizationEndpoint: https://accounts.google.com/o/oauth2/v2/auth
        tokenEndpoint: https://oauth2.googleapis.com/token
        authorizationParameters: {access_type: offline, prompt: consent}
        clientIDCredential: oauth_client_id
        clientSecretCredential: oauth_client_secret
        scopes: [openid]
        pkce: true
    - id: workspace-service-account
      displayName: Workspace service account
      description: Use administrator-managed domain-wide delegation.
      type: serviceAccount
      connectionKind: gmail-workspace-service-account
      fields:
        - {name: service_account_key, goName: ServiceAccountKey, type: secretString, description: Service-account JSON key., required: true}
        - {name: delegated_user, goName: DelegatedUser, type: string, description: Workspace user to impersonate., required: true}
      guide:
        startURL: https://console.cloud.google.com/iam-admin/serviceaccounts
        steps: [Create a service account and enable domain-wide delegation.]
```

A connection selects exactly one method. Generated credentials include
`AuthMethodID` plus the union of method fields, and only the selected method's
required fields are validated. The project connection stores the selected
method as `auth_method` in the credential and as `AuthMethodID` in the
connection configuration.

The optional `auth.methodLabel` is the singular noun the setup UI uses for a
method, such as `Provider`. It is 1 to 32 characters without surrounding
whitespace, and only a manifest with `methods` declares it.

A method may declare non-secret `configuration.fields`, such as a Claude
workspace ID that only the Claude method uses. They take the same types as
`spec.configuration.fields`, never `secretString`. Their names are unique
across `spec.configuration.fields` and every method's configuration fields,
and no name repeats a credential field. The setup UI shows them with their
method, and `required: true` means required while that method is selected.
Code generation adds each one to `Config` as an optional field: `Config`
validation checks its type and never requires it.

`auth_method` and `AuthMethodID` are reserved for the selected method; no
credential field may use them as its name or `goName`.

#### Several API-key methods

[schema/testdata/api-key-methods.yaml](../schema/testdata/api-key-methods.yaml)
declares one `apiKey` method per model provider, and each connection selects
one of them. `methodLabel: Provider` names its methods, and each method's
`configuration.fields` hold that provider's non-secret settings, such as the
Claude workspace ID. See
[Generated Config and Credentials](connector-contract.md#generated-config-and-credentials)
for its generated code.

#### Studio unit for a connection field

A `spec.configuration` field may declare `studioUnit: {unit, port}`. Once the
connection is saved, the connection form renders that Studio unit for the field
instead of a plain input, and the unit's output port writes the value. In
[schema/testdata/api-key-methods.yaml](../schema/testdata/api-key-methods.yaml),
the `model` field uses the `modelPicker` unit's `model` port.
`unit` names a `spec.studio.units` ID, and `port` names one of that unit's
outputs whose type equals the field type, such as `string` for a `string`
field. Credential fields and method configuration fields cannot declare
`studioUnit`. A host that does not support it shows the plain input.

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

Code generation creates the direct `New<Trigger>Trigger` factory and its
binding definition. The connector's project runner loads the stored bindings
and keeps acknowledged events in their durable project inboxes. The
provider implements the generated source hook and owns transport acknowledgement,
reconnection, filtering, and event decoding. The application selects the target
and owns Flow ID, start-input, and RPC mapping.

```bash
go run ./cmd/connectorctl validate connectors/openai/connector.yaml
go run ./cmd/connectorctl generate connectors/openai/connector.yaml
go run ./cmd/connectorctl generate --check connectors/openai/connector.yaml
```

Adding a connector also requires its own `go.mod`, README, generated file, and
tests. Add its repository-relative directory to the sorted root `catalog.yaml`.
That membership entry is the only shared inventory change. Its README owns the
provider architecture, security boundary, runnable examples, and verification.
Directories may have any depth below `connectors/`. The first directory is
the company: `metadata.company` must slug to that folder, and the folder
must contain `logo.svg`.

```bash
go run ./cmd/connectorctl catalog --check --catalog catalog.yaml
go run ./cmd/connectorctl catalog --catalog catalog.yaml --output dist/pages/catalog.yaml
```

CI rejects unregistered, missing, unsafe, or symlinked connector paths.
Connector `go.mod` files must require an already-published SDK release and
cannot contain `replace`, pseudo-version, branch, or commit dependencies. A
required connector module must use a released, complete tag, and no other
module from this repository may be required; see
[Connector dependencies](versioning-and-releases.md#connector-dependencies).
The Dex Go SDK requirement is a minimum; see
[Dex versions](versioning-and-releases.md#dex-versions).

## Credential refresh and minting

A host that keeps connections, such as a platform that shares one connection
between applications, refreshes and mints their access tokens itself. It runs
no connector code, so each method declares the token requests and how their
responses map into credentials. A method declares one of three grants:

- an `oauth2` method that maps a refresh token refreshes with it;
- an `apiKey` method with `clientCredentials` mints with the client
  credentials grant from its stored client ID and secret;
- a `serviceAccount` method with `serviceAccount.jwtBearer` mints with a
  signed JWT bearer assertion.

A method without one, such as a plain API key, keeps its credential as stored,
so a method whose credential expires must declare its grant. `auth.refreshable`
is true exactly when some method declares one, and a generated test fails when
a connector's credential refresh driver refreshes a method without a
declaration (see
[Credential refresh agreement](#credential-refresh-agreement)).

Every grant shares these rules:

- A response that carries an OAuth `error` member is a failure, whatever its
  HTTP status, because some providers report errors with HTTP 200.
- A failure below HTTP 500 whose code is in `terminalErrorCodes` means the
  stored grant can never succeed again: the connection needs reauthorization
  or new client or key material. Every other failure is retried.
- `credentialMappings` map the response into credential fields by dotted JSON
  path. A mapped value that the response omits keeps the stored one, so a
  provider that does not rotate its refresh token keeps it.
- `missingExpiry` decides the expiry of a token whose response has no
  `expires_in`: `keep`, the default, stores it without an expiry, so the host
  replaces it only after the provider rejects it; `{lifetime: 2h}` sets one.
- `additionalParameters` are fixed, non-secret parameters added to every
  request. They cannot replace `grant_type`, `refresh_token`, `client_id`,
  `client_secret`, `assertion`, `client_assertion`, or
  `client_assertion_type`.

[schema/testdata/token-grants.yaml](../schema/testdata/token-grants.yaml)
declares each grant.

### Refreshing an OAuth method

An `oauth2` method's `tokenEndpoint`, client credential fields, and
`credentialMappings` serve both the authorization code exchange and the
refresh. Two optional fields shape both requests:

- `tokenEndpointAuthMethod` presents the client: `client_secret_post`, the
  default, sends `client_id` and `client_secret` in the body;
  `client_secret_basic` sends them as HTTP Basic credentials. A connection
  without a client secret, which only a method with an optional client secret
  field can have, sends only `client_id`, as a PKCE public client. A connector
  whose connection owner chooses the method declares a
  [value source](#value-sources) instead, such as
  `{fromSetting: tokenEndpointAuthMethod}` naming an `enum` setting whose
  values are the two methods.
- `tokenRequestEncoding` is `form`, the default, or `json` for providers that
  document only JSON token requests.

A refresh posts `grant_type=refresh_token` and the stored refresh token. The
optional `refresh` block holds the rules of its response:

```yaml
oauth2:
  tokenEndpoint: https://login.example.com/token
  tokenEndpointAuthMethod: client_secret_basic
  tokenRequestEncoding: json
  clientIDCredential: client_id
  clientSecretCredential: client_secret
  scopes: [offline_access, files.read]
  credentialMappings:
    - {credential: access_token, source: access_token}
    - {credential: refresh_token, source: refresh_token}
    - {credential: instance_url, source: instance_url}
  refresh:
    terminalErrorCodes: [invalid_grant, invalid_client]
    missingExpiry: {lifetime: 2h}
    additionalParameters: {scope: offline_access files.read}
```

Without `refresh`, a method that maps `refresh_token` refreshes with
`terminalErrorCodes: [invalid_grant]`, `missingExpiry: keep`, and no
additional parameters.

The scopes of a refresh response follow RFC 6749 section 5.1:

- a response without `scope` (or a `scopes` array) keeps the granted scopes
  unchanged;
- a response that reports scopes that no longer cover the declared `scopes`
  needs reauthorization, as a terminal error code does. Providers that never
  report `offline_access` need not report it.

A method that stores several refresh tokens, such as a bot token and a user
token, lists one grant for each, and the grants refresh independently:

- each grant's response sets the expiry of the tokens it maps;
- a host refreshes a grant when that grant's own token nears expiry, never
  because another grant does, since every needless rotation of a single-use
  refresh token widens the window in which a lost response loses the token;
- a grant whose refresh token is blank is skipped;
- the connection reports the earliest expiry of its tokens.

A grant with `grantedScopes: userScopes` checks the scopes its response reports
against `userScopes` instead of `scopes`:

```yaml
refresh:
  grants:
    - refreshTokenCredential: bot_refresh_token
      credentialMappings:
        - {credential: bot_token, source: access_token}
        - {credential: bot_refresh_token, source: refresh_token}
    - refreshTokenCredential: user_refresh_token
      grantedScopes: userScopes
      credentialMappings:
        - {credential: user_token, source: access_token}
        - {credential: user_refresh_token, source: refresh_token}
```

### Minting with client credentials

An `apiKey` method whose access token comes from the OAuth client credentials
grant declares `clientCredentials`. It posts `grant_type=client_credentials`
and the space-separated `scopes`, and presents the client as
`tokenEndpointAuthMethod` says. `terminalErrorCodes` defaults to
`[invalid_client]`.

```yaml
clientCredentials:
  tokenEndpoint: {template: "https://login.example.com/{tenant_id}/token"}
  clientIDCredential: client_id
  clientSecretCredential: client_secret
  scopes: [https://api.example.com/.default]
  credentialMappings:
    - {credential: access_token, source: access_token}
  terminalErrorCodes: [invalid_client, unauthorized_client]
```

### Minting with a JWT bearer assertion

A `serviceAccount` method declares `serviceAccount.jwtBearer`. The host signs
an RS256 assertion (RFC 7523) with `privateKey`, sets `iss`, `sub` (when
`subject` is declared), `aud`, `iat`, `exp` after `lifetime`, and the
space-separated `scope` claim (when `scopes` is declared), and posts it as
`grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer` to `tokenEndpoint`
without client authentication. `credentialCopies` set credential fields from
other credential fields after each mint. `terminalErrorCodes` defaults to
`[invalid_grant]`.

```yaml
serviceAccount:
  jwtBearer:
    keyCredential: service_account_key
    privateKey: {fromKey: private_key}
    issuer: {fromKey: client_email}
    subject: {fromCredential: delegated_user}
    audience: {fromKey: token_uri}
    tokenEndpoint: {fromKey: token_uri}
    scopes: [https://api.example.com/mail.read]
    lifetime: 1h
    credentialMappings:
      - {credential: access_token, source: access_token}
    credentialCopies:
      - {credential: primary_email, fromCredential: delegated_user}
```

### Host-kept credentials

A host that keeps a shared connection keeps the credential fields that only
its token requests use, and serves the credential to applications without
them, so code that an application runs can never refresh or mint the shared
credential or rotate a single-use refresh token away from the other
applications. The fields follow from the declarations, with no field of their
own:

- an `oauth2` method's `clientSecretCredential`, every `refresh.grants`
  `refreshTokenCredential`, and every field that a credential mapping of the
  method or of a grant fills from a `refresh_token` member, such as
  `refresh_token` or `authed_user.refresh_token`;
- a `clientCredentials` grant's `clientSecretCredential`;
- a `serviceAccount.jwtBearer` grant's `keyCredential` and the field that its
  `privateKey` names with `{fromCredential}`.

A `jwtBearer` `privateKey` is therefore `{fromKey}` or `{fromCredential}` of a
`secretString` field, without `values`, and no `credentialCopies` entry copies
a host-kept field. `schema.HostKeptCredentials` implements the rule, and each
release lists the result as `hostKeptCredentials` for every method under
`authMethods` in `connector-release.json`, which a host reads instead of
deriving the fields again. The host-kept credentials of a method are part of
the [connection contract](versioning-and-releases.md#connection-compatibility).

For a connector whose host keeps some field, `connectorctl generate` adds
`UnmarshalSharedCredential` to the generated `Credentials`. The Connector SDK
decodes a shared entry's credential with it (see
[Shared connections](../sdkgo/projectconfig/README.md#host-kept-secrets)). It
requires every field that `Validate` requires except the host-kept ones, and
the access token that a `clientCredentials` or `jwtBearer` grant mints, because
an application of a shared connection cannot mint it. It accepts host-kept
fields that a broker still sends.

The generated `zz_generated_shared_credential_test.go` checks the connector:

- each method's sample credential decodes without its host-kept fields and with
  them, `Validate` still requires a required host-kept field, and every other
  required field stays required;
- it type-checks every non-test package of the connector module and fails when
  code passes a value that holds `Credentials` to `reflect`, `fmt`, `log`,
  `log/slog`, or an encoding package, or reads a host-kept field of
  `Credentials` anywhere except these three places:
  - the generated codec in `zz_generated_connector.go`;
  - the `CredentialRefreshDriver` methods `Refresh` and `RefreshRequired`;
  - the unexported functions and methods of the connector's root package that
    `Refresh` or `RefreshRequired` use, directly or through each other.

A shared entry never calls either driver method, so with these checks an
application of a shared connection calls its provider with the access token
alone. `go test` of the connector runs the test, and `connectorctl generate
--check` keeps it current.

### Value sources

`tokenEndpointAuthMethod`, `tokenEndpoint` of `clientCredentials`, and every
`jwtBearer` value except the lists name where a host finds the value. They
share one value source type with the OAuth endpoints and `scopes` of
[Endpoints and scopes from connection settings](#endpoints-and-scopes-from-connection-settings).
Every `oauth2` field (the endpoints, `scopes`, `tokenEndpointAuthMethod`, the OIDC
endpoints and the credential derivation endpoints) accepts only a literal or
`{fromSetting}`, without `values`, because those are the forms hosts read; the
other forms below belong to `clientCredentials` and `jwtBearer`. Each is one of:

- a literal string;
- `{fromCredential: <field>}`, a credential field of the method;
- `{fromSetting: <field>}`, a `spec.configuration` field or one of the
  method's `configuration.fields`; for an endpoint, a required `url` field;
- `{fromKey: <member>}`, a top-level string member of the JSON key in
  `keyCredential`;
- `{template: "https://host/path/{name}"}`, whose placeholders each name a
  credential field or setting. The host inserts each value path-escaped, and
  the scheme and host are fixed.

`values` translates the value of `fromCredential`, `fromSetting`, or `fromKey`,
such as a login environment into its server; the key `""` translates a blank
value, and an untranslated value cannot be resolved:

```yaml
audience:
  fromCredential: login_environment
  values: {"": https://login.example.com, production: https://login.example.com, sandbox: https://test.example.com}
```

A `tokenEndpointAuthMethod` source is not a template, and every value it can
resolve to, a literal, a translation, or the values of the `enum` field or
setting it names, is `client_secret_post` or `client_secret_basic`. A literal
or translated token endpoint is an absolute HTTPS URL. A host checks
an endpoint that it reads from a credential, setting, or key before it sends a
secret there. Release metadata writes a literal as a JSON string, every other
source as an object, and `missingExpiry` as `"keep"` or `{"lifetime": "2h"}`.

### Credential refresh agreement

A connector keeps its hand-written `CredentialRefreshDriver` for applications
that refresh in process, so its declarations and its driver must agree.
`connectorctl generate` writes `zz_generated_credential_refresh_test.go` beside
every manifest that declares a grant. The test runs the driver against a stub
token endpoint, through the HTTP client it passes to
`NewCredentialRefreshDriver`, and for every declared grant checks that:

- the driver sends one request to the declared token endpoint, with the
  declared encoding, client authentication, grant parameters, additional
  parameters, and, for a JWT bearer grant, an assertion signed by the stored
  key with the declared claims;
- a successful response changes exactly the credentials that
  `credentialMappings` and `credentialCopies` name, and the expiry follows
  `expires_in`;
- a response without `refresh_token` keeps the stored refresh token;
- an error code is terminal exactly when the grant declares it, among the
  declared codes and the common RFC 6749 and provider codes;
- a response without `expires_in` takes the declared `missingExpiry`
  lifetime, or, for `keep`, never needs reauthorization;
- a connection without its optional client secret sends a public client
  request, or the driver refuses it with a reauthorization error.

It also checks that, for a method that declares no grant, the driver's
`RefreshRequired` is false with no expiry and with a past one, and `Refresh`
sends no token request, so a minting method cannot drop its declaration
unnoticed. `go test` of the connector runs it in pull requests, before
a release, and before a release candidate, and `connectorctl generate --check`
fails when it is stale.

A driver may check more than the declarations, such as the scopes a response
reports or a provider-owned API domain. When such a check rejects the test's
sample values, `testdata/credential-refresh-samples.json` overrides them for
every grant (`"*"`), a method (its ID, or `spec.auth`), or one grant (the test
case name, such as `"spec.auth refresh bot_refresh_token"`):

```json
{"*": {"response": {"api_domain": "https://example.pipedrive.com"}}}
```

A driver whose constructor takes a connection setting that a declaration reads
gets it from `newCredentialRefreshDriverWithSettings(httpClient, settings)`,
which the connector writes in a test file of its external test package.

## Studio setup bundle

A connector with a provider-specific setup experience declares `spec.studio`:

```yaml
studio:
  setup:
    entrypoint: index.html
    hostApiRange: ">=0.2.0 <0.3.0"
    backendCapabilities: [oauth.connection.manage, use.configuration.write, slack.channels-list]
    mockScenarios: [not-configured, connected, revoked]
    icon: icon.svg
  commands:
    - id: listChannels
      capability: slack.channels-list
      request:
        method: GET
        url: https://slack.com/api/conversations.list
        credential: {field: bot_token, scheme: bearer}
        fixedQuery: {exclude_archived: "true", limit: "200"}
        parameters:
          - {name: cursor, location: query, target: cursor}
  units:
    - id: channelPicker
      goName: ChannelPicker
      description: Select one Slack channel.
      backendCapabilities: [slack.channels-list]
      outputs:
        - {name: channelId, goName: ChannelID, type: string}
```

The UI build output must contain the entrypoint and icon. Release automation
packages regular files only, enforces file-count and expanded-size limits, and
creates a deterministic tarball. Connector UI runs in a sandbox iframe and may
request only the listed Host API capabilities. It must never receive or render
credential values.

`studio.commands` is a release-owned allowlist for provider reads used by the
setup bundle. The iframe invokes every entry through the single
`provider.command.execute` Host API command. Dex injects the named secret,
allows only declared path/query parameters, bounds the JSON response, and never
returns credential material. Provider pagination, response projection, and
filtering remain connector UI code; Dex Web does not implement any provider's
resource semantics. Studio commands support HTTPS `GET`, fixed query values,
fixed headers, explicit path or query parameters, and two credential schemes:

- `bearer` sends `Authorization: Bearer <secret>`.
- `header` sends the raw secret in the header that `credential.header` names,
  such as `x-goog-api-key` for Gemini or `x-api-key` for Anthropic. Only this
  scheme declares `header`.

`fixedHeaders` maps non-secret header names to values that Dex sends with
every request, such as a provider API version. Secrets belong only in the
credential field.

```yaml
commands:
  - id: listModels
    capability: anthropic.models-list
    request:
      method: GET
      url: https://api.anthropic.com/v1/models
      credential: {field: api_key, scheme: header, header: x-api-key}
      fixedHeaders: {anthropic-version: "2023-06-01"}
```

`credential.header` and every `fixedHeaders` name must be an RFC 7230 token.
Names compare case-insensitively, and `_` matches `-`, because CGI-style
servers merge the two. Validation rejects these names because the broker, the
HTTP transport, or intermediaries own them:

- `Authorization`. Use the `bearer` scheme.
- `Accept`, which Dex sets.
- `Host`, `Content-Length`, `Transfer-Encoding`, `Connection`, `Keep-Alive`,
  `TE`, `Trailer`, `Upgrade`, and every `Proxy-*` name.
- `Cookie`, `Set-Cookie`, `Origin`, `Referer`, `Forwarded`, and every
  `X-Forwarded-*` name.
- `X-HTTP-Method`, `X-HTTP-Method-Override`, and `X-Method-Override`, which
  could turn the `GET` into another method.

`fixedHeaders` names must be unique under the same comparison and cannot
repeat the credential header. Each value is 1 to 256 printable ASCII characters
(`0x20`-`0x7E`) without leading or trailing spaces. Dex Web rejects a command
whose fixed header value contains the credential value before it sends the
request.

Dex Web `cli-v0.13.8` and earlier ignore `credential.header` and
`fixedHeaders` when they load release metadata. They reject every scheme other
than `bearer` before sending a request. They run a command that declares
`fixedHeaders` without those headers, so the provider may reject it. Connector
UI must handle either failure like any other command error. `ModelPicker` in
`@superdurable/dex-connectors-react` then offers manual model ID entry.

`studio.units` is the release-owned catalog of small UI components. It does
not decide which operations or Triggers display a unit. A Flow composes unit
instances in each generated Step or Trigger binding config and maps named ports
to its own configuration object with JSON Pointers. Unit IDs and port names are
generated as Go constants so Flow code does not repeat release-owned strings.
The public Connector catalog publishes each unit ID and description so the
GitHub Pages directory can display and search available UI units.

The same bundle entrypoint renders either the connection surface or exactly one
configuration unit, as selected by Connector Studio Host API 0.2. Connection
authorization stays connector-wide. Unit values are isolated by Flow type and
Step type for operations, or by Flow type and binding name for Triggers.

A bundle renders markup; Dex Web owns how it looks. Build the UI from
`StudioSurface`, `StudioHeader`, `StudioField`, `StudioButton`, and
`StudioNotice` in `@superdurable/dex-connectors-react`, give any other element
only classes from `connectorStudioClassNames`, and call
`applyConnectorStudioTheme(ready)` for every host ready message. An LLM
connector that uses `mountModelPickerBundle` gets both. The package README
lists the markup each class expects.

The theme call writes the Studio stylesheet the host sends in the ready
message and then applies the validated `themeTokens`, so a Dex Web restyle
reaches released bundles without a connector release. The package is inlined
when the bundle is built, and its compiled copy of the stylesheet is the
fallback: the bundle keeps it under a host that sends no stylesheet or one
that does not style every class the bundle knows. A bundle that hard-codes its
own styles or classes would not follow a Dex Web restyle. With the shared
classes, only new markup, such as a new component or class, needs a connector
release.

The UI links `sdk/react` with a `file:` dependency, and that package has its own
React installed. Set `resolve.dedupe: ["react", "react-dom"]` in the UI's
`vite.config.ts`. Without it, Vite inlines two copies of React, the shared
client's hooks run without a renderer, and the frame stays blank.

`script/studio_bundle_theme_check.py` enforces these rules in CI and
`make check`. It reads the non-test files under `connectors/**/ui/src` and
fails when one of them:

- creates, renders, or reaches into a `<style>` or `<link>` element, or edits
  the shared theme's `<style>` element;
- constructs or edits a stylesheet, or imports or loads a CSS file;
- sets a JSX `style` prop, writes `element.style` or `cssText`, or sets a
  `style` attribute;
- renders a class outside `connectorStudioClassNames` through a JSX
  `className` attribute or a `className` object property, such as
  `createElement` props or a JSX spread; computes a `className` instead of
  writing a string literal; or sets classes through `className`, `classList`,
  or a `class` attribute in the DOM;
- writes raw HTML, for example through `innerHTML`; or
- calls `applyConnectorStudioTheme` anywhere but inside a React effect that
  passes it the host ready message and lists that message as a dependency.

It also fails when `ui/index.html` declares or links a stylesheet, sets an
inline style, or uses a class outside the contract, when no non-test source
file calls `applyConnectorStudioTheme` or `mountModelPickerBundle` imported
from the package or from a local module that re-exports it, or when the Vite
config does not dedupe `react` and `react-dom`. A `<style>` mentioned only in a
comment or in string text does not count on its own.

## Connector mocks

An operation may declare maintained mock outcomes that applications use in
their tests instead of the provider. A manifest with any `mocks` generates a
mock package beside the connector, named after the Go package with a `mock`
suffix, such as `llm/llmmock`. The catalog lists the mocks and the package's
import path as `mocks` and `mockPackage`. Connector maintainers write the
mocks; adding one is a minor change, changing its data a patch, and renaming
or removing one a breaking change.

An operation can declare `mockChecks` to run connector-owned checks on its
generated mock. `requestFunction` names an exported function in the connector
package with signature `func(Input) *sdkgo.Failure`. A non-nil Failure selects
`defect` with its kind and message, and the mock fills empty Provider and
Operation fields. It is a rejected application request, so it does not fail
the test or consume a scripted answer. `answer` names a declared
`branch` and an exported `function` with signature `func(Input, Output) error`.
That function runs only for answers on that branch. An error reports a
scripted-answer mismatch with `t.Errorf` and selects `defect`. Keep both
functions free of provider calls and secret-bearing error text. For example:

```yaml
      mockChecks:
        requestFunction: ValidateGenerateTextRequest
        answer: {branch: generated, function: ValidateGenerateTextAnswer}
```

Operations without `mockChecks` retain the existing generated code and mock
behavior. Use the real connector's provider-independent request and response
checks where available; model and wire-format checks remain with the real
operation. For text generation, `textgen.ValidatePortableRequest` checks the
input, while a connector answer function can return the error from
`textgen.ExtractStructuredAnswer(request, response.Text)`.

The fixture in
[`schema/testdata/mocks.yaml`](../schema/testdata/mocks.yaml) declares one
mock of each shape:

```yaml
      mocks:
        - name: gear
          branch: found
          description: A widget with every field set.
          default: true
          output: {id: w-1, name: Gear, createdAt: "2026-01-02T03:04:05Z", tags: [metal, "007"]}
        - name: missing
          branch: notFound
          description: The provider reports no such widget.
          failure: {kind: NOT_FOUND, message: The widget does not exist.}
        - name: invalidID
          branch: defect
          description: The widget ID is empty.
          failure: {kind: VALIDATION, message: The widget ID is required.}
        - name: throttled
          description: The provider throttles the read once.
          retry: {failure: {kind: RATE_LIMIT, message: Too many requests.}, after: 2s}
```

- A branch mock names a declared `branch` and an `output`, a `failure`, or
  both. A paginated query's branch mock may use `pages` instead of `output`.
- A `retry` mock asks Dex to retry the Step, after `after` when it is set.
- An `uncertain` mock represents a write that may have reached the provider,
  cannot be repeated safely, and cannot be confirmed by read-back. A request
  that never left the process, a 429, or a 503 returns Retry; exhausted retries
  fail the Flow. Only a mutation declaring `uncertain` can mock it.
- A `failure` names a Connector SDK failure kind, such as `NOT_FOUND`, and a
  safe message. The mock fills the provider and operation.
- `default: true` marks the mock that answers every call a test does not
  script. A default selects a branch other than `defect` without a failure.
- Outputs are YAML written as the operation output's JSON. Quote a string
  that YAML would read as a number, a boolean, or a date, such as `"007"`.

A listing query declares `pagination` with the JSON field names of the input's
cursor and the output's next cursor. Its pages chain through that cursor: each
page but the last names a distinct next cursor, and the last ends the listing
with an empty or zero one. The generated mock answers an input without a
cursor with the first page and an input whose cursor equals page `k`'s next
cursor with page `k+1`. When the pages name one-based page numbers, an input
page `n` gets page `n`, so an explicit page `1` also gets the first page:

```yaml
      pagination: {inputField: page, nextField: nextPage}
      mocks:
        - name: threePages
          branch: listed
          description: Three unordered pages with a duplicate across a page boundary and an archived widget.
          default: true
          pages:
            - {widgets: [{id: w-3, name: Spring}, {id: w-1, name: Gear}], nextPage: 2}
            - {widgets: [{id: w-1, name: Gear}, {id: w-9, name: Archived, tags: [archived]}], nextPage: 3}
            - {widgets: [], nextPage: 0}
```

Write defaults that keep a weak application test from passing: a paginated
default serves at least three pages, and when the provider documents no order
its items are unordered, include a duplicate across a page boundary, and
include items the application is expected to filter out.

Once a connector declares a mock, `connectorctl validate` requires the whole
surface: at least one mock for every declared branch of every operation,
including an `uncertain` mock where the branch exists, exactly one default per
query, and at most one per mutation. Every manifest load also checks each
mock's shape, names, failure kinds, retry delays, and page chain. The
generated `zz_generated_mock_test.go` strictly decodes every output into the
connector's Go type and, when the type has a `Validate() error` method, runs
it; `go test` in the connector module runs that test.

`connectorctl mocks scaffold connector.yaml` appends a failure mock for every
optional branch that has none, with a failure kind chosen from the branch ID,
and an `uncertain` mock for a mutation that declares the branch. It edits only
the lines it inserts, prints the happy-path branches whose outputs a
maintainer still writes by hand, and adds nothing on a second run. Review the
scaffolded kinds and messages before committing them.

The generated package exposes `New(t, connectionName)`, `Connection()` for
the connector's Step factories, one scripting accessor per operation, a
constructor per branch and per manifest mock, `<Operation>Retry`, and
`<Operation>Default`. A connector with mocks shows its use in its
`examples/` tree. Scripting and case semantics belong to
[`sdkgo/connectormock`](../sdkgo/README.md#connector-mocks).
