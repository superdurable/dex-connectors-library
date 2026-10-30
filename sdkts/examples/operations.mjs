// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

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
const query = {
  definition: () => definition,
  invoke: async (providerCall, input) => {
    const credential = await credentials.resolve(providerCall);
    if (credential.apiKey !== 'test-only') throw new Error('credential resolution failed');
    return newQueryBranch('found', { id: input }, zeroReceipt());
  },
};
const result = await runQuery(query, call, 'item-1');
if (result.branch !== 'found' || result.value.id !== 'item-1' || result.receipt.callId !== call.id) throw new Error('operation example failed');
