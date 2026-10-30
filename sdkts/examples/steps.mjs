// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepRef } from '@superdurable/dex';
import { goToBranch, newMutationBranch, newMutationStep, newQueryBranch, newQueryStep } from '../dist/index.js';

const annotations = { groupId: 'example', groupLabel: 'Example', explanation: 'Calls the example provider.' };
const connection = { provider: 'example', name: 'default' };
const context = { flowId: 'example-flow', stepExecutionId: 'provider:1', attempt: 1 };
const query = newQueryStep({
  stepType: 'GetItem', annotations, connection, mapToOperationInput: input => input,
  operation: { definition: () => ({ operation: { connectorId: 'example', operationId: 'getItem' },
    branches: [{ id: 'found', description: 'Item found' }, { id: 'defect', description: 'Connector defect', optional: true }], stepDefaults: {} }),
    invoke: (_call, input) => newQueryBranch('found', { id: input.id }) },
  branches: [goToBranch('found', new StepRef('QueryDone'))],
});
const mutation = newMutationStep({
  stepType: 'CreateItem', annotations, connection, mapToOperationInput: input => input,
  operation: { definition: () => ({ operation: { connectorId: 'example', operationId: 'createItem' },
    branches: [{ id: 'created', description: 'Item created' }, { id: 'defect', description: 'Connector defect', optional: true }], stepDefaults: {} }),
    idempotencyKey: callId => callId,
    invoke: (_call, input) => newMutationBranch('created', { id: input.id }) },
  branches: [goToBranch('created', new StepRef('MutationDone'))],
});
const queried = await query.execute(context, { id: 'item-1' });
const created = await mutation.execute(context, { id: 'item-1' });
if (queried.kind !== 'next' || created.kind !== 'next' || query.getStepOptions().executeRetry.totalDurationMs !== 3600_000) throw new Error('Step factory example failed');
