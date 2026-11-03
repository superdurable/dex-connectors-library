// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { createCall, SecretString } from '../dist/index.js';
import { newTextGenerationQuery, textGenerationBranchDefinitions } from '../dist/textgen/index.js';
import { newWireFormat } from '../dist/textgen/openaichat/index.js';

const provider = createServer(async (request, response) => {
  for await (const _chunk of request) { /* Consume the request. */ }
  response.setHeader('Content-Type', 'application/json');
  response.end(JSON.stringify({ id: 'example-response', model: 'example-model', choices: [
    { index: 0, message: { content: 'Hello from the model.' }, finish_reason: 'stop' },
  ], usage: { prompt_tokens: 3, completion_tokens: 5, total_tokens: 8 } }));
});
await new Promise(resolve => provider.listen(0, '127.0.0.1', resolve));
try {
  const address = provider.address();
  if (!address || typeof address === 'string') throw new Error('provider address unavailable');
  const operation = { connectorId: 'example', operationId: 'generateText' };
  const query = newTextGenerationQuery({
    definition: { operation, branches: textGenerationBranchDefinitions(), stepDefaults: { executeDurability: 'sync', executeMethodTimeoutMs: 60_000 } },
    wireFormat: newWireFormat({ providerName: 'example' }), baseURL: `http://127.0.0.1:${address.port}/v1`,
    connectionModel: 'example-model', requestTimeoutMs: 10_000, maxResponseBytes: 65_536, maxStreamEventBytes: 4_096,
    resolveCredential: () => new SecretString('example-only'),
  });
  const context = { flowId: 'textgen-example', stepExecutionId: 'generateText:1', attempt: 1, recordHeartbeat: async () => {} };
  const call = createCall(context, { provider: 'example', name: 'default' }, operation);
  const outcome = await query.invoke(call, { messages: [{ role: 'user', text: 'Say hello.' }] });
  assert.equal(outcome.kind, 'branch');
  assert.equal(outcome.branch, 'generated');
  assert.equal(outcome.value.text, 'Hello from the model.');
} finally {
  await new Promise(resolve => provider.close(resolve));
}
