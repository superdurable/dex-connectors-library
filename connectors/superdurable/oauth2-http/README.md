# OAuth 2.0 HTTP Connector

The TypeScript package `@superdurable/connector-superdurable-oauth2-http` implements
`sendRequest` and `postNonIdempotent` with generated OAuth refresh and shared
credential decoding.
See its [runnable TypeScript example](typescript/examples/api-request/) and
[package guide](typescript/README.md). TypeScript replay matches the Go
provider scenarios, and the example runs through a real local Dex stack.
Live TypeScript authorization with a hosted provider remains unverified.

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http`.

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
- the `sendRequest` Mutation sends one repeat-safe HTTP request with the access
  token as a Bearer credential and returns the status code and body;
- the `postNonIdempotent` Mutation sends one HTTP POST that must never be sent
  twice, at most once per Step execution.

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
  replayed to another host. The 3xx answer selects `rejected` for
  `sendRequest` and `uncertain` for `postNonIdempotent`, because a redirect
  after a POST may follow a processed write.
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
| `rejected` (optional) | A redirect, a definite refusal (400, 401, 403, 404, 411, 413, 414, 415, or 422), or an authorization the server will no longer renew. `statusCode` is zero in the last case. A 401 is final only after one renewal and one resend. |
| `uncertain` (optional) | Never selected. `sendRequest` repeats every unknown outcome; the branch stays declared so applications that wired it keep compiling. |
| `defect` (optional) | The input or the settings are invalid, so no request was sent. |

`sendRequest` is `repeatOnUnknown: safe`: every unknown outcome is sent again,
for every method. Dex retries, after `Retry-After` when the API sends it, a
request that never left the Worker, a lost or cut-off answer, a timeout, any
5xx, and any status that does not prove a refusal, such as 408, 409, 410, 425,
or 429. The retry policy allows five attempts within two minutes, after which
the Flow fails. Use `sendRequest` only for requests that are safe to repeat:
reads, absolute updates, deletes, or writes the API deduplicates. The call's
idempotency key is the same for every retry of one Step execution, so name the
API's deduplication header with `idempotencyKeyHeader`. Send a POST that must
never be repeated with `postNonIdempotent`.

An unwired optional branch fails the Flow when it is selected. This step from
[`examples/api-request`](go/examples/api-request/flow/workflow.go) wires only
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

## The postNonIdempotent Mutation

`postNonIdempotent` sends one HTTP POST that must never be sent twice, such as a
payment or an order the API does not deduplicate. Its input has the `path`,
`url`, `query`, `headers`, and `body` fields of `sendRequest`, with the same
rules; the method is always `POST` and no idempotency header is sent. The output
is the same as `sendRequest`.

The operation is `repeatOnUnknown: unsafe` and runs with sync durability under
the Connector SDK's single-dispatch fence: the Step records a claim heartbeat
before the POST and sends at most once per Step execution.

| Branch | When |
| --- | --- |
| `completed` | The API answered 2xx. |
| `rejected` (optional) | The API refused the only POST of this Step execution with a definite refusal (400, 401 after a failed renewal, 403, 404, 411, 413, 414, 415, or 422), or the authorization could not be renewed before the POST was sent. |
| `uncertain` (optional) | The POST may have reached the API without a usable answer: a lost or cut-off answer, a timeout, a redirect, a 5xx other than 503, or another status that does not prove a refusal, such as 408 or 409. An attempt after an earlier possible send also selects it without sending. |
| `defect` (optional) | The input or the settings are invalid, so no request was sent. |

Dex retries a 429 or 503, after `Retry-After` when the API sends it, a POST that
never left the Worker, and a 401 after which the token was renewed; those
answers prove the API did not process the POST, so the next attempt sends it
again. A checkpoint that cannot be read also returns Retry without sending. The
connector cannot read a POST back, so a Flow that selects `uncertain` checks
the API's state before it acts.

## Mocks

`oauth2httpmock` scripts the operations in application tests. The `sendRequest`
default, `userProfile`, answers a call the test does not script with a 200 JSON
profile, and the `postNonIdempotent` default, `createdOrder`, with a 201 order.
The package also has a constructor for each branch and for the manifest mocks
`notFound`, `authorizationExpired`, `connectionLost`, `invalidPath`, `throttled`,
and `invalidOrder`. See
[`workflow_mock_integration_test.go`](go/examples/api-request/flow/workflow_mock_integration_test.go).

## Example

[`examples/api-request`](go/examples/api-request/README.md) is a Worker and a Flow
that read one path of the protected API, or send an order once with
`postNonIdempotent` when the input sets `orderBody`, and record the answer.

The [Python package](python/README.md) exposes the same `sendRequest` and
`postNonIdempotent` Mutations through typed Step factories. Its [API request example](python/examples/api_request/README.md)
uses the static `oauth2-http-api` connection and has a real Dex Worker and Client
integration test. The package uses Connector SDK 0.12.0 manifest-generated OAuth
refresh, the SDK's single-dispatch fence, and Go-recorded provider conformance
scenarios.

## Verification

The module requires `sdkgo v0.38.0`. Run the checks standalone, as CI does:

```bash
cd connectors/superdurable/oauth2-http/go
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

The real Dex tests in the example need a running `dexcli dev`:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 go test -tags=integration ./examples/api-request/... -count=1 -v
```

The Keycloak tests need Docker and the `live` build tag. They start
`quay.io/keycloak/keycloak:26.8.0` (set `KEYCLOAK_IMAGE` for another image),
import [`testdata/keycloak/realm.json`](go/testdata/keycloak/realm.json) with
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
