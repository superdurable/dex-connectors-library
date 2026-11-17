# Connector TypeScript SDK

`@superdurable/connectors-sdk` is the provider-neutral TypeScript SDK for Dex connectors. It provides stable value contracts, typed Query and Mutation operations, Dex Step factories, credential refresh, OAuth token exchanges, HTTP safety helpers, project configuration loading, scripted mocks, and single-dispatch marker decisions. The package is at version `0.13.0`, requires Node 22 or 24, and uses `@superdurable/dex` 1.45.4 or newer. It is ESM-only, matching Dex: use `import`; direct `require()` fails with `ERR_PACKAGE_PATH_NOT_EXPORTED`.

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
node examples/triggers.mjs
node examples/polling_trigger.mjs
```

All durations in this package are milliseconds. Public JSON shapes use the Go SDK's field names. Credential values and project objects stay outside Flow state and cannot be serialized through their wrappers.

The [Dex integration harness](test/integ/step_flows.mjs) exercises the same Step factories through a real Worker and Client. Start `dexcli dev --open=false --dex-port 18891 --durability process-crash-safe` with a local Dex server, set `DEX_SERVER_ADDRESS` when its gRPC address differs from `127.0.0.1:18891`, and run `npm run test:integration`. It verifies a happy path, optional branch, unwired optional branch failure, retry then success, retry exhaustion, uncertain mutation, a lost-response single-dispatch fence, repeated claim heartbeats, result Attribute persistence, progress and text Stream delivery, and scripted mock replay. It uses fixture operations and no provider credentials. CI runs the same harness in `sdkts-integration` using its once-per-run resolved Dex CLI.

## Portable text generation

The [runnable text generation example](examples/textgen.mjs) uses `newTextGenerationQuery` with the OpenAI-compatible Chat Completions adapter from `@superdurable/connectors-sdk/textgen/openaichat`. The `@superdurable/connectors-sdk/textgen` entrypoint also exports request and response contracts, model ID validation, portable schema checks, branch definitions, temperature policies, error rules, and the provider-neutral wire-format contract. The example exercises a local fixture provider; a consuming connector supplies a real connection, credential resolver, and Dex Step.

`generateText` requires sync Execute durability and a request timeout shorter than the Step Execute timeout. It writes streamed text deltas to a configured text Stream as bytes arrive; the Result text is authoritative after retries or a blocked response. Register that Stream in the Flow persistence schema. Its six branches are `generated`, `truncated`, `blocked`, `providerRejected`, `invalidResponse`, and `defect`; only `generated` is required. The query retries transport errors, stalled responses, interrupted streams, and safe HTTP failures. A credential is resolved once after local validation and is never stored in Flow state, results, failures, or receipts.

Each invocation resolves the trimmed request model or connection model, then validates messages, the token limit, the structured-output name and schema, declared features, temperature, and reasoning effort in that order. It applies model schema rules, resolves one credential, and encodes the request before dispatch. The first error in this order selects `defect` with zero provider requests. A nil heartbeat every five seconds keeps a silent call alive and clears any Step heartbeat checkpoint. Stream text may contain text from an attempt later retried or blocked, so consumers use the final Result for decisions. A caller may inject `httpClient` to own HTTP transport as in Go; the default transport permits private HTTPS endpoints and neither transport should replay redirects.

An injected HTTP client may return a complete SSE response body without calling `onResponseChunk`. If it calls `onResponseChunk`, it must feed every chunk in order before returning and stop when the callback returns `false`. A provider retry-delay reader that throws or returns a non-finite value is treated as absent, allowing the `Retry-After` header to supply the delay. Usage token counts are JavaScript numbers and require safe integers; unlike Go's `int64` counts, larger values are rejected because they cannot be represented exactly in the public TypeScript `Usage` contract.

The schema subset is the same as Go `sdkgo/textgen`: root object, closed objects with every property required, nullable optional values, arrays with `items`, and only the documented portable keywords. Structured output is checked locally even when the provider accepts a transformed schema. Model IDs are validated according to whether the adapter sends them in a JSON body or path segment. Optional request fields rejected by a wire format or selected model produce a `defect` branch before dispatch.

Exact structured-output numbers may use at most 256 literal characters and an exponent magnitude of at most 400. Date and time formats follow Go's parser, including calendar-valid dates, one-digit hours, comma fractions, and offsets through hour 24 and minute 60. The JSON-object instruction uses Go's sorted, HTML-safe JSON encoding so provider requests match the Go connector. The shared [string-format fixture](../sdkgo/textgen/textgentest/testdata/string_format_cases.json) runs in both Go and TypeScript tests.

The `@superdurable/connectors-sdk/textgen/textgentest` entrypoint ports Go's credential-safe fake provider and reusable exchange suite. [Its provider-dialect test](test/textgentest.test.ts) exercises model precedence, all common branches, error rules, complete and interrupted streams, usage, receipts, and credential redaction. The [Dex integration harness](test/integ/step_flows.mjs) calls `runTextGenerationDexScenarios` through a real Worker and Client to verify retry, heartbeat, text Stream persistence, and an unwired optional branch.

## Typed operations and credential resolution

The [runnable operation example](examples/operations.mjs) validates a Query definition, binds a live Step context to stable call identity, resolves credentials at invocation time, and completes a Go-compatible zero receipt. `Mutation` adds an idempotency-key method and may return `newMutationUncertain` only for a dispatched write that cannot be repeated or confirmed by read-back. `runQuery` and `runMutation` throw `RetryError` for a safe retry; a Step factory turns that into Dex retry behavior. A Step that calls them directly catches the error and calls `throwDexRetry`, the same translation. The example uses a test context; application Steps receive `AsyncContext` from Dex.

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

A Mutation Step factory's `maxSends` option sets the send cap of one Step, from 1 to 10, replacing the operation's manifest `execution.maxSends`; 1 disables resend. Only an operation whose definition declares `repeatOnUnknown: 'limited'` accepts it, and construction throws for `safe`, `unsafe`, or a definition that predates the field. When the manifest retry policy limits attempts and `stepOptionsOverride.executeRetry` is unset, a larger cap adds the difference to the maximum attempts; it never lowers them. Every attempt receives the cap on `Call.maxSendsOverride`, which the connector's single-dispatch `Fence` applies. The [Step factory example](examples/steps.mjs) raises a cap from 3 to 5.

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
`ProviderTransportError` also preserves `status` and `headers` when body reading fails after response headers arrive. A connector can distinguish an interrupted 2xx exchange from a failure before any response; its message never includes header values.
`ProviderStalledError` preserves the same metadata when the stall occurs after response headers.
`encodeQueryValues` encodes a string multimap exactly like Go `url.Values.Encode`: UTF-8 byte-sorted keys, repeated values in order, `+` for spaces, and Go's query escaping. Empty value lists are omitted.
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

## Application environment

AppManifest `application.environment` declares application-owned strings, and Dex Web stores each value in the project configuration: ordinary values inline and application secrets as exact private object versions. The [complete runnable application environment example](examples/application_environment.mjs) writes a secret, pins it in a snapshot, and resolves it the way a Worker does.

`LoadedProject.resolveApplicationEnvironment()` reads each secret by exact version, checks its digest, and requires the object to bind the project scope and field name. It makes no provider calls, refreshes, writes, or mutable configuration reads, so later secret versions do not change a running Worker. The returned `ApplicationEnvironment` offers `lookup(name)` and `apply()`. `apply()` sets only application-owned names in `process.env`, or in the object you pass. It first checks every field and refuses all of them if any name is reserved: `PATH`, `HOME`, `PORT`, `HOST`, `NODE_OPTIONS`, `SSL_CERT_FILE`, `SSL_CERT_DIR`, `PUBLIC_BASE_URL`, `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY`, or a `SUPERVERSE_`, `DEX_`, `AWS_`, `LD_`, `GO`, or `GIT_` prefix. Resolve and apply once at startup, before reading application settings or creating clients. The object redacts values from `toString`, template literals, `util.inspect`, and console output, and `JSON.stringify` throws. Errors never contain values. Never put resolved values in Flow inputs, Attributes, Results, Streams, or logs.

`validateAndSortEnvironmentDeclarations`, `canonicalEnvironmentDeclarations`, `validateEnvironmentValue`, `ConfigurationStore.createApplicationSecret`, and `ConfigurationStore.validateApplicationEnvironment` apply Go's rules for writers such as Dex Web. Names are unique uppercase ASCII identifiers of at most 128 characters, with at most 128 fields. Values are at most 32,768 UTF-8 bytes and contain no NUL. `minLength` counts Unicode code points. A field has at most 128 unique enum values, and a secret field cannot declare an enum. A local Dex Web project selected with `DEX_CONNECTOR_CONFIG_FILE` has no application environment, so it resolves to zero fields, as in Go and Python.

```js
// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { inspect } from 'node:util';
import { ConfigurationStore, InMemoryObjectStore, LoadedProject } from '../dist/projectconfig/index.js';

