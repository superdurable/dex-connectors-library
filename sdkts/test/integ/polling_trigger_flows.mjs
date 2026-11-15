// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Real Dex integration tests of the poll Trigger kit, mirroring sdkgo/integrationtest/polling_trigger_*.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import process from 'node:process';
import { Client, Registry, StepList, Worker, forceComplete, openBlobCache } from '@superdurable/dex';
import { newTriggerContext } from '../../dist/index.js';
import { InMemoryObjectStore, TriggerInbox, newDurableTriggerTarget, newPollFeedCursorStore } from '../../dist/projectconfig/index.js';
import { newFeedMember, newFeedPoller } from '../../dist/pollingtrigger/index.js';
import { ChangeLogFetcher, ChangeLogProvider, PollEnvironment, commitHookCursor, eventually, newLogRecorder, pollConnection, pollDefinition, pollScope, waitForItemFlow } from './polling_trigger_support.mjs';

const serverAddress = process.env.DEX_SERVER_ADDRESS ?? '127.0.0.1:18891';
const suffix = randomUUID().replaceAll('-', '');

class StartPollItem {
  getStepType() { return 'StartPollItem'; }
  execute(_context, input) { return forceComplete(input); }
}
class PollItemFlow {
  getFlowType() { return `SdktsPollItem${suffix}`; }
  getSteps() { return StepList.startStep(new StartPollItem()); }
}

const itemFlow = new PollItemFlow();
const registry = new Registry([itemFlow]);
const directory = await mkdtemp(join(tmpdir(), 'sdkts-poll-trigger-integration-'));
const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
const client = new Client(registry, cache, { serverAddress });
let worker;
const startWorker = async () => {
  worker = new Worker(registry, cache, { serverAddress });
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000 });
};
const stopWorker = async () => { await worker.close(); worker = undefined; };

let runCount = 0;
/** Run one scenario with its own provider, project storage, and Flow IDs. */
async function scenario(name, test) {
  const provider = new ChangeLogProvider();
  await provider.start();
  const environment = new PollEnvironment({ client, itemFlow, objects: new InMemoryObjectStore(), provider, logs: newLogRecorder(), testRunId: `${suffix}-${++runCount}` });
  const processes = [];
  const original = environment.startProcess.bind(environment);
  environment.startProcess = options => { const started = original(options); processes.push(started); return started; };
  try {
    await test(environment);
    process.stdout.write(`ok ${name}\n`);
  } finally {
    // Cleanup stops every process; a scenario already asserted how each one ended.
    for (const started of processes) await started.stop().catch(() => undefined);
    await provider.close();
  }
}

