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

// An application raises the send cap of one limited Mutation Step from the manifest's 3 to 5.
const sendCaps = [];
const notification = newMutationStep({
  stepType: 'SendNotification', annotations, connection, mapToOperationInput: input => input, maxSends: 5,
  operation: { definition: () => ({ operation: { connectorId: 'example', operationId: 'sendNotification' }, repeatOnUnknown: 'limited', maxSends: 3,
    branches: [{ id: 'sent', description: 'Notification sent' }, { id: 'defect', description: 'Connector defect', optional: true }],
    stepDefaults: { executeRetry: { initialIntervalMs: 1000, backoffCoefficient: 2, maximumIntervalMs: 60_000, maximumAttempts: 4, totalDurationMs: 600_000 } } }),
    idempotencyKey: callId => callId,
    invoke: (call, input) => { sendCaps.push(call.maxSendsOverride); return newMutationBranch('sent', { id: input.id }); } },
  branches: [goToBranch('sent', new StepRef('NotificationDone'))],
});
await notification.execute(context, { id: 'notification-1' });
if (sendCaps[0] !== 5 || notification.getStepOptions().executeRetry.maximumAttempts !== 6) throw new Error('send cap example failed');
