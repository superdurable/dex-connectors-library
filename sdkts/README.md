# Connector TypeScript SDK

`@superdurable/connectors-sdk` is the provider-neutral TypeScript SDK for Dex connectors. It provides stable value contracts, typed Query and Mutation operations, credential refresh, OAuth token exchanges, HTTP safety helpers, project configuration loading, scripted mocks, and single-dispatch marker decisions. Dex Step factories and the live single-dispatch fence are being completed against the Dex SDK's public Context deadline and instance-addressed Step APIs. The package is at version `0.1.0` and requires Node 22 or 24.

Install dependencies and verify the package from this directory:

```sh
npm ci
npm run check
node examples/core.mjs
node examples/operations.mjs
node examples/providerhttp.mjs
node examples/projectconfig.mjs
node examples/connectormock.mjs
node examples/oauthtoken.mjs
node examples/singledispatch.mjs
```

All durations in this package are milliseconds. Public JSON shapes use the Go SDK's field names. Credential values and project objects stay outside Flow state and cannot be serialized through their wrappers.

## Typed operations and credential resolution

The [runnable operation example](examples/operations.mjs) validates a Query definition, binds a live Step context to stable call identity, resolves credentials at invocation time, and completes a Go-compatible zero receipt. `Mutation` adds an idempotency-key method and may return `newMutationUncertain` only for a dispatched write that cannot be repeated or confirmed by read-back. `runQuery` and `runMutation` throw `RetryError` for a safe retry; a Step factory turns that into Dex retry behavior. The example uses a test context; application Steps receive `AsyncContext` from Dex.

```js
import { createCall, newQueryBranch, runQuery, StaticCredentialProvider, validateQueryDefinition, zeroReceipt } from '../dist/index.js';

const definition = {
  operation: { connectorId: 'sample', operationId: 'getItem' },
  branches: [{ id: 'found', description: 'Item was found' }, { id: 'defect', description: 'Connector defect', optional: true }],
  stepDefaults: {},
};
validateQueryDefinition(definition);
const context = { flowId: 'example-flow', stepExecutionId: 'getItem:1', attempt: 1 }; // Test context; a real Step receives Dex AsyncContext.
const call = createCall(context, { provider: 'sample', name: 'default' }, definition.operation);
const credentials = new StaticCredentialProvider([[call.connection, { apiKey: 'test-only' }]]);
```

`Call.context` is the live Dex `AsyncContext` object attached as a non-enumerable field by `createCall`; do not put it in Flow input or persisted state. Hosts with coordinated refresh implement `RefreshingCredentialProvider` and optionally `RejectedCredentialRefreshingProvider`. `resolveCredential` selects refresh support when present; `resolveCredentialAfterRejection` forces one coordinated refresh after an explicit authentication rejection.

## Core contracts, errors, and credentials

The [complete runnable core example](examples/core.mjs) validates a stable call ID and receipt, constructs a branch result, selects the default one-hour retry policy, and reveals a secret only at the provider boundary. `newMutationRetry` requests another safe attempt; `newMutationUncertain` is reserved for a dispatched write that cannot be repeated or read back. A definite business result selects a branch.

```js
import { CallIDSchema, ReceiptSchema, newQueryBranch, defaultConnectorStepExecuteRetryPolicy, SecretString } from '../dist/index.js';

const callId = CallIDSchema.parse('6750e62f-4d15-4e7e-a9d3-ec90f4f17b35');
const receipt = ReceiptSchema.parse({ callId, provider: 'fixture', observedAt: new Date().toISOString() });
const result = newQueryBranch('found', { title: 'Example' }, receipt);
const retryPolicy = defaultConnectorStepExecuteRetryPolicy();
const credential = new SecretString('test-only');
if (result.kind !== 'branch' || retryPolicy.totalDurationMs !== 3600000 || credential.reveal() !== 'test-only') throw new Error('core example failed');
```

## Provider HTTP

The [complete runnable HTTP example](examples/providerhttp.mjs) sends one bounded request to a local server. `sendProviderRequest` resolves every address before connecting, pins the chosen address, and returns redirects without following them. `ProviderTransportError.isProvablyNotSent` is true only when no socket was assigned. A failure after socket assignment has an unknown dispatch outcome.
`BodyTooLargeError` preserves the response `status` and `headers`, so a caller can classify a 429 or 503 and its `Retry-After` even when the response body exceeds its limit.