try {
  await startWorker();

  // R1 and R2: no backlog on the first start, and changes that arrive while the Worker and the process are
  // down each start one Flow.
  await scenario('delivers only new changes across restarts', async environment => {
    const { provider } = environment;
    const backlog = provider.appendItems(`backlog-${environment.testRunId}`, 5);
    const first = environment.startProcess();
    await environment.waitForSeed();
    const fresh = provider.appendItems(`fresh-${environment.testRunId}`, 3);
    for (const [index, itemId] of fresh.entries()) {
      assert.deepEqual(await waitForItemFlow(client, environment.itemFlowId(itemId, 6 + index)), { itemId, sequence: 6 + index });
    }
    for (const [index, itemId] of backlog.entries()) await assert.rejects(client.describeFlow(environment.itemFlowId(itemId, 1 + index)), `backlog item ${itemId} must not start a Flow`);
    await first.stop();

    await stopWorker();
    const offline = provider.appendItems(`offline-${environment.testRunId}`, 4);
    for (let restart = 0; restart < 2; restart++) {
      const restarted = environment.startProcess();
      await eventually(async () => (await environment.cursorState('main')).token === '12', 'the restarted process records the offline changes');
      await restarted.stop();
    }
    const commitsWhileDown = (await environment.cursorState('main')).commitCount;
    await startWorker();
    environment.startProcess();
    for (const [index, itemId] of offline.entries()) {
      assert.deepEqual(await waitForItemFlow(client, environment.itemFlowId(itemId, 9 + index)), { itemId, sequence: 9 + index });
    }
    await eventually(async () => (await environment.pendingEvents('main')).length === 0, 'the inbox drains');
    assert.equal((await environment.cursorState('main')).commitCount, commitsWhileDown, 'restarts and empty polls write no commits');
    assert.equal(environment.logs.find('poll cursor seeded', { binding: 'main' }).length, 1);
  });

  // R3 and R4: a crash between record and commit refetches from the old cursor; a crash after the commit
  // replays the inbox and never refetches the committed page.
  for (const crash of ['crash between record and commit', 'crash after commit before delivery']) {
    await scenario(crash, async environment => {
      await environment.seedAndStop();
      const itemIds = environment.provider.appendItems(`crash-${environment.testRunId}`, 3);
      const crashed = environment.startProcess({ wrapCursor: (cursor, abort) => commitHookCursor(cursor, crash === 'crash between record and commit'
        ? { beforeCommit: () => { abort(); throw new Error('simulated process crash'); } }
        : { afterCommit: abort }) });
      // The simulated crash aborts the process from inside the commit.
      assert.equal((await crashed.result)?.name, 'TriggerDeliveryAbortedError');
      assert.equal((await environment.pendingEvents('main')).length, 3, 'every recorded event survives the crash');
      const requestsBeforeRestart = environment.provider.changeRequests().length;

      environment.startProcess();
      for (const [index, itemId] of itemIds.entries()) {
        assert.deepEqual(await waitForItemFlow(client, environment.itemFlowId(itemId, 1 + index)), { itemId, sequence: 1 + index });
        assert.ok(environment.deliveries(`${itemId}@${1 + index}`) >= 1);
      }
      await eventually(async () => (await environment.pendingEvents('main')).length === 0, 'the inbox drains');
      await eventually(() => environment.provider.changeRequests().length > requestsBeforeRestart, 'the restarted process polls');
      const afterRestart = environment.provider.changeRequests().slice(requestsBeforeRestart);
      if (crash === 'crash after commit before delivery') {
        for (const request of afterRestart) assert.equal(request.after, '3', 'the committed page is never fetched again');
      } else {
        assert.equal(afterRestart[0].after, '0', 'an uncommitted page is fetched again from the old cursor');
      }
    });
  }

  // R5: a 410 reseeds and the gap is logged, never delivered.
  await scenario('reseeds an expired cursor', async environment => {
    const { provider } = environment;
    provider.appendItems(`before-${environment.testRunId}`, 3);
    await environment.seedAndStop();
    provider.truncate();
    const gap = provider.appendItems(`gap-${environment.testRunId}`, 2);
    environment.startProcess();
    await eventually(async () => (await environment.cursorState('main')).resetCount === 1, 'the cursor is reseeded');
    const after = provider.appendItems(`after-${environment.testRunId}`, 2);
    for (const [index, itemId] of after.entries()) await waitForItemFlow(client, environment.itemFlowId(itemId, 3 + index));
    for (const [index, itemId] of gap.entries()) assert.equal(environment.deliveries(`${itemId}@${1 + index}`), 0);
    const reset = environment.logs.find('poll cursor invalid; reseeding', { binding: 'main' });
    assert.equal(reset.length, 1);
    assert.equal(reset[0].level, 'warn');
    assert.equal(reset[0].attributes.reset_count, 1);
    assert.notEqual((await environment.cursorState('main')).lastResetAtUnixMs, undefined);
  });

  // R6: the next provider request waits for Retry-After; elapsed time is the behavior under test.
  await scenario('honors Retry-After', async environment => {
    const { provider } = environment;
    await environment.seedAndStop();
    provider.script({ status: 429, retryAfter: '3' });
    const [itemId] = provider.appendItems(`limited-${environment.testRunId}`, 1);
    const requestsBeforeStart = provider.changeRequests().length;
    environment.startProcess();
    await waitForItemFlow(client, environment.itemFlowId(itemId, 1));
    const requests = provider.changeRequests().slice(requestsBeforeStart);
    assert.ok(requests.length >= 2);
    assert.ok(requests[1].at - requests[0].at >= 3000, `the retry waited ${requests[1].at - requests[0].at} ms`);
    const paused = environment.logs.find('poll rate limited; pausing connection', { binding: 'main' });
    assert.equal(paused.length, 1);
    assert.equal(paused[0].attributes.delay_ms, 3000);
  });

  // R7: two replicas share one cursor and never lose or regress.
  await scenario('replicas share one cursor', async environment => {
    await environment.seedAndStop();
    const commitCounts = [];
    const observe = cursor => ({
      readPollCursor: () => cursor.readPollCursor(),
      async replacePollCursor(revision, state) {
        const record = await cursor.replacePollCursor(revision, state);
        commitCounts.push(state.commitCount);
        return record;
      },
    });
    environment.startProcess({ pageSize: 10, wrapCursor: observe });
    environment.startProcess({ pageSize: 10, wrapCursor: observe });
    const itemIds = environment.provider.appendItems(`replica-${environment.testRunId}`, 50);
    for (const [index, itemId] of itemIds.entries()) await waitForItemFlow(client, environment.itemFlowId(itemId, 1 + index));
    await eventually(async () => (await environment.pendingEvents('main')).length === 0, 'the inbox drains');
    assert.equal((await environment.cursorState('main')).token, '50');
    assert.equal(new Set(commitCounts).size, commitCounts.length, `every commit count is written once: ${commitCounts}`);
  });

  // R8: one feed serves a durable Flow member and a non-durable member, each receiving exactly its events.
  await scenario('a feed serves two bindings', async environment => {
    const key = environment.inboxKey('odd');
    const durable = newDurableTriggerTarget(new TriggerInbox(environment.objects, pollScope, key), key, environment.flowTarget(), { logger: environment.logs.logger });
    const selectSequences = isOdd => change => (change.payload.sequence % 2 === 1) === isOdd ? [change] : [];
    const oddMember = newFeedMember(pollDefinition, { connection: pollConnection, trigger: pollDefinition.trigger, name: 'odd' }, selectSequences(true), durable);
    const evenEvents = [];
    const evenMember = newFeedMember(pollDefinition, { connection: pollConnection, trigger: pollDefinition.trigger, name: 'even' }, selectSequences(false), { handleTrigger: async (_context, event) => { evenEvents.push(event.id); } });
    const feedCursor = newPollFeedCursorStore(environment.objects, pollScope, { connectorId: 'changelog', connectionName: pollConnection.name, feedName: 'itemFeed' });
    const poller = newFeedPoller({ name: 'itemFeed', cursor: feedCursor, fetcher: new ChangeLogFetcher(environment.provider.baseURL, 100), members: [oddMember, evenMember] });
    const controller = new globalThis.AbortController();
    const result = environment.newRunner(poller).run(newTriggerContext(controller.signal)).then(() => undefined, error => error);
    try {
      await eventually(async () => (await feedCursor.readPollCursor()).state, 'the feed seeds', 20_000);
      const itemIds = environment.provider.appendItems(`feed-${environment.testRunId}`, 4);
      for (const sequence of [1, 3]) await waitForItemFlow(client, environment.itemFlowId(itemIds[sequence - 1], sequence));
      await eventually(() => evenEvents.length === 2, 'the even member receives its events');
      assert.deepEqual(evenEvents, [`${itemIds[1]}@2`, `${itemIds[3]}@4`]);
      for (const sequence of [2, 4]) assert.equal(environment.deliveries(`${itemIds[sequence - 1]}@${sequence}`), 0);
    } finally {
      controller.abort();
      assert.equal((await result)?.name, 'TriggerDeliveryAbortedError');
    }
    assert.equal(evenEvents.length, 2);
  });

  process.stdout.write('sdkts poll Trigger integration passed\n');
} finally {
  await client.close();
  if (worker) await worker.close();
  cache.close();
  await rm(directory, { recursive: true, force: true });
}
