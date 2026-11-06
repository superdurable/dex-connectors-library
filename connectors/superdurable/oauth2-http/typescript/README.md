# OAuth 2.0 HTTP TypeScript connector

`@superdurable/connector-superdurable-oauth2-http` supplies `sendRequest` for Dex
TypeScript Flows. `connector.yaml` generates its models, Step factory, mock,
private and shared credential decoders, and OAuth refresh declaration. The
package uses Connector SDK 0.7.0.

Configure a Dex Web Connection with the authorization and token endpoints,
scopes, and API base URL, then authorize it with the provider. The connector
resolves credentials through the project adapter and refreshes expired tokens
before sending a request. On a 401 it asks the source to renew once. The
access token goes only to the configured API origin, and the transport never
follows redirects.

At Worker startup, call `loadFromEnvironment()` and
`newProjectConnection(project, ConnectionName)` as in the
[runnable example](examples/api-request/main.ts). Its static connection name
is `oauth2-http-api`. The example keeps credentials out of Flow input and
persists no token. `requestTimeout` from the connection is converted from a
Go duration to milliseconds; blank uses the manifest default.

`newSendRequestStep` routes `completed` to the required destination. `rejected`,
`uncertain`, and `defect` are optional and fail the Flow when unwired. GET,
HEAD, and OPTIONS retry transport failures and 5xx responses. For other methods,
an outcome that may have reached the API is uncertain unless the API returns
408 or 429. Every request uses a bounded response body, and bearer credentials
echoed by the provider are redacted from its output.

Run `npm test`, `npm run typecheck`, and `npm run lint` from this directory.
The generated OAuth agreement suite and Go-recorded provider replay run in
`npm test`. The local Dex integration test uses a stand-in API on port 40912
and requires `DEX_FLOW_SERVICE_ADDRESS` set to a running Dex development
server. Live provider authorization for the TypeScript port has not been
verified.
