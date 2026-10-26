# OAuth 2.0 HTTP API request example

> **Host support.** Steps 3 and 4 need a Dex Web that accepts the `fromSetting`
> endpoints in this connector's release metadata. Dex Web `cli-v1.6.4` rejects
> that metadata, so the authorization steps are not verified; the repository
> tests run the Flow against a real Dex Server and a local authorization
> server instead. See the connector README's
> [host support](../../README.md#architecture) note.

This operation-only example reads one path of an OAuth 2.0 protected API and
records the answer:

1. `SendRequest` sends a `GET` for the requested path with the connection's
   access token, refreshing the token first when it is about to expire;
2. `recordResponse` stores a 2xx answer and completes the Flow.

Application Steps store the report in the `oauth2-http-request-report`
Attribute, because a Connector branch target receives only the current
operation Result. The summary and display RPCs read that Attribute for Dex
Web. The example wires only `completed`. An API rejection or local `defect`, such
as a path without a leading slash, fails the Flow with the selected branch.
A throttled request is retried after the API's `Retry-After`.

The Flow sets its type to `OAuth2HTTPRequest` and names each application Step
explicitly. Dex Web **Start Flow** starts the Flow and Step types written in the
Flow Definition Graph, which omit the Go package, while the Go SDK otherwise
registers package-qualified defaults. The start Step also keeps a `WaitFor`
that returns at once, because Dex Web Start Flow in dexcli v0.13.8 invokes
`WaitFor` on the start Step and an execute-only Step rejects it.

## Release baseline

- OAuth 2.0 HTTP Connector `v0.1.0`
- dexcli `v0.13.8` or a later stable release

## 1. Prepare a clean local test project

Dex Web configures a connection only for an exact released Connector module.
Create a separate project that consumes the release, then copy the Flow source
so dexcli analyzes it as an application dependency:

```bash
mkdir oauth2-http-api-request-e2e
cd oauth2-http-api-request-e2e
mkdir -p flow build

curl -fsSL \
  https://raw.githubusercontent.com/superdurable/dex-connectors-library/refs/tags/connectors/superdurable/oauth2-http/v0.1.0/connectors/superdurable/oauth2-http/examples/api-request/flow/workflow.go \
  -o flow/workflow.go

go mod init example.com/oauth2-http-api-request-e2e
go mod edit -go=1.24.0
go get github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http@v0.1.0
go mod tidy
```

`go mod edit -go=1.24.0` keeps the project's Go version at or below the Go
release that built dexcli. Otherwise dexcli reports `go_type_check_failed`.

Generate the Flow Definition Graph used by Dex Web:

```bash
dexcli visualize ./flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/oauth2-http-api-request
```

The command must finish without blocking diagnostics and create
`build/oauth2-http-api-request.json` with `"valid": true`. The repository's
compatibility gate runs the same analysis twice from a clean consumer module
and requires identical output.

## 2. Start Dex

```bash
dexcli dev \
  --flow-rendering-dir "$PWD/build"
```

Record the Dex Web URL and Dex Server address that dexcli prints. With the
default ports they are `http://127.0.0.1:8802` and `127.0.0.1:8801`.

## 3. Create an OAuth client

Follow
[authorization server setup](../../README.md#authorization-server-setup) to
create a confidential client at your authorization server. The Redirect URI is
the Dex Web URL followed by `/api/v2/connector-oauth/callback`, such as
`http://127.0.0.1:8802/api/v2/connector-oauth/callback`.

## 4. Configure the connection in Dex Web

Open **Connections** and select `oauth2-http / oauth2-http-api`. Enter the
authorization endpoint, token endpoint, scopes, and API base URL from the
server's discovery document, then the OAuth client ID and secret, and choose
**Authorize**. After the server returns to Dex Web, the connection status
should be **Ready**.

## 5. Run the Worker

The Worker reads the `DEX_PROJECT_*` project configuration environment
described in [project configuration](../../../../../sdkgo/projectconfig/README.md);
Dex Web or Superverse Studio writes that configuration when you save the
connection.

```bash
export DEX_FLOW_SERVICE_ADDRESS="127.0.0.1:8801"

GOWORK=off go run \
  github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http/examples/api-request@v0.1.0
```

The Worker listens on `127.0.0.1:8884` by default. Set
`DEX_WORKER_BIND_ADDRESS` to use another address, and `DEX_BLOB_CACHE_DIR` to
move its blob cache. If Dex is unreachable, the Worker logs
`dex server unavailable; retrying` with a delay that grows to 30 seconds and
starts once Dex answers.

## 6. Start the Flow

In Dex Web, open the `OAuth2HTTPRequest` Flow, choose **Start Flow**, select
the Worker address, and enter a path under the connection's API base URL. For a
Keycloak realm whose base URL is its origin, the UserInfo endpoint is:

```json
{
  "path": "/realms/app/protocol/openid-connect/userinfo"
}
```

Open the run and follow it to **Completed**. The display shows the status code
and body. A path that does not exist fails the Flow on the unwired `rejected` branch.

## Verify from the repository

The example tests run from the Connector directory:

```bash
GOWORK=off go test -race ./examples/...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  GOWORK=off go test -tags=integration ./examples/api-request/flow/...
```

The integration tests use a real Dex Server and a local plain-http
authorization server. They verify a completed request after a token refresh
and a duplicate start with the same request ID, a 404 that fails on the unwired `rejected` branch, a
throttled request that Dex retries after the server's `Retry-After` delay, and
a Flow failure for an unwired `defect` branch. A mock test runs the same Flow on
an `oauth2httpmock` connection, as an application test does.

Before `v0.1.0` is published, build a local release artifact with
`go run ./cmd/connectorctl release-artifact` from the repository root and pass
its directory to `dexcli dev --connector-release-override oauth2-http=DIRECTORY`.
The test project must still resolve the connector `v0.1.0` module without a
`replace`, as the compatibility gate does with a local module proxy.
