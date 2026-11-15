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
if (checkpoints.at(-1)?.providerId !== 'item-1' || 'sendCount' in checkpoints.at(-1)) throw new Error('provider identity was not checkpointed');

// The response was lost, so Dex retries. A limited operation with send cap 3 may resend after read-back.
const retryCheckpoints = [];
const retryContext = {
  ...context, attempt: 2, hasLastHeartbeatValue: () => true, getLastHeartbeatValue: () => checkpoints.at(-1),
  recordHeartbeat: async marker => { retryCheckpoints.push(marker); },
};
const retryFence = newFence(createCall(retryContext, call.connection, call.operation), 10_000, { maxSends: 3 });
const retryDecision = retryFence.decide();
if (retryDecision.kind !== 'readBackThenResendAllowed' || retryDecision.marker?.providerId !== 'item-1') throw new Error('bounded resend was not offered');
const isWriteConfirmedByReadBack = false; // A real connector reads back retryDecision.marker.providerId here.
if (!isWriteConfirmedByReadBack) await retryFence.dispatch(async () => ({ providerId: 'item-2' }));
if (retryCheckpoints[0]?.state !== 'claimed' || retryCheckpoints[0]?.sendCount !== 2) throw new Error('resend claim did not count both sends');
