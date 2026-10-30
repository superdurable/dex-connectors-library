# Connector TypeScript SDK

`@superdurable/connectors-sdk` is the provider-neutral TypeScript SDK for Dex connectors. It provides stable value contracts, typed Query and Mutation operations, Dex Step factories, credential refresh, OAuth token exchanges, HTTP safety helpers, project configuration loading, scripted mocks, and single-dispatch marker decisions. The package is at version `0.1.0`, requires Node 22 or 24, and uses `@superdurable/dex` 1.28.0 or newer.

Install dependencies and verify the package from this directory:

```sh
npm ci
npm run check
node examples/core.mjs
node examples/operations.mjs
node examples/steps.mjs
node examples/progress.mjs
node examples/providerhttp.mjs
node examples/projectconfig.mjs
node examples/refreshing_project.mjs
node examples/shared_project.mjs
node examples/connectormock.mjs
node examples/oauthtoken.mjs
node examples/singledispatch.mjs
node examples/fence.mjs
```

All durations in this package are milliseconds. Public JSON shapes use the Go SDK's field names. Credential values and project objects stay outside Flow state and cannot be serialized through their wrappers.

The [Dex integration harness](test/integ/step_flows.mjs) exercises the same Step factories through a real Worker and Client. Start `dexcli dev --open=false --dex-port 18891` with a local Dex server, set `DEX_SERVER_ADDRESS` when its gRPC address differs from `127.0.0.1:18891`, and run `npm run test:integration`. It verifies a happy path, optional branch, retry then success, retry exhaustion, uncertain mutation, a lost-response single-dispatch fence, repeated claim heartbeats, result Attribute persistence, progress and text Stream delivery, and scripted mock replay. It uses fixture operations and no provider credentials.

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

## Dex Step factories

The [runnable Step factory example](examples/steps.mjs) builds Query and Mutation Steps with instance-addressed branch targets. `newQueryStep` and `newMutationStep` validate required routes at construction; optional unwired routes force-fail the Flow if selected. Their `getStepOptions()` applies the connector's one-hour Execute retry policy and accepts Execute-only overrides. A provider retry becomes a Dex retry; a provider delay of at least one second is floored to whole seconds before `retryAfter` is thrown. Register each Step instance in a Dex `StepList`, along with its target Steps and any result Attribute used by the Flow.

Dex CLI 1.28.0 introduces TypeScript Flow analysis for FDG 2.0 and Dex Web connector release identity. A connector package supplies a static `dex-connector.json` manifest, and the consuming Flow source declares `// dex:group` and `// dex:explanation` beside each Step class. The generic SDK factories expose `StepAnnotations` and `configurationUI()` to applications; those values alone do not create a connector manifest. This SDK's integration harness verifies runtime Step behavior but does not verify FDG generation, which requires a consuming connector package and the CLI 1.28.0 binary.

```js
import { StepRef } from '@superdurable/dex';
import { goToBranch, newQueryBranch, newQueryStep } from '../dist/index.js';

const query = newQueryStep({
  stepType: 'GetItem', annotations, connection, mapToOperationInput: input => input,
  operation: { definition: () => ({ operation: { connectorId: 'example', operationId: 'getItem' },
    branches: [{ id: 'found', description: 'Item found' }, { id: 'defect', description: 'Connector defect', optional: true }], stepDefaults: {} }),
    invoke: (_call, input) => newQueryBranch('found', { id: input.id }) },
  branches: [goToBranch('found', new StepRef('QueryDone'))],
});
```

## Progress and text Streams

The [runnable progress example](examples/progress.mjs) shows `Call.reportProgress` and `Call.writeText` with test Streams. Both methods are no-ops when their Stream is omitted. In a real Flow, pass `progressStream`, `textStream`, and optional `textOptions` to a Query or Mutation Step factory, then register both Streams in that Flow's persistence schema. `reportProgress` adds the stable call ID, Execute attempt, and one-based sequence; `writeText` uses Dex's buffered text writer. The real Worker test in the integration harness verifies delivery and ordering.

```js
const call = createCall(context, { provider: 'example', name: 'default' }, operation, { progressStream, textStream });
const query = {
  definition: () => ({ operation, branches: [{ id: 'found', description: 'Found' }, { id: 'defect', description: 'Defect', optional: true }], stepDefaults: {} }),
  invoke: (providerCall, input) => {
    providerCall.reportProgress({ phase: 'reading', percent: 0 });
    providerCall.writeText('Found ');
    providerCall.writeText(input.id);
    providerCall.reportProgress({ phase: 'finished', percent: 1 });
    return newQueryBranch('found', { id: input.id });
  },
};
```

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