```js
import { createServer } from 'node:http';
import { parseRetryAfter, sendProviderRequest } from '../dist/providerhttp/index.js';

const server = createServer((_request, response) => {
  response.writeHead(429, { 'Retry-After': '7', 'Content-Type': 'application/json' });
  response.end('{"error":{"code":"RATE_LIMIT"}}');
});
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
try {
  const address = server.address();
  const response = await sendProviderRequest({
    url: `http://127.0.0.1:${address.port}/`, method: 'GET', maxResponseBytes: 1024,
    addressPolicy: 'privateNetworkHTTP',
  });
  if (response.status !== 429 || parseRetryAfter(response.headers['retry-after']) !== 7000) throw new Error('HTTP example failed');
} finally {
  await new Promise(resolve => server.close(resolve));
}
```

## Project configuration

The [complete runnable local configuration example](examples/projectconfig.mjs) loads Dex Web's mode-0600 `connections.json` and resolves credentials only at use time. Hosted use selects `DEX_PROJECT_*` environment settings, validates an exact snapshot digest, and uses a versioned S3 bucket with conditional writes. `encodeGoCompatibleJSON` produces Go-compatible storage bytes with sorted map keys and HTML-safe strings. Shared entries resolve through the credential broker; reauthorization and revocation terminate credential use, and expired local or hosted credentials require refresh.

```js
import { mkdtemp, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { loadFromEnvironment } from '../dist/projectconfig/index.js';

const directory = await mkdtemp(join(tmpdir(), 'sdkts-example-'));
const path = join(directory, 'connections.json');
try {
  await writeFile(path, JSON.stringify({
    schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1',
    connections: [{ connectorId: 'example', connectionName: 'default', modulePath: 'example',
      provider: 'example', configuration: {}, credentials: { apiKey: 'test-only' } }],
  }), { mode: 0o600 });
  const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
  const credential = await project.resolveCredential('example', 'default');
  if (credential.apiKey !== 'test-only') throw new Error('project configuration example failed');
} finally {
  await rm(directory, { recursive: true, force: true });
}
```

## Connector mocks

The [complete runnable mock example](examples/connectormock.mjs) validates a JSON fixture, builds a scripted mutation from the real operation definition, and invokes it through the same result validator as a provider operation. `retryCase` and `uncertainCase` model the other provider outcomes; `decodeCallReport` reads the Go-compatible test harness report. Scripts support `respond`, `when`, `default`, `strict`, and `forFlow`. Mutation terminal outcomes replay by stable idempotency key; retries consume the next case. After a real Dex Flow completes, call `assertNoFailures()` to surface unscripted calls or invalid cases.

```js
import { z } from 'zod';
import { branchCase, decodeOutput, newMutation } from '../dist/connectormock/index.js';
import { createCall, runMutation } from '../dist/index.js';

const output = decodeOutput('{"id":"item-1"}', z.strictObject({ id: z.string() }));
const response = branchCase('created', output);
if (response.kind !== 'branch' || response.value.id !== 'item-1') throw new Error('mock example failed');

const definition = { operation: { connectorId: 'sample', operationId: 'createItem' }, branches: [{ id: 'created', description: 'Created' }, { id: 'defect', description: 'Connector defect', optional: true }], stepDefaults: {} };
const mutation = newMutation(definition).respond(branchCase('created', output));
const context = { flowId: 'example-flow', stepExecutionId: 'createItem:1', attempt: 1 }; // Test context; a real Step receives Dex AsyncContext.
const call = createCall(context, { provider: 'sample', name: 'default' }, definition.operation);
const result = await runMutation(mutation, call, { id: 'item-1' });
if (result.branch !== 'created' || mutation.calls().length !== 1) throw new Error('scripted mock example failed');
mutation.assertNoFailures();
```

## OAuth token refresh

The [runnable OAuth exchange example](examples/oauthtoken.mjs) shows a provider-neutral refresh-token grant. The injected `fetch` returns a local fixture; a connector normally uses the provider's fixed HTTPS token endpoint. `TokenEndpoint` also supports client-credentials and JWT-bearer grants, optional Basic authentication, declared response fields, bounded responses, and terminal OAuth error-code classification. A connector-specific `CredentialRefreshDriver` owns the credential shape; the host owns serialized persistence and must keep an old refresh token when the provider omits a replacement.

```js
import { SecretString } from '../dist/index.js';
import { TokenEndpoint, isRefreshRequiredWithIssueTime } from '../dist/oauthtoken/index.js';

const endpoint = new TokenEndpoint({
  providerName: 'Example', url: 'https://auth.example.test/token',
  terminalErrorCodes: ['invalid_grant'],
  fetch: async () => Response.json({ access_token: 'new-access', expires_in: 3600, token_type: 'Bearer' }),
});
const token = await endpoint.exchangeRefreshToken(
  { id: 'app-id', secret: new SecretString('test-client-secret'), authenticationMethod: 'client_secret_post' },
  new SecretString('test-refresh-token'),
);
const issuedAt = new Date();
const expiresAt = new Date(issuedAt.getTime() + token.expiresInMs);
if (isRefreshRequiredWithIssueTime(true, issuedAt, expiresAt, issuedAt, 'refresh')) throw new Error('fresh token was counted as expired');
if (token.nextRefreshToken(new SecretString('test-refresh-token')).reveal() !== 'test-refresh-token') throw new Error('refresh token was lost');
```

## Single-dispatch markers

The [runnable marker example](examples/singledispatch.mjs) demonstrates the Go-compatible heartbeat checkpoint decision. A claimed write requires read-back before another attempt can send. The live fence will use the Dex Step deadline, heartbeat APIs, and the same marker format.

```js
import { decideDispatch } from '../dist/singledispatch/index.js';

const callId = '5af8fafa-06ff-5b2d-bb13-04693809fabf';
const first = decideDispatch(callId, 1, undefined);
const later = decideDispatch(callId, 2, { callId, attempt: 1, state: 'claimed' });
if (first.kind !== 'sendAllowed' || later.kind !== 'readBackRequired') throw new Error('single-dispatch decision example failed');
```

## Release notes

### 0.1.0

First public TypeScript Connector SDK release: Go-compatible contracts and conformance fixtures, safe provider HTTP, project configuration, typed operation and credential interfaces, OAuth exchanges, scripted mocks, and single-dispatch markers. Dex Step factories and the live fence require the upcoming public Dex Context deadline and instance-addressed Step APIs; no live provider authorization has been tested.
