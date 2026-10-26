# OAuth 2.0 HTTP Connector

> **Verification status: partial live.** The `sendRequest` operation and token
> refresh ran against a local Keycloak 26.8 with 60-second access tokens and
> single-use refresh tokens, and the example ran on a real Dex stack against
> local fakes. No hosted identity provider (Okta, Auth0, Entra ID, Google) and
> no Dex Web authorization was used. See [verification status](../../../docs/verification-status.md)
> for what is and is not verified.

The generic OAuth 2.0 HTTP connector calls any HTTP API that an OAuth 2.0
authorization server protects. It covers servers that have no dedicated
connector, such as a Keycloak realm or a company's internal identity provider:

- the connection settings name the server's authorization and token endpoints,
  the scopes, and the API's base URL;
- the credential holds the OAuth client ID, the client secret, and the tokens;
- the `sendRequest` Mutation sends one HTTP request with the access token as a
  Bearer credential and returns the status code and body.

## Architecture

A host authorizes the connection with the authorization code flow and PKCE. It
reads the endpoints and scopes from the connection settings because the
manifest declares them with `fromSetting` instead of constants:

```yaml
oauth2:
  authorizationEndpoint: {fromSetting: authorizationEndpoint}
  tokenEndpoint: {fromSetting: tokenEndpoint}
  scopes: {fromSetting: scopes}
```

