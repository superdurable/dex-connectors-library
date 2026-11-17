# OAuth 2.0 HTTP TypeScript connector

`@superdurable/connector-superdurable-oauth2-http` supplies `sendRequest` and
`postNonIdempotent` for Dex TypeScript Flows. `connector.yaml` generates its
models, Step factories, mock, private and shared credential decoders, and OAuth
refresh declaration. The package uses Connector SDK 0.12.0.

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
`uncertain`, and `defect` are optional and fail the Flow when unwired.
`sendRequest` is repeat-safe: every method retries a lost answer, a 5xx, and
any status other than a redirect or a definite refusal (400, 401, 403, 404,
411, 413, 414, 415, or 422), and it never selects `uncertain`.

`newPostNonIdempotentStep` sends one POST per Step execution under the
Connector SDK single-dispatch fence with sync durability. A 429, a 503, a 401
after a renewed token, and a POST that never left the process are retried; any
other outcome that may follow a processed POST selects `uncertain` and is never
sent again. Every request uses a bounded response body, and bearer credentials
echoed by the provider are redacted from its output.

Run `npm test`, `npm run typecheck`, and `npm run lint` from this directory.
The generated OAuth agreement suite and Go-recorded provider replay run in
`npm test`. The local Dex integration test uses a stand-in API on port 40912
and requires `DEX_FLOW_SERVICE_ADDRESS` set to a running Dex development
server. Live provider authorization for the TypeScript port has not been
verified.

One operation has a 25-second budget shared by token resolution and every provider request. The default transport parses request URLs before sending them, so dot segments such as `/a/./b` and percent-encoded dot segments such as `/a/%2e/b` reach the server as `/a/b`.