// Dex Web's role: store the secret as an exact private version, then pin it in a configuration snapshot.
const scope = { projectId: 'example', kind: 'preview' };
const objects = new InMemoryObjectStore();
const configurations = new ConfigurationStore(objects, scope);
const declarations = [
  { name: 'REVIEW_API_KEY', required: true, secret: true, minLength: 8 },
  { name: 'SLACK_REVIEW_CHANNEL', required: true, enum: ['#ops', '#reviews'] },
];
const reviewKey = await configurations.createApplicationSecret('REVIEW_API_KEY', 'test-only-review-key');
const written = await configurations.writeConfiguration(0, {
  connections: [], triggerBindings: [], operationConfigurations: [],
  environment: { REVIEW_API_KEY: { secretRef: reviewKey }, SLACK_REVIEW_CHANNEL: { value: '#reviews' } },
});
await configurations.validateApplicationEnvironment(declarations, written);
const snapshot = await configurations.freezeConfiguration(written.revision);

// The Worker's role; a hosted Worker gets the same project from loadFromEnvironment().
const project = new LoadedProject(await configurations.readSnapshot(snapshot), snapshot, objects, undefined, undefined);
const environment = await project.resolveApplicationEnvironment();
const workerEnvironment = {};
environment.apply(workerEnvironment); // Omit the argument to set process.env before creating application clients.
if (workerEnvironment.REVIEW_API_KEY !== 'test-only-review-key' || workerEnvironment.SLACK_REVIEW_CHANNEL !== '#reviews') throw new Error('application environment example failed');
if (inspect(environment).includes('test-only') || String(environment).includes('test-only')) throw new Error('application environment was not redacted');
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

