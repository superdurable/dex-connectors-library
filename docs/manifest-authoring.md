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

The generated surface also includes `NewLocalConnection` and a
`ConnectionName` field on every operation factory config. The local helper
decodes every manifest authentication field, including multiple optional
secret fields, without assuming a single-token OAuth response. Keep secret
fields typed as `secretString` so code generation constructs `SecretString`
at the provider boundary.

### Multiple authentication methods

Use `auth.methods` when one named connection can use more than one provider
authentication model. Every method has a stable lower-camel ID, its own fields,
guide, and optional OAuth metadata. `defaultMethod` must identify the method the
setup UI selects first; mark at most one method `recommended`.

```yaml
auth:
  defaultMethod: googleOAuth
  methods:
    - id: googleOAuth
      displayName: Google OAuth
      description: Authorize an individual Google or Workspace account.
      recommended: true
      type: oauth2
      connectionKind: gmail-google-oauth
      fields:
        - {name: access_token, goName: AccessToken, type: secretString, description: Short-lived access token produced by OAuth., required: true}
        - {name: refresh_token, goName: RefreshToken, type: secretString, description: Long-lived refresh token produced by OAuth., required: true}
      guide:
        startURL: https://console.cloud.google.com/apis/credentials
        steps: [Create a Web application and add the Redirect URI shown by Dex Web.]
      oauth2:
        authorizationEndpoint: https://accounts.google.com/o/oauth2/v2/auth
        tokenEndpoint: https://oauth2.googleapis.com/token
        scopes: [openid]
        pkce: true
    - id: workspaceServiceAccount
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

Generated credentials include `AuthMethodID` plus the union of method fields.
Only the selected method's required fields are validated. Local configuration
stores the selection as `auth_method`; hosted runtimes keep the selection in
the immutable non-secret revision and resolve secrets through the broker.

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
Connector `go.mod` files
must pin an already-published SDK version and cannot contain `replace`,
pseudo-version, branch, or commit dependencies. A required connector module
must be pinned at a released, complete tag, and no other module from this
repository may be required; see
[Connector dependencies](versioning-and-releases.md#connector-dependencies).

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
