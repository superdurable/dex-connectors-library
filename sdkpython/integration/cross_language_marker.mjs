// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// The TypeScript half of the cross-language marker run; cross_language_marker.py starts it.
// A TypeScript RPC Trigger target invokes an RPC whose Python handler raises UndeliverableTriggerError.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import process from 'node:process';
import { deliverTrigger, newDexRPCTriggerTarget, newTriggerContext } from '../../sdkts/dist/index.js';

// The Dex TypeScript SDK resolves from sdkts, which owns that dependency.
const { Client, Registry, StepList, deadEnd, openBlobCache, rpc, stringCodec } = await import(new URL('../../sdkts/node_modules/@superdurable/dex/dist/src/index.js', import.meta.url).href);

const [serverAddress, flowType, flowId] = process.argv.slice(2);

class AwaitDecision {
  getStepType() { return 'AwaitDecision'; }
  execute() { return deadEnd(); }
}
class DecisionFlow {
  getFlowType() { return flowType; }
  getSteps() { return StepList.startStep(new AwaitDecision()); }
  reject(_context, _reason) { return undefined; }
}
rpc({ inputCodec: stringCodec })(DecisionFlow.prototype.reject, { kind: 'method', name: 'reject' });

const flow = new DecisionFlow();
const directory = await mkdtemp(join(tmpdir(), 'sdkpython-cross-language-'));
const cache = openBlobCache({ directory, maxBytes: 16 * 1024 * 1024 });
const client = new Client(new Registry([flow]), cache, { serverAddress });
const records = [];
const logger = { isEnabled: () => true, log: (level, message, attributes) => { records.push({ level, message, attributes }); process.stderr.write(`${JSON.stringify({ level, message, attributes })}\n`); } };
try {
  let attempts = 0;
  const target = newDexRPCTriggerTarget(client, flow.reject, () => true, () => flowId, () => 'duplicate request', { logger });
  const counting = { handleTrigger: async (context, event) => { attempts++; await target.handleTrigger(context, event); } };
  await deliverTrigger(newTriggerContext(), counting, { id: `Ev-${randomUUID()}`, occurredAt: new Date(), payload: {} }, { logger });
  assert.equal(attempts, 1, 'the Python handler marker was consumed after one attempt');
  const skipped = records.find(record => record.message === 'trigger event skipped: undeliverable');
  assert.deepEqual(skipped?.attributes.error_cause_type, 'WorkerInvocationError');
  process.stdout.write(`${JSON.stringify({ attempts, records })}\n`);
} finally {
  await client.close();
  cache.close();
  await rm(directory, { recursive: true, force: true });
}