The [runnable marker example](examples/singledispatch.mjs) demonstrates the Go-compatible checkpoint decision. The [runnable fence example](examples/fence.mjs) records a claim before one provider write, repeats it every five seconds while the write runs, and checkpoints an accepted provider ID for read-back. Construct one `Fence` per sync-durable Mutation Step attempt, call `decide()` before credentials or provider lookups, and use `recordNotSent()` for a pre-send Retry. `dispatch()` passes an `AbortSignal` bounded by the smaller of the Execute timeout and public `Context.deadline`, preserving time for outcome classification. Use `recordCanResend()` only after positive evidence the provider did not process a request. A failed claim checkpoint prevents the send; by default, an unconfirmed claim on a later attempt permits read-back only.

Bounded resend lets a `limited` operation, whose duplicate is acceptable, resend after an unknown outcome. Pass the manifest send cap as `newFence(call, timeout, { maxSends })`, an integer from 1 to 10. Without the option the cap is 1, which disables resend and keeps the decisions of earlier releases. From 2, an application's `Call.maxSendsOverride` replaces the connector's cap. With a cap of at least 2, markers carry `sendCount`, the possibly effective sends of the call ID through its attempt, omitted when zero; with a cap of 1 they keep the earlier bytes. Decoding accepts both forms. The fence bounds the sends before the current attempt: without a marker, one per earlier attempt; otherwise the marker's count plus one per later attempt. A legacy marker without `sendCount` counts its own attempt when `claimed` and the earlier attempts when `notSent` or `canResend`. Below the cap, a decision that would require read-back becomes `readBackThenResendAllowed`: read back when possible, select the happy path on confirmed success, and otherwise call `dispatch()` once. A marker with `providerId` still means reading back that ID first. At the cap, the decision stays `readBackRequired`, so an unconfirmed outcome selects `uncertain`. A claim records the bound plus one, `notSent` and `canResend` record the bound, and a provider ID records its claim's count. `recordNotSent()` is allowed only after `sendAllowed`, and a short write window records `notSent` only then: after `readBackThenResendAllowed` an earlier send is unresolved, so return Retry without it, and the next attempt counts this one as a possible send. An own entry with an impossible send count fails closed. A marker from the current or a later attempt never permits a resend; it requires read-back.

From 0.13.0, one Step may make several fenced writes, such as an agent tool that sends a Gmail message and then posts a Slack thread reply, also concurrently under `Promise.all`. Each `Fence` keeps the marker of its own call ID, and every heartbeat any Fence of the Step attempt records carries the markers of all its calls: the previous attempt's entries plus those recorded so far in this attempt. The shared per-attempt state lives in a registry on `globalThis` under `Symbol.for('@superdurable/connectors-sdk/singleDispatchCheckpoints/v1')`, so connector packages that load different copies of this SDK still share it. `decideDispatch` takes the previous attempt's whole heartbeat and reads only its own call ID's entry. A missing entry, or a heartbeat it cannot read, including the single marker that releases before 0.13.0 recorded and a `null` or `undefined` value, counts as no marker: attempt one may send, and a later attempt assumes each earlier attempt sent once, which can only overcount. Only a heartbeat read error fails closed besides an impossible own entry. The heartbeat format is internal to the connector SDKs and may change; hosts must not parse it and classify a result by the connector's selected branch.

```js
import { decideDispatch } from '../dist/singledispatch/index.js';

// The last heartbeat is opaque: pass Dex's value through. Without a checkpoint entry for this
// call ID, each earlier attempt counts as one possible send.
const callId = '5af8fafa-06ff-5b2d-bb13-04693809fabf';
const first = decideDispatch(callId, 1, undefined);
const later = decideDispatch(callId, 2, undefined);
if (first.kind !== 'sendAllowed' || later.kind !== 'readBackRequired') throw new Error('single-dispatch decision example failed');
const belowCap = decideDispatch(callId, 2, undefined, undefined, 3);
const atCap = decideDispatch(callId, 4, undefined, undefined, 3);
if (belowCap.kind !== 'readBackThenResendAllowed' || atCap.kind !== 'readBackRequired') throw new Error('bounded resend decision example failed');
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

const retryFence = newFence(createCall(retryContext, call.connection, call.operation), 10_000, { maxSends: 3 });
const retryDecision = retryFence.decide();
if (retryDecision.kind !== 'readBackThenResendAllowed' || retryDecision.marker?.providerId !== 'item-1') throw new Error('bounded resend was not offered');
const isWriteConfirmedByReadBack = false; // A real connector reads back retryDecision.marker.providerId here.
if (!isWriteConfirmedByReadBack) await retryFence.dispatch(async () => ({ providerId: 'item-2' }));
```

## Triggers

