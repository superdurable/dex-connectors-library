# Connector TypeScript SDK

`@superdurable/connectors-sdk` is the provider-neutral TypeScript SDK for Dex connectors. It provides stable value contracts, typed Query and Mutation operations, Dex Step factories, credential refresh, OAuth token exchanges, HTTP safety helpers, project configuration loading, scripted mocks, and single-dispatch marker decisions. The package is at version `0.6.0`, requires Node 22 or 24, and uses `@superdurable/dex` 1.28.0 or newer. It is ESM-only, matching Dex: use `import`; direct `require()` fails with `ERR_PACKAGE_PATH_NOT_EXPORTED`.

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
node examples/manifest_refresh.mjs
node examples/singledispatch.mjs
node examples/fence.mjs
node examples/textgen.mjs
```

All durations in this package are milliseconds. Public JSON shapes use the Go SDK's field names. Credential values and project objects stay outside Flow state and cannot be serialized through their wrappers.

The [Dex integration harness](test/integ/step_flows.mjs) exercises the same Step factories through a real Worker and Client. Start `dexcli dev --open=false --dex-port 18891` with a local Dex server, set `DEX_SERVER_ADDRESS` when its gRPC address differs from `127.0.0.1:18891`, and run `npm run test:integration`. It verifies a happy path, optional branch, unwired optional branch failure, retry then success, retry exhaustion, uncertain mutation, a lost-response single-dispatch fence, repeated claim heartbeats, result Attribute persistence, progress and text Stream delivery, and scripted mock replay. It uses fixture operations and no provider credentials. CI runs the same harness in `sdkts-integration` using its once-per-run resolved Dex CLI.

## Portable text generation

The [runnable text generation example](examples/textgen.mjs) uses `newTextGenerationQuery` with the OpenAI-compatible Chat Completions adapter from `@superdurable/connectors-sdk/textgen/openaichat`. The `@superdurable/connectors-sdk/textgen` entrypoint also exports request and response contracts, model ID validation, portable schema checks, branch definitions, temperature policies, error rules, and the provider-neutral wire-format contract. The example exercises a local fixture provider; a consuming connector supplies a real connection, credential resolver, and Dex Step.

`generateText` requires sync Execute durability and a request timeout shorter than the Step Execute timeout. It writes streamed text deltas to a configured text Stream as bytes arrive; the Result text is authoritative after retries or a blocked response. Register that Stream in the Flow persistence schema. Its six branches are `generated`, `truncated`, `blocked`, `providerRejected`, `invalidResponse`, and `defect`; only `generated` is required. The query retries transport errors, stalled responses, interrupted streams, and safe HTTP failures. A credential is resolved once after local validation and is never stored in Flow state, results, failures, or receipts.

Each invocation resolves the trimmed request model or connection model, then validates messages, the token limit, the structured-output name and schema, declared features, temperature, and reasoning effort in that order. It applies model schema rules, resolves one credential, and encodes the request before dispatch. The first error in this order selects `defect` with zero provider requests. A nil heartbeat every five seconds keeps a silent call alive and clears any Step heartbeat checkpoint. Stream text may contain text from an attempt later retried or blocked, so consumers use the final Result for decisions. A caller may inject `httpClient` to own HTTP transport as in Go; the default transport permits private HTTPS endpoints and neither transport should replay redirects.

An injected HTTP client may return a complete SSE response body without calling `onResponseChunk`. If it calls `onResponseChunk`, it must feed every chunk in order before returning and stop when the callback returns `false`. A provider retry-delay reader that throws or returns a non-finite value is treated as absent, allowing the `Retry-After` header to supply the delay. Usage token counts are JavaScript numbers and require safe integers; unlike Go's `int64` counts, larger values are rejected because they cannot be represented exactly in the public TypeScript `Usage` contract.

The schema subset is the same as Go `sdkgo/textgen`: root object, closed objects with every property required, nullable optional values, arrays with `items`, and only the documented portable keywords. Structured output is checked locally even when the provider accepts a transformed schema. Model IDs are validated according to whether the adapter sends them in a JSON body or path segment. Optional request fields rejected by a wire format or selected model produce a `defect` branch before dispatch.

Exact structured-output numbers may use at most 256 literal characters and an exponent magnitude of at most 400. Date and time formats follow Go's parser, including calendar-valid dates, one-digit hours, comma fractions, and offsets through hour 24 and minute 60. The JSON-object instruction uses Go's sorted, HTML-safe JSON encoding so provider requests match the Go connector. The shared [string-format fixture](../sdkgo/textgen/textgentest/testdata/string_format_cases.json) runs in both Go and TypeScript tests.

The `@superdurable/connectors-sdk/textgen/textgentest` entrypoint ports Go's credential-safe fake provider and reusable exchange suite. [Its provider-dialect test](test/textgentest.test.ts) exercises model precedence, all common branches, error rules, complete and interrupted streams, usage, receipts, and credential redaction. The [Dex integration harness](test/integ/step_flows.mjs) calls `runTextGenerationDexScenarios` through a real Worker and Client to verify retry, heartbeat, text Stream persistence, and an unwired optional branch.

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

The [complete runnable HTTP example](examples/providerhttp.mjs) sends one bounded request to a local server. `sendProviderRequest` resolves every address before connecting, pins the chosen address for IP literals and hostnames, and returns redirects without following them. `ProviderTransportError.isProvablyNotSent` is true for DNS, TCP connect, or TLS handshake failures before a usable connection could send HTTP bytes. Failures after connection or request writing have an unknown dispatch outcome, matching the Go connectors' DNS, connect, TLS, and obtained-connection trace.
`BodyTooLargeError` preserves the response `status` and `headers`, so a caller can classify a 429 or 503 and its `Retry-After` even when the response body exceeds its limit.
`ProviderRequest.onBodyLimit` defaults to `destroy`, closing an oversized response immediately. Set it to `drain` for provider calls that continue or bill work after a client disconnect: the SDK releases buffered bytes, delivers callback bytes only through the limit, reads to EOF, then rejects with `BodyTooLargeError`. Drain requires at least one positive `timeoutMs`, positive `stallTimeoutMs`, or `signal`; otherwise `sendProviderRequest` throws `TypeError`. These bounds remain active while draining. For non-2xx responses, an explicit `maxErrorResponseBytes` takes precedence and truncates the body immediately instead of draining. Once drain begins, a later read failure still rejects with `BodyTooLargeError`, including when `ignoreErrorBodyReadFailure` is set. The runnable example exercises `drain` with a local server.

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

The [runnable refresh example](examples/refreshing_project.mjs) binds `newRefreshingCredentialProvider` to a named project connection. `newCredentialProvider` reads an API key or another nonrenewable credential on each call. A refresh provider writes a `REFRESHING` head with a monotonic fence before sending one provider request, publishes an immutable result, and lets observers recover a result whose final head write was lost. An expired `AUTHORIZING` head publishes its immutable result when present or fences the connection as `REAUTHORIZATION_REQUIRED`; observers reload after competing writes. A terminal provider response also fences the connection as `REAUTHORIZATION_REQUIRED`; a transient response restores the prior credential. The local Dex Web projection writes an accepted refresh back to its mode-0600 `connections.json` only while that file still holds the imported credential. The decoder and encoder must cover the complete private credential, including a renewal token omitted by the provider's response.

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

The [runnable OAuth exchange example](examples/oauthtoken.mjs) shows a provider-neutral refresh-token grant. The injected `fetch` returns a local fixture; a connector normally uses the provider's fixed HTTPS token endpoint. `TokenEndpoint` also supports client-credentials and JWT-bearer grants, optional Basic authentication, declared response fields, bounded responses, and terminal OAuth error-code classification. The [manifest refresh example](examples/manifest_refresh.mjs) uses `ManifestCredentialRefreshDriver` with a generated authorization declaration and field map to exchange refresh-token, client-credentials, or JWT-bearer grants, preserve rotated material, check scopes, and replace only mapped fields. Constructor options also accept startup-pinned `settings`, a caller-owned `fetch`, and an injectable `now` clock. Credentials for a connector with several authorization methods use `authMethodId`; an empty value selects `defaultMethod`. Refresh validation uses `requiredScopes` when declared, and a grant can override `exactScopes` or `acceptsMissingTokenType`. `refresh.scopeSeparators` selects `whitespace` (the default) or `whitespaceAndComma` for both exact and all-scope checks. A blank client secret requires reauthorization unless `refresh.allowsPublicClient` is true. A response lacking `expires_in` is retryable by default or with `missingExpiry: reject`; `keep` returns an expired result, and a positive declared lifetime supplies its expiry. Only declared terminal OAuth errors below HTTP 500 require reauthorization; transport, server, and other token errors remain retryable.

An access token with no stored expiry remains usable by default. Declare `storedMissingExpiry: refresh` on a refresh, client-credentials, or JWT-bearer grant to refresh it, as Atlassian does. The separate `missingExpiry` declaration controls a token response without `expires_in`; when absent, that response is retryable. A declared `missingExpiry` lifetime supplies an expiry, while `keep` returns an expired result that the current project provider rejects. Refreshing multiple grants follows the Go driver's current single-expiry behavior: active grants rotate together and the earliest expiry wins. A process failure between provider response and persistence, or failure of a later grant after an earlier rotation, can lose a one-use refresh token.

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

### 0.6.0

Manifest token grants accept `responseScopes: ignore` when the Go credential driver does not inspect reported scope values. The default checks returned scopes against declared requirements. Retryable token endpoint failures from manifest refresh now throw `RetryError`. Manifest grants also check HubSpot scope arrays, Microsoft Graph resource-prefixed scopes, Zoho and Salesforce provider origins, Pipedrive domain changes, one-hour expiry caps, and monday.com JWT expiry. A configurable token endpoint can use the private-network HTTP address policy.

### 0.5.0

Manifest refresh supports `acceptedScopeSets` for providers that grant equivalent scope alternatives; on OAuth refresh it applies to every grant under `refresh.grants` and excludes `offline_access` from response checks. JWT bearer grants may declare `subjectValidation: goMailAddress`; the driver then validates the trimmed delegated subject using Go `mail.ParseAddress` address equality before sending a token request. `missingExpiry: reject` requires a positive `expires_in` and treats a malformed successful response as retryable.

### 0.4.0

Adds `storedMissingExpiry` to refresh-token, client-credentials, and JWT-bearer declarations. `refresh` renews a stored credential without expiry independently of `missingExpiry`, which still controls token responses without `expires_in`.

### 0.3.0

Adds declaration-driven required and exact scope checks, per-grant token-type rules, Go-compatible duration validation, and default-method selection for manifest refresh drivers. Scope parsing now follows `scopeSeparators`: whitespace by default, or spaces and commas when declared. A blank client secret now requires reauthorization unless public-client refresh is declared. A missing `expires_in` is retryable unless the manifest declares `missingExpiry`. These changes alter the published 0.2.0 refresh driver's behavior.

Public HTTPS address checks now refuse the IPv4 broadcast address, matching Go's global-unicast rule. Adds opt-in draining of oversized Provider HTTP responses with bounded cancellation and byte-exact chunk callbacks.

### 0.2.0

Adds manifest-driven refresh for OAuth, client-credentials, and JWT bearer grants, including rotated refresh tokens, declared scope and token-type checks, and retryable token endpoint failures. Connector migration requires schema and codegen support for the new token-type declarations.

### 0.1.0

First public ESM-only TypeScript Connector SDK release: Go-compatible contracts and conformance fixtures, safe provider HTTP, project configuration with durable credential refresh and guarded local writeback, typed operations and Dex Step factories, OAuth exchanges, scripted mocks, and the live single-dispatch fence. No live provider authorization has been tested.
