// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createCall, newQueryBranch, runQuery } from '../dist/index.js';

const progressMessages = [];
const textChunks = [];
const progressStream = { write: (_context, message) => progressMessages.push(message) };
const textStream = { bufferedText: () => ({ write: chunk => textChunks.push(chunk) }) };
const operation = { connectorId: 'example', operationId: 'getItem' };
const context = { flowId: 'example-flow', stepExecutionId: 'getItem:1', attempt: 1 }; // Test context; real Steps use Dex AsyncContext.
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
const result = await runQuery(query, call, { id: 'item-1' });
if (result.branch !== 'found' || progressMessages.length !== 2 || textChunks.join('') !== 'Found item-1') throw new Error('progress example failed');
