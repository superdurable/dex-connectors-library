# Connector TypeScript SDK

`@superdurable/connectors-sdk` is the provider-neutral TypeScript SDK for Dex connectors. Phase 1a provides stable value contracts, credential interfaces, HTTP safety helpers, project configuration loading, and mock fixture data. It does not define Dex Steps or Trigger runners yet. The package is unreleased at version `0.1.0` and requires Node 20 or later.

Install dependencies and verify the package from this directory:

```sh
npm ci
npm run check
node examples/core.mjs
node examples/providerhttp.mjs
node examples/projectconfig.mjs
node examples/connectormock.mjs
```

All durations in this package are milliseconds. Public JSON shapes use the Go SDK's field names. Credential values and project objects stay outside Flow state and cannot be serialized through their wrappers.

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

The [complete runnable mock example](examples/connectormock.mjs) validates a JSON fixture before building a scripted branch. `retryCase` and `uncertainCase` model the other provider outcomes; `decodeCallReport` reads the Go-compatible test harness report.

```js
import { z } from 'zod';
import { branchCase, decodeOutput } from '../dist/connectormock/index.js';

const output = decodeOutput('{"id":"item-1"}', z.strictObject({ id: z.string() }));
const response = branchCase('created', output);
if (response.kind !== 'branch' || response.value.id !== 'item-1') throw new Error('mock example failed');
```