## Project credential providers

The [runnable refresh example](examples/refreshing_project.mjs) binds `newRefreshingCredentialProvider` to a named project connection. `newCredentialProvider` reads an API key or another nonrenewable credential on each call. A refresh provider writes a `REFRESHING` head with a monotonic fence before sending one provider request, publishes an immutable result, and lets observers recover a result whose final head write was lost. A terminal provider response fences the connection as `REAUTHORIZATION_REQUIRED`; a transient response restores the prior credential. The local Dex Web projection writes an accepted refresh back to its mode-0600 `connections.json` only while that file still holds the imported credential. The decoder and encoder must cover the complete private credential, including a renewal token omitted by the provider's response.

For a loaded shared connection, the same provider reads the selected host connection through the credential broker. The [runnable shared connection example](examples/shared_project.mjs) passes a fifth `decodeShared` function to handle broker material without host-kept renewal secrets. `resolveWithRefresh` never invokes the local refresh driver for a shared connection. `resolveAfterRejection` asks the broker for a newer revision; broker reauthorization and selection failures stop the call.

```js
import { createCall } from '../dist/index.js';
import { loadFromEnvironment, newRefreshingCredentialProvider } from '../dist/projectconfig/index.js';

const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: path });
const provider = newRefreshingCredentialProvider(
  project.connections, { connectorId: 'example', connectionName: 'default' },
  value => ({ accessToken: String(value.accessToken), refreshToken: String(value.refreshToken) }),
  value => ({ accessToken: value.accessToken, refreshToken: value.refreshToken }),
);
const context = { flowId: 'example-flow', stepExecutionId: 'refresh:1', attempt: 1 };
const call = createCall(context, { provider: 'example', name: 'default' }, { connectorId: 'example', operationId: 'getItem' });
const credentials = await provider.resolveWithRefresh(call, {
  refreshRequired: () => false,
  refresh: async state => ({
    credentials: { accessToken: 'new', refreshToken: state.credentials.refreshToken },
    expiresAt: new Date(Date.now() + 3600_000),
  }),
});
```

```js
const provider = newRefreshingCredentialProvider(project.connections, { connectorId: 'example', connectionName: 'shared' },
  value => {
    if (typeof value.accessToken !== 'string' || typeof value.refreshToken !== 'string') throw new TypeError();
    return { accessToken: value.accessToken, refreshToken: value.refreshToken };
  }, value => ({ ...value }),
  value => {
    if (typeof value.accessToken !== 'string') throw new TypeError();
    return { accessToken: value.accessToken, refreshToken: '' };
  });
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

The [runnable marker example](examples/singledispatch.mjs) demonstrates the Go-compatible heartbeat checkpoint decision. The [runnable fence example](examples/fence.mjs) records a claim before one provider write, repeats it every five seconds while the write runs, and checkpoints an accepted provider ID for read-back. Construct one `Fence` per sync-durable Mutation Step attempt, call `decide()` before credentials or provider lookups, and use `recordNotSent()` for a pre-send Retry. `dispatch()` passes an `AbortSignal` bounded by the smaller of the Execute timeout and public `Context.deadline`, preserving time for outcome classification. Use `recordCanResend()` only after positive evidence the provider did not process a request. A failed claim checkpoint prevents the send; an unconfirmed claim on a later attempt permits read-back only.

```js
import { decideDispatch } from '../dist/singledispatch/index.js';

const callId = '5af8fafa-06ff-5b2d-bb13-04693809fabf';
const first = decideDispatch(callId, 1, undefined);
const later = decideDispatch(callId, 2, { callId, attempt: 1, state: 'claimed' });
if (first.kind !== 'sendAllowed' || later.kind !== 'readBackRequired') throw new Error('single-dispatch decision example failed');
```

```js
import { newFence } from '../dist/singledispatch/index.js';

const fence = newFence(call, 10_000);
if (fence.decide().kind !== 'sendAllowed') throw new Error('write was unexpectedly fenced');
const result = await fence.dispatch(async signal => {
  if (signal.aborted || checkpoints.at(-1)?.state !== 'claimed') throw new Error('claim was not recorded before dispatch');
  return { providerId: 'item-1' };
});
await fence.recordProviderId(result.providerId);
```

## Release notes

### 0.1.0

First public TypeScript Connector SDK release: Go-compatible contracts and conformance fixtures, safe provider HTTP, project configuration with durable credential refresh and guarded local writeback, typed operations and Dex Step factories, OAuth exchanges, scripted mocks, and the live single-dispatch fence. No live provider authorization has been tested.