The library does not decide whether those settings must be HTTPS: only the
connection owner knows where the server runs, and the host decides its own
policy. See
[endpoints and scopes from connection settings](../../../docs/manifest-authoring.md#endpoints-and-scopes-from-connection-settings).

> **Host support.** A host must accept the `fromSetting` form in the release
> metadata and resolve it from the saved connection settings when it builds the
> authorization URL, redeems the code, and refreshes. Dex Web `cli-v1.6.4`
> reads `authorizationEndpoint` as a string and rejects this connector's
> release metadata, so Dex Web cannot authorize this connector until it
> supports `fromSetting` endpoints and scopes. The Keycloak test below
> performs the code exchange itself, as a host would.

The connector then keeps the access token fresh:

- `CredentialRefreshDriver` refreshes the token before it expires, using the
  refresh-token grant at the token endpoint from the settings. It sends the
  client ID and secret in the request body, or as an HTTP Basic header when
  `tokenEndpointAuthMethod` is `client_secret_basic`.
- A server that invalidates a refresh token the moment it is used is
  supported. Every refresh returns the replacement pair, the credential
  provider persists it before the next call, and a refresh token the server no
  longer accepts means the connection must be authorized again.
- After a 401 the operation asks the credential provider to renew the token
  once and sends the request again. The provider renews only when the stored
  expiry has elapsed, because a 401 alone does not prove that the token
  expired.

Before every call the credential provider asks the driver whether the stored
token is due, and the SDK's refresh timing decides, so the tests make each
refresh certain by moving the clock past the stored expiry.

## Security boundary

- **Client secret.** It is the secret credential field `oauth_client_secret`,
  never a setting. Settings are plain configuration; credentials are resolved
  per call and never enter Flow input, Attributes, Results, logs, or errors.
- **Token destination.** The access token goes only to the origin (scheme,
  host, and port) of `apiBaseUrl`. A request `url` must have that origin, and a
  `path` is joined to `apiBaseUrl`; anything else selects `defect` before a
  request is sent.
- **No redirects.** The connector never follows a redirect, so a token is never
  replayed to another host. The 3xx answer selects `rejected`.
- **Connection-owned headers.** A request that sets `Authorization`, `Cookie`,
  `Host`, or another header that the transport owns selects `defect`.
- **Redaction.** The access tokens a request used are replaced by `[REDACTED]`
  in the returned body if the API echoes them.
- **Bounded reads.** A response is cut at `maxBodyBytes` and marked truncated.
- **Plain http and the dialer.** The three URLs the connection owner enters may
  use `http://`. The connector's client is `providerhttp.PrivateNetworkHTTP`
  from the Connector SDK: it resolves each host once, checks every resolved
  address, and connects only to an address it checked:
  - `http` connects only when every resolved address is loopback or private
    (`127.0.0.0/8`, `::1`, `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`,
    `fc00::/7`);
  - any other `http` host, including one with a single public address, is
    refused and selects `defect`; use `https`;
  - link-local addresses (`169.254.0.0/16` and `fe80::/10`, which include the
    cloud metadata service), the IPv6 metadata address `fd00:ec2::254`, and
    unspecified or multicast addresses are never allowed, even over `https`.

  The connector uses its own transport, so `WithHTTPClient` accepts a client
  whose transport is nil or an `*http.Transport` and ignores its proxy settings;
  another transport makes `New` fail. Over a private network the authorization
  code, the client secret, and the tokens still travel unencrypted.

## Authorization server setup

Create a confidential OAuth client at the authorization server, then enter its
endpoints in the connection settings:

1. Create a client for the authorization code flow with PKCE and turn client
   authentication on, so the server issues a client secret. In a Keycloak realm
   that is **Clients** > **Create client** with **Client authentication** on and
   **Standard flow** enabled; under **Advanced** set **Proof Key for Code
   Exchange Code Challenge Method** to `S256`.
2. Add the exact Redirect URI that the host displays when you configure the
   connection to the client's valid redirect URIs.
3. Copy the endpoints from the server's discovery document at
   `<issuer>/.well-known/openid-configuration`. A Keycloak realm serves it at
   `https://HOST/realms/REALM/.well-known/openid-configuration`; the keys are
   `authorization_endpoint` and `token_endpoint`.
4. Copy the client ID and the client secret from the client's **Credentials**
   tab into the credential fields.

A server that rotates refresh tokens needs no extra setting. To make the
connection last, let the realm keep sessions longer than the longest idle time
between calls: a refresh token that expired or was already used requires a new
authorization.

## Connection settings and credentials

A Keycloak realm on a private network, as the settings of a connection:

```json
{
  "authorizationEndpoint": "http://keycloak.internal:8080/realms/app/protocol/openid-connect/auth",
  "tokenEndpoint": "http://keycloak.internal:8080/realms/app/protocol/openid-connect/token",
  "scopes": ["openid", "profile"],
  "apiBaseUrl": "http://keycloak.internal:8080"
}
```

| Setting | Meaning |
| --- | --- |
| `authorizationEndpoint` | Required. Where Dex Web sends the user to approve access. |
| `tokenEndpoint` | Required. Where the code is exchanged and the token refreshed. |
| `scopes` | Required. Scopes to request, one per entry. |
| `tokenEndpointAuthMethod` | `client_secret_post` (default) or `client_secret_basic`: how a refresh presents the client. The manifest cannot yet declare this as the source for a host's code exchange. |
| `apiBaseUrl` | Required. Base URL of the protected API; the only origin that receives the token. |
| `maxBodyBytes` | Largest request and response body. Blank uses 1 MiB. |
| `requestTimeout` | Longest single HTTP request. Blank uses 12 seconds. |

| Credential | Meaning |
| --- | --- |
| `oauth_client_id` | The OAuth client's ID. |
| `oauth_client_secret` | Secret. The OAuth client's secret. |
| `access_token` | Secret. Produced by the authorization and refreshed by the connector. |
| `refresh_token` | Secret. Optional: a server that issues none needs a new authorization when the access token expires. |

Settings are read when the application starts, so restart it after a change; a
refreshed credential applies to the next call.

## The sendRequest Mutation

`sendRequest` sends one HTTP request. The input is:

| Field | Meaning |
| --- | --- |
| `method` | `GET` (default), `HEAD`, `OPTIONS`, `POST`, `PUT`, `PATCH`, or `DELETE`. |
| `path` | Path under `apiBaseUrl`, starting with one `/`. No query, fragment, backslash, or `..` segment. |
| `url` | An absolute URL on the origin of `apiBaseUrl`, such as a next-page link. Set exactly one of `path` and `url`. |
| `query` | Query parameters, merged with those in `url`. A name may have several values. |
| `headers` | Extra headers, such as `Accept`. |
| `body` | Request body as text; not allowed with `GET` and `HEAD`. `Content-Type` defaults to `application/json`. |
| `idempotencyKeyHeader` | A header name, such as `Idempotency-Key`, that carries the Step's idempotency key. |

The output holds `statusCode`, `contentType`, `body`, `bodyIsBase64` for a body
that is not valid UTF-8, and `bodyTruncated`.

Branches, in the order the operation decides them:

| Branch | When |
| --- | --- |
| `completed` | The API answered 2xx. |
| `rejected` (optional) | A redirect, a 4xx other than 408 and 429, or an authorization the server will no longer renew. `statusCode` is zero in the last case. A 401 is final only after one renewal and one resend. |
| `uncertain` (optional) | `POST`, `PUT`, `PATCH`, or `DELETE` that was written but got no usable answer: the connection closed, timed out, was cut off, or the API answered 5xx. |
| `defect` (optional) | The input or the settings are invalid, so no request was sent. |

Dex retries, after `Retry-After` when the API sends it, a 408 or 429, a request
that never left the Worker, and, for `GET`, `HEAD`, and `OPTIONS`, any
transport failure or 5xx answer. An unsafe method is never repeated after it
may have reached the API; a Flow that selects `uncertain` reads the API's state
before it writes again. The call's idempotency key is the same for every retry
of one Step execution, so an API that deduplicates by key can make a retry
safe: name the header with `idempotencyKeyHeader`.

An unwired optional branch fails the Flow when it is selected. This step from
[`examples/api-request`](examples/api-request/flow/workflow.go) wires only
`completed`; a 404 fails the Flow on the unwired `rejected` branch:

```go
		dex.DefineStep(oauth2http.NewSendRequestStep(oauth2http.SendRequestStepConfig[Input]{
			StepType: sendRequestStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "oauth2-http", GroupLabel: "OAuth 2.0 HTTP", Explanation: "Send a GET request to the connection's API with the OAuth access token."},
			Connection:  flow.connection,
			MapToOperationInput: func(input Input) oauth2http.SendRequestInput {
				return oauth2http.SendRequestInput{Method: "GET", Path: input.Path}
			},
			Completed: sdkgo.GoTo(recordResponse{}),
		})),
```

## Mocks

`oauth2httpmock` scripts the operation in application tests. The default,
`userProfile`, answers a call the test does not script with a 200 JSON profile.
The package also has a constructor for each branch and for the manifest mocks
`notFound`, `authorizationExpired`, `connectionLost`, `invalidPath`, and
`throttled`. See
[`workflow_mock_integration_test.go`](examples/api-request/flow/workflow_mock_integration_test.go).

## Example

[`examples/api-request`](examples/api-request/README.md) is a Worker and a Flow
that read one path of the protected API and record the answer.

## Verification

The module requires `sdkgo v0.25.0`, which adds
`oauthtoken.TokenEndpoint.AllowsPlainHTTP` for plain-http token endpoints. Until
that release is published, run the checks in the repository workspace:

```bash
make workspace
cd connectors/superdurable/oauth2-http
go test -race ./...
go vet ./...
```

After the release, run them standalone, as CI does, with `GOWORK=off`.

The real Dex tests in the example need a running `dexcli dev`:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 go test -tags=integration ./examples/api-request/... -count=1 -v
```

The Keycloak tests need Docker and the `live` build tag. They start
`quay.io/keycloak/keycloak:26.8.0` (set `KEYCLOAK_IMAGE` for another image),
import [`testdata/keycloak/realm.json`](testdata/keycloak/realm.json) with
random test passwords, and remove the container when they finish. They sign in
with the authorization code flow and PKCE, call UserInfo through `sendRequest`
over plain http, check that a replayed refresh token requires reauthorization,
and wait past the 60-second lifetime once, so the run takes about 90 seconds:

```bash
go test -tags=live -run Keycloak -count=1 -v -timeout 10m .
```

Not covered: Dex Web's own authorization callback and its reading of the
`fromSetting` endpoints (the Keycloak test performs the code exchange itself,
as a host would), a hosted identity provider, `client_secret_basic` against a
real server (the unit tests cover the request shape), a server that issues no
refresh token, and the credential provider that Dex Web uses (the tests use an
in-memory provider with the same refresh rules).
