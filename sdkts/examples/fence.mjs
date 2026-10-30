// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createCall } from '../dist/index.js';
import { newFence } from '../dist/singledispatch/index.js';

const checkpoints = [];
const context = {
  flowId: 'example-flow', stepExecutionId: 'write:1', attempt: 1,
  deadline: new Date(Date.now() + 10_000), cancellationSignal: new AbortController().signal,
  hasLastHeartbeatValue: () => false, getLastHeartbeatValue: () => undefined,
  recordHeartbeat: async marker => { checkpoints.push(marker); },
};
const call = createCall(context, { provider: 'example', name: 'default' }, { connectorId: 'example', operationId: 'writeItem' });
const fence = newFence(call, 10_000);
if (fence.decide().kind !== 'sendAllowed') throw new Error('write was unexpectedly fenced');
const result = await fence.dispatch(async signal => {
  if (signal.aborted || checkpoints.at(-1)?.state !== 'claimed') throw new Error('claim was not recorded before dispatch');
  return { providerId: 'item-1' };
});
await fence.recordProviderId(result.providerId);
if (checkpoints.at(-1)?.providerId !== 'item-1') throw new Error('provider identity was not checkpointed');