A Trigger is a long-running provider event source outside a Flow, at parity with the Go SDK's Trigger contract. The [runnable Trigger example](examples/triggers.mjs) serves a signed webhook, records each event in an inbox before answering 200, and delivers it to the application target; its in-memory inbox lasts only for the process, while `project.triggerInbox(key)` persists. The [Dex integration harness](test/integ/trigger_flows.mjs) runs the Dex targets against a real Worker and Client, replays a local Dex Web inbox after a real process restart, delivers a signed relayed request through a persisted inbox, and consumes an inbox the Go SDK wrote.

- `TriggerSource.run(context, target)` owns the provider transport. It calls `prepareTriggerDelivery` before acknowledging an event and retries with `deliverTrigger`, whose capped exponential delay starts at 250 milliseconds and doubles up to 30 seconds. `TriggerContext` carries the `AbortSignal` that stops delivery, event-scoped log attributes, and the skip recorder; create it with `newTriggerContext(signal)`.
- `newTrigger({ definition, binding, source, target })` replays a durable target's pending events first, then runs the source, consuming every `UndeliverableTriggerError`.
- `newDexFlowTriggerTarget(client, flow, filter, resolveFlowId, mapToFlowInput)` starts a Flow with the event ID as request ID and Flow ID reuse disallowed, so a duplicate start counts as delivered. `newDexRPCTriggerTarget(client, rpcMethod, filter, resolveFlowId, mapToRPCInput, options)` invokes an RPC with a request ID that every redelivery of the event repeats, so Dex Server applies the RPC's Flow changes at most once per event while the Flow run keeps that ID, provided `mapToRPCInput` is deterministic. Unlike the Go SDK target, which sends a new request ID per call, it derives the ID with `triggerRPCRequestId(rpcName, eventId)`: `trigger-rpc-` followed by the first 16 bytes, as unpadded base64url, of the SHA-256 digest of the UTF-8 bytes of the RPC name, one `0x00` byte, and the UTF-8 bytes of the event ID, with an unpaired surrogate encoded as U+FFFD. For example, `receiveReply` and `Ev1` hash the bytes `72 65 63 65 69 76 65 52 65 70 6c 79 00 45 76 31` and yield `trigger-rpc-kDFFz9I4ELDtzXkmvRTzlA`. The RPC name is `options.rpcName` when set, and otherwise the handler's JavaScript method name. Set `rpcName` to the RPC's wire name whenever the RPC has an `@rpc({ name })` or `declareRPCMethod` name override, or a build may minify or rename methods: without it, renaming the method changes the IDs of events still being redelivered. It must contain a non-blank character and no NUL; setting it to the current method name keeps existing IDs. The ID never covers the mapped input. The target cannot see its binding, so two bindings that route one event to the same RPC on the same Flow share one ID and Dex applies only the first; give each such binding its own RPC. Declare the handler transactional (`isTransactional: true`) or with Attribute locks: Dex then answers a repeat from the recorded reply without running the handler. Any other handler runs again on a repeat, and Dex discards that attempt's Flow changes when it makes any. Each Flow run keeps only its newest request IDs, as many as the server's `limits.maxRequestIdsPerRun` (1000 by default), shared with its other requests that carry request IDs such as Client Attribute writes; a redelivery after its ID was evicted applies again, so a handler that must ignore repeats beyond that window also keeps its own record of event IDs.
  - Dex fingerprints the whole RPC request: input, RPC name, run, timeout, locks, loads, and the transactional flag. A deterministic mapper repeats an identical request, which Dex joins while it is in flight and answers from the recorded reply once it committed. A conflict happens only when the same event maps to a different request: a mapper that reads the clock or changed between releases, two bindings that route the event to the same RPC on the same Flow, or changed RPC options. Dex then answers `INVALID_ARGUMENT` "request ID ... was already used for a different request", and the target logs ERROR "trigger rpc request conflict; event dropped" with the event, request, RPC, and Flow IDs, never the payload or input, and rejects with `TriggerRPCRequestConflictError`, an `UndeliverableTriggerError` whose cause is the `DexServiceError`, so delivery drops the event on its first conflict. Dex reports the same conflict while the earlier request is still in flight, and the SDK cannot tell it from a committed one: if that earlier request later fails without committing, the event is lost. Keep `mapToRPCInput` deterministic and set `rpcName` where the method name can change. Once Dex distinguishes committed conflicts (superdurable/dex#1866), a future release can drop only events whose earlier request committed.
  - The Dex Client reads an input when the handler declares two or more parameters or an input codec, which the target cannot see, so it picks the call per shape. Without a mapper (`undefined`), it invokes an input-free RPC with the request ID; a handler with two or more parameters requires a mapper. A handler with two or more parameters, or a mapper that returns a value other than `undefined`, gets the mapped input with the request ID, which covers a default-valued input parameter with an `inputCodec`. A shorter handler whose mapper returns `undefined` may be input-free or take an input codec, so it is invoked as in 0.10.0, without a request ID; omit the mapper of an input-free RPC to deduplicate it. A handler name that is empty or contains NUL, without `rpcName`, is also invoked without a request ID. An input-free RPC whose mapper returns a value is not supported: 0.10.0 sent that value as the run ID.
  - A closed or never-started Flow, an input that cannot be encoded, a handler that throws `UndeliverableTriggerError`, and a request ID conflict are undeliverable. Every other failure is retried.
- `newDurableTriggerTarget(project.triggerInbox(key), key, target)` stores acknowledged events in project storage until the target consumes them. The inbox object key and document match the Go SDK byte for byte: each event stays the raw JSON bytes it was written as, so a Go-written event keeps numbers beyond double precision, Go's key order, and bytes that are not valid UTF-8 through every rewrite, and the document decodes as Go's strict decoder does. Replay hands each payload to the `decodePayload` option as UTF-8 text and as the exact stored bytes. The default, `parseTriggerPayloadJSON`, decodes the bytes as Go's encoding/json decodes them into `any`: every finite number becomes the nearest double, so 9007199254740993 arrives as 9007199254740992, 1e-400 becomes 0, and a number beyond the double range such as 1e400 is refused, leaving its event pending; strings and keys follow Go's rules, so an unpaired surrogate escape and each invalid UTF-8 byte become U+FFFD. A connector whose payloads carry exact integers beyond 2^53 passes its own `decodePayload`, which reads the raw text or bytes. New events encode with `encodeGoJSONValue`, which writes like Go's encoding/json for maps, without recursion up to Go's 10000 nesting levels, and refuses NaN, Infinity, array holes, cycles, and other values Go cannot encode; object keys are sorted like a Go map's, while a Go struct keeps its field order, so the two differ only in key order. A local Dex Web project keeps inboxes in `trigger-inboxes/` beside `connections.json`. One inbox holds at most 1000 events and 8 MiB, as in Go; on S3 both SDKs store at most 4 MiB per object.
- `@superdurable/connectors-sdk/webhooktrigger` supplies `WebhookEndpoint`, which serves a provider webhook as a `node:http` request listener: 200 after every accepting source recorded the event, 400 for a failed verification or decoding, 405 for another method, 413 for an oversized body, and 503 while no source runs or recording fails. Each verified event records and queues one at a time, in the order it was verified, so the inboxes and the sources hold events in arrival order even when an earlier request records more slowly; each answer waits until its event is queued. A request that fails verification never holds up others, and an event that cannot record and queue within `arrivalTimeoutMs` (10 seconds by default) of its slot's reservation is answered 503 for a provider retry. Its durable inboxes refuse a write they have not started; a write already in flight that then succeeds still queues the event, so it is delivered at least once, possibly after events that arrived later. `newRelayedRequestVerification(project)` verifies requests a Dex host relays.
- `@superdurable/connectors-sdk/relaysignature` signs and verifies host-relayed requests independently of any connector: `verifyRelaySignature(token, headers, rawBody, receivedAt)` returns `{ isValid: true, signedAt }` or `{ isValid: false, refusal }` with `missing`, `malformed`, `stale`, or `mismatch`. `LoadedProject.verifyRelayedRequest` reads the scope token itself, once for concurrent requests, and rereads it at most every 30 seconds after a well-formed, fresh signature mismatches; a mismatch inside that window rejects with the retryable `RelayTokenRenewalDeferredError`, so a host's retry after a token rotation is accepted; `LoadedProject.registerRelayedTrigger(key, path)` registers a shared connection's relayed route on the credential broker's origin. Neither logs nor returns the token.

Delivery records go to a `TriggerLogger`; `defaultTriggerLogger` writes INFO and above as JSON lines on stderr. Records carry IDs, error classes, and gRPC codes, never error messages, payloads, or credentials. A TypeScript RPC handler rejects an event by throwing `UndeliverableTriggerError`, whose name, `UndeliverableTriggerErrorType`, the Worker reports as the error type; the RPC target requires that exact type and the exact message prefix. A Worker code alone never marks an event undeliverable, so a Go handler's marker is retried by a TypeScript target.

```js
const key = { connectorId: 'sample', connectionName: 'workspace', triggerName: 'itemCreated', bindingName: 'start' };
const inbox = new TriggerInbox(new InMemoryObjectStore(), { projectId: 'example', kind: 'preview' }, key);
const target = newDurableTriggerTarget(inbox, key, { handleTrigger: async (_context, event) => { delivered.push(event.payload.itemId); } });
const definition = { trigger: { connectorId: 'sample', triggerName: 'itemCreated' }, description: 'Receive a new item.' };
const runner = newTrigger({ definition, binding: { connection, trigger: definition.trigger, name: 'start' }, source: endpoint.newSource(), target });
```

## Polling Triggers

A connector whose provider exposes a change feed (a sync token, a history ID, a page token, or a sequence) builds its Trigger sources on `@superdurable/connectors-sdk/pollingtrigger`, at parity with the Go SDK's `sdkgo/pollingtrigger`, instead of its own poll loop. The connector supplies a `ChangeFetcher`: `seedCursor(context)` returns the provider's current position without reporting existing items, and `fetchChanges(context, cursor)` returns one `ChangePage` of at most `MaxChangesPerPage` (500) changes after a `Cursor`. The kit owns the cursor, the schedule, and delivery. The [runnable polling example](examples/polling_trigger.mjs) wires a fake change log to a durable inbox, and the [Dex integration harness](test/integ/polling_trigger_flows.mjs) runs the kit against a real Worker and Client for backlog, restarts, crashes on both sides of the commit, cursor reset, Retry-After, two replicas, and a shared feed.

- **Durable cursor.** `newBindingPoller` keeps one cursor per binding in `poll-cursors/` and `newFeedPoller` one cursor per shared feed in `poll-feeds/`, beside the bindings' `trigger-inboxes/` (`LoadedProject.pollCursorStore(key)` and `pollFeedCursorStore(key)`). The object keys and the `connectors.dex.dev/project-poll-cursor/v1alpha1` document match the Go SDK byte for byte, so a Go and a TypeScript replica can share one cursor: an opaque token of at most 64 KiB of UTF-8, an optional keyed snapshot, and commit and reset counters, written only by compare-and-swap. Snapshot keys sort by their UTF-8 bytes, as Go compares strings, and each `value` is raw JSON bytes. Reads decode as Go's strict decoder does; an integer beyond 2^53, which Go's int64 holds but a JavaScript number does not, makes the document invalid. A poll whose page is empty and leaves the cursor where it was writes nothing. The in-memory and Dex Web local stores keep cursors in process memory, as Go's do, so a local restart reseeds.
- **Ordering rule.** The cursor commits past a change only after every member target recorded it (a target with `prepareTrigger`, such as `newDurableTriggerTarget` builds) or consumed it (any other target). Recorded events are delivered after the commit, in page order; while one fails, the poller retries it before fetching again. A commit conflict with a replica still delivers what this replica recorded. A retry from the same cursor skips the event IDs each member already recorded or consumed, even when the provider now returns a different page, and still delivers recorded events the retry no longer lists. Delivery is at least once: every event ID must be a pure function of the provider change, so a refetch after a crash yields the same IDs and `newDexFlowTriggerTarget` starts one Flow per event.
- **Seed and reset.** The first poll seeds the cursor with no backlog. A fetch error marked with `markCursorInvalid` (for example 410 Gone for a sync token) reseeds, increments `resetCount`, and logs the gap at WARN; changes in the gap are not delivered.
- **Schedule.** One `Runner` from `newRunner` serves every poller of one connection, one poll at a time, in configuration order. `PollSchedule` sets `intervalMs` (at least 1000), `jitterFraction` (default 0.1), `maxBackoffMs` (default 15 minutes), and `maxPagesPerPoll` (default 10). Failures back off per poller, `intervalMs * 2^(n-1)` up to `maxBackoffMs`. An error marked with `markRateLimited(error, retryAfterMs)` pauses every poller of the runner for `max(retryAfterMs, intervalMs)`, or backs off exponentially without a Retry-After; `parseRetryAfter` from `providerhttp` reads the header. A fetcher calls `waitForPollRequest(context)` before every provider request, so an application `RequestLimiter` gates seeds and multi-request fetches too. `wakePoller(name)` makes a poller due now.
- **Keyed snapshots.** `KeyedSnapshotOptions` enables a bounded snapshot (default 10,000 entries, at most 100,000) stored in the cursor document and committed with it. A full-listing Trigger calls `diffKeyedSnapshot` inside `fetchChanges` and emits events from `added`, `changed`, and `removed`; a delta feed calls `applyKeyedSnapshotChanges`, optionally with `evictLeastRecentlySeen`. A feed member's selector receives the committed entry of a change that implements `KeyedItem` (`snapshotKey`, `snapshotHash`, `snapshotValue`).

`run(context)` checks every stored cursor, replays each durable target, and then polls until the context's signal aborts, when it rejects with `TriggerDeliveryAbortedError`. It rejects early only for a stored document that fails strict decoding (`PollCursorDocumentInvalidError`), which it never overwrites. A cursor state over a bound (`PollCursorTooLargeError`) is a configuration fault retried every `maxBackoffMs`. `newTriggerCredentialCall(connection, trigger, signal)` gives a poller the same stable credential-call ID the Go SDK and the Gmail Trigger use, and `newBindingTriggerSource(config, schedule)` adapts one binding poller to `TriggerSource` for `newTrigger`.

```js
// Durable inbox: events are recorded here before the cursor moves past them.
const target = newDurableTriggerTarget(new TriggerInbox(objects, scope, key), key, { handleTrigger: async (_context, event) => { delivered.push(event.id); } });
const cursor = newPollCursorStore(objects, scope, key);
const poller = newBindingPoller({ definition, binding, cursor, fetcher, target });
const runner = newRunner({ connection, schedule: { intervalMs: 1000, maxBackoffMs: 60_000 }, pollers: [poller] });
// Replays the inbox, seeds on first start, then polls until the signal aborts.
const running = runner.run(newTriggerContext(stop.signal)).catch(() => undefined);
```

Records carry `connector`, `connection`, and `trigger` plus `binding` (or `feed`), never payloads, cursor tokens, or snapshot values: INFO `poll cursor seeded` (`reset`), WARN `poll cursor invalid; reseeding` (`reset_count`), WARN `poll failed; retrying` (`attempt`, `delay_ms`), WARN `poll rate limited; pausing connection` (`delay_ms`, `pollers`), WARN `poll paused: trigger inbox full`, ERROR `poll paused: cursor state exceeds bound` and `poll cursor document invalid`, and DEBUG `poll cursor committed` (`commit_count`, `events`) and `poll cursor commit conflict`. Delivery uses the `trigger delivery failed; retrying`, `trigger delivered after retry`, and `trigger event skipped: undeliverable` records of `deliverTrigger`. The application's storage grant needs `GetObject` and `PutObject` on `poll-cursors/` and `poll-feeds/` as well.

## Release notes

### 0.13.0

(breaking) Several fenced writes in one Step. Every `Fence` heartbeat holds one marker per call ID of the Step attempt, merged across Fences, concurrent ones and ones from other copies of this SDK included. `decideDispatch` takes the previous attempt's whole heartbeat and reads only its own call ID's entry. A heartbeat it cannot read, including a single marker from an earlier release or a `null` value, counts as no marker instead of failing closed; a later attempt then assumes each earlier attempt sent once. The heartbeat format is internal and may change; hosts must not parse it. Releases before 0.13.0 fail closed on the new format.

### 0.12.0

Durable poll Trigger kit, at parity with the Go SDK's `sdkgo/pollingtrigger`. The new `@superdurable/connectors-sdk/pollingtrigger` export adds `newBindingPoller`, `newFeedPoller`, `newFeedMember`, `newRunner`, `newBindingTriggerSource`, `newMemoryCursorStore`, the fetcher signals `markCursorInvalid` and `markRateLimited`, `waitForPollRequest`, `diffKeyedSnapshot`, `applyKeyedSnapshotChanges`, and `newTriggerCredentialCall`. `projectconfig` adds `newPollCursorStore`, `newPollFeedCursorStore`, `LoadedProject.pollCursorStore` and `pollFeedCursorStore`, `PollCursorTooLargeError`, `PollCursorDocumentInvalidError`, and the cursor document codec, whose keys and bytes match the Go SDK's. The in-memory store now holds poll cursor objects up to 8 MiB, as it already did for Trigger inboxes. These additions are not breaking.

### 0.11.0

Bounded resend on unknown outcomes in the single-dispatch fence. The heartbeat marker gains an optional `sendCount`, the possibly effective sends of the call ID through the marker's attempt, encoded after `providerId` and omitted when zero. Only a `Fence` with a send cap of at least 2 writes it; a cap of 1 writes the same marker bytes as 0.10.1. `FenceOptions.maxSends` and a new last parameter of `decideDispatch` set the send cap from 1 to 10. With a cap of at least 2, a decision that would require read-back becomes the new `DecisionKind` `readBackThenResendAllowed` while the bounded prior sends stay below the cap: the connector reads back and may then call `dispatch()` once. At the cap it stays `readBackRequired`. `Decision.priorSendUpperBound` reports the bound. `MutationDefinition` gains `repeatOnUnknown` and `maxSends`, and `MutationStepConfig.maxSends` lets an application set one `limited` Step's cap, carried to the Fence on the new `Call.maxSendsOverride` through the new `CallOptions` of `createCall`. The default cap is 1, so a connector that does not pass `maxSends` keeps every earlier decision and marker; `readBackThenResendAllowed` appears only with a cap of at least 2. Earlier SDK releases reject a marker with `sendCount` as invalid, so roll back a connector that enabled bounded resend only after its in-flight fenced calls finish. These additions are not breaking.

### 0.10.1

Bug fix: `newDexRPCTriggerTarget` now sends one request ID per RPC name and event ID, so an event delivered more than once, after a durable inbox replay or a provider or relay retry, applies the RPC's Flow changes at most once within the Flow run's request-ID window when the input mapper is deterministic. Previously each delivery attempt was a new request and could apply twice. The new `RPCTriggerOptions.rpcName` sets the RPC name in the ID; set it to the wire name when the RPC has an `@rpc({ name })` override or the code is minified. A conflict happens only when the same event maps to a different request (a non-deterministic or changed mapper, two bindings to the same RPC and Flow, or changed RPC options): Dex answers `INVALID_ARGUMENT` "request ID ... was already used for a different request", and the target logs it at ERROR and drops the event as undeliverable with `TriggerRPCRequestConflictError`. Dex reports the same conflict while the earlier request is still in flight, so if that request later fails without committing, the event is lost; a mapper that reads the clock, which 0.10.0 applied again on every redelivery, now drops the redelivery instead. Keep mappers deterministic; superdurable/dex#1866 would let a future release drop only committed conflicts. The mapper may now be `undefined` for an input-free RPC. `triggerRPCRequestId`, `TriggerRPCRequestIdPrefix`, `RPCTriggerOptions`, and `TriggerRPCRequestConflictError` expose the derivation and the conflict. The package now requires `@superdurable/dex` 1.45.4 or newer, the first release with `RPCInvokeOptions.requestId`. Construction accepts every argument 0.10.0 accepted, so this change is not breaking for supported usage; the one behavior change outside it is an input-free RPC whose mapper returns a value, which 0.10.0 sent as the run ID by accident and which is not supported (omit the mapper instead).

### 0.10.0

Trigger support at parity with the Go SDK: Trigger definitions, bindings, runners, `deliverTrigger`, the Dex Flow and RPC Trigger targets, the durable project Trigger inbox, the `webhooktrigger` endpoint, and the connector-neutral `relaysignature` module with `LoadedProject.verifyRelayedRequest` and `registerRelayedTrigger`. `createTriggerCall` builds the credential call of a Trigger source. These additions are not breaking.

### 0.9.1

`LoadedProject.resolveApplicationEnvironment()` resolves the application environment that `dex-app.yaml` declares, at parity with Go `ResolveApplicationEnvironment` and Python `resolve_application_environment`. It reads application secrets by exact version with a digest check, and returns a redacting `ApplicationEnvironment` whose `apply()` refuses reserved names. `ConfigurationStore` adds `createApplicationSecret`, `resolveApplicationEnvironment`, and `validateApplicationEnvironment`. The package also exports the declaration validators and `isApplicationEnvironmentName`. Configuration snapshots now reject environment entries that Go rejects: reserved or invalid names, oversized values, and secret references outside the scope or field. They also accept the JSON `null` members that Go writes or decodes as empty: `connections`, `triggerBindings`, `operationConfigurations`, `environment`, `value`, `secretRef`, `credentialBroker`, `authMethodId`, and `sharedConnectionId`. This addition is not breaking.

Bug fix: a hosted project's credential broker token, at startup and on renewal after a 401, is now read as an application secret object with exact version, digest, scope, and the name `DEX_CREDENTIAL_BROKER_TOKEN`. The broker receives the token value, as in Go and Python. Previously the token sent was the raw object JSON. Concurrent broker reads now share one token read and one renewal after 401, and an unreadable configuration head during renewal keeps the current token reference, as in Go. A caller's abort or deadline also ends its wait for the token, so a stalled token read no longer blocks later broker reads. Credential broker token references with version `null` or a NUL, CR, or LF are rejected as Go rejects them, and endpoints follow Go's validation: at most 2048 UTF-8 bytes, no user information, query, or fragment, and plain HTTP only for private hosts. The broker client sends each request to the validated, normalized URL, and an endpoint is refused when URL normalization would change its host, such as a percent-encoded host, a hexadecimal, octal, or shortened IPv4 address, or an internationalized domain name.

### 0.9.0

`throwDexRetry` is exported. A Step that calls `runQuery` or `runMutation` directly catches `RetryError` and calls it to get the same Dex retry behavior as the Step factories. This addition is not breaking.

### 0.8.0

Manifest OAuth refresh and client-credentials minting honor `tokenExchangeTimeout` in addition to the caller's deadline; a timeout is retryable, matching Go's credential refresh drivers. The declaration accepts a fixed Go duration of at most one hour or `{fromSetting: "requestTimeout"}`; pass the pinned setting as a Go duration string or integer nanoseconds within the same bound. Provider HTTP preserves response metadata on interrupted body reads, and `encodeQueryValues` supplies Go-compatible query encoding for connector ports. These additions are not breaking; existing connectors keep their current query serialization until migrated.

### 0.7.0

Manifest token endpoint templates now apply declared `templateValueRules` to each placeholder before path escaping. A rule can trim Go Unicode whitespace, reject names, bound UTF-8 length, accept full-match patterns, and lowercase the accepted value. An invalid value requires reauthorization before any token request. The Microsoft app-only manifests use this to reject multi-tenant aliases and invalid tenants.
Template patterns use a portable Go and JavaScript subset: lookaround, backreferences, Unicode property escapes, quoted literals, POSIX classes, and inline flags are rejected when the manifest is loaded.
`trimClientID` on refresh or client-credentials declarations trims the client ID sent in the token request when the Go driver does so.
JWT-bearer grants accept a PEM private key directly from a credential field, as Salesforce declares, without requiring a JSON key credential.
Private-network HTTP token endpoints also reject fragments and non-printable or non-ASCII URL text before transport, matching the configurable Go connector.
Migration: `TokenEndpoint` accepts uppercase ASCII `HTTP://` and `HTTPS://` schemes; leading whitespace remains invalid. An address-policy refusal retains its `AddressRefusal` type so a connector can classify it as a terminal validation defect.

### 0.6.0

Manifest token grants accept `responseScopes: ignore` when the Go credential driver does not inspect reported scope values. The default checks returned scopes against declared requirements. Retryable token endpoint failures from manifest refresh now throw `RetryError`. Manifest grants also check HubSpot scope arrays, Microsoft Graph resource-prefixed scopes, Zoho and Salesforce provider origins, Pipedrive domain changes, one-hour expiry caps, and monday.com JWT expiry. Each scope-array element is one name, including any comma it contains. A configurable token endpoint can use the private-network HTTP address policy.

Without an injected transport, that policy checks resolved addresses before sending refresh credentials and refuses link-local, metadata, and public HTTP targets. A caller-owned `fetch` remains available for trusted proxies and tests. HubSpot's absent or null `scopes` array skips the array scope check; a null response URL reuses and validates the stored URL.

Generated connector timestamp fields use `GoTimeInputSchema`. It accepts the same input syntax as Go `time.Time.UnmarshalJSON`, including comma fractional seconds and unusual timezone offsets, and preserves the supplied string without rounding.

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
