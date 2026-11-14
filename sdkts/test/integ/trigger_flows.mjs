// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { execFileSync } from 'node:child_process';
import { once } from 'node:events';
import { chmod, mkdtemp, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import process from 'node:process';
import { setTimeout } from 'node:timers/promises';
import { URL, fileURLToPath } from 'node:url';
import { Buffer } from 'node:buffer';
import { Attribute, Client, Registry, StepList, Worker, deadEnd, doubleCodec, forceComplete, jsonCodec, openBlobCache, rpc } from '@superdurable/dex';
import {
  StaticCredentialProvider, TriggerRPCRequestConflictError, UndeliverableTriggerError, deliverTrigger, isTriggerUndeliverable, newDexFlowTriggerTarget, newDexRPCTriggerTarget,
  newTrigger, newTriggerContext, triggerRPCRequestId,
} from '../../dist/index.js';
import { InMemoryObjectStore, TriggerInbox, loadFromEnvironment, newDurableTriggerTarget } from '../../dist/projectconfig/index.js';
import { RelaySignatureHeader, RelayTimestampHeader, signRelayedRequest, verifyRelaySignature } from '../../dist/relaysignature/index.js';
import { WebhookEndpoint } from '../../dist/webhooktrigger/index.js';

const serverAddress = process.env.DEX_SERVER_ADDRESS ?? '127.0.0.1:18891';
const suffix = randomUUID().replaceAll('-', '');
const quietLogger = { isEnabled: () => false, log: () => undefined };

class StartItem {
  getStepType() { return 'StartItem'; }
  execute(_context, input) { return forceComplete(input); }
}
class ItemFlow {
  getFlowType() { return `SdktsTriggerItem${suffix}`; }
  getSteps() { return StepList.startStep(new StartItem()); }
}

const receivedReplies = [];
class AwaitReply {
  getStepType() { return 'AwaitReply'; }
  execute() { return deadEnd(); }
}
class ApprovalFlow {
  getFlowType() { return `SdktsTriggerApproval${suffix}`; }
  getSteps() { return StepList.startStep(new AwaitReply()); }
  receiveReply(_context, input) {
    if (input.kind === 'reject') throw new UndeliverableTriggerError(new Error('approval already decided'));
    if (input.kind === 'notReady') throw new Error('approval is still loading');
    const isDuplicate = receivedReplies.includes(input.eventId);
    if (!isDuplicate) receivedReplies.push(input.eventId);
    return { output: { isDuplicate } };
  }
}
rpc()(ApprovalFlow.prototype.receiveReply, { kind: 'method', name: 'receiveReply' });

// Counts handler runs per RPC; the Worker runs in this process.
const ledgerHandlerCalls = { recordTransactional: 0, recordNonTransactional: 0, recordDefaultedAmount: 0, incrementTotal: 0, recordWired: 0 };
// Each queued gate holds one recordGated run in flight until the test settles it.
const ledgerGates = [];
const ledgerTotal = new Attribute('total', doubleCodec);
class AwaitLedgerEntries {
  getStepType() { return 'AwaitLedgerEntries'; }
  execute() { return deadEnd(); }
}
class LedgerFlow {
  getFlowType() { return `SdktsTriggerLedger${suffix}`; }
  getSteps() { return StepList.startStep(new AwaitLedgerEntries()); }
  getPersistenceSchema() { return { attributes: [ledgerTotal] }; }
  recordTransactional(context, input) { ledgerHandlerCalls.recordTransactional++; return this.addToTotal(context, input); }
  recordNonTransactional(context, input) { ledgerHandlerCalls.recordNonTransactional++; return this.addToTotal(context, input); }
  // A default-valued input parameter leaves length 1, so the Client reads the input through its inputCodec.
  recordDefaultedAmount(context, input = { amount: 1 }) { ledgerHandlerCalls.recordDefaultedAmount++; return this.addToTotal(context, input); }
  incrementTotal(context) { ledgerHandlerCalls.incrementTotal++; return this.addToTotal(context, { amount: 1 }); }
  recordWired(context, input) { ledgerHandlerCalls.recordWired++; return this.addToTotal(context, input); }
  async recordGated(context, input) {
    const gate = ledgerGates.shift();
    if (gate) { gate.entered.resolve(); await gate.settled.promise; }
    return this.addToTotal(context, input);
  }
  getTotal(context) { return { output: ledgerTotal.get(context) ?? 0 }; }
  addToTotal(context, input) {
    const total = (ledgerTotal.get(context) ?? 0) + input.amount;
    ledgerTotal.set(context, total);
    return { output: total };
  }
}
rpc({ outputCodec: doubleCodec, isTransactional: true })(LedgerFlow.prototype.recordTransactional, { kind: 'method', name: 'recordTransactional' });
rpc({ outputCodec: doubleCodec })(LedgerFlow.prototype.recordNonTransactional, { kind: 'method', name: 'recordNonTransactional' });
rpc({ inputCodec: jsonCodec(), outputCodec: doubleCodec, isTransactional: true })(LedgerFlow.prototype.recordDefaultedAmount, { kind: 'method', name: 'recordDefaultedAmount' });
rpc({ outputCodec: doubleCodec, isTransactional: true })(LedgerFlow.prototype.incrementTotal, { kind: 'method', name: 'incrementTotal' });
rpc({ name: 'recordLedgerEntry', outputCodec: doubleCodec, isTransactional: true })(LedgerFlow.prototype.recordWired, { kind: 'method', name: 'recordWired' });
rpc({ outputCodec: doubleCodec, isTransactional: true, timeoutMs: 30_000 })(LedgerFlow.prototype.recordGated, { kind: 'method', name: 'recordGated' });
rpc({ outputCodec: doubleCodec })(LedgerFlow.prototype.getTotal, { kind: 'method', name: 'getTotal' });

const itemFlow = new ItemFlow();
const approvalFlow = new ApprovalFlow();
const ledgerFlow = new LedgerFlow();
const registry = new Registry([itemFlow, approvalFlow, ledgerFlow]);
const directory = await mkdtemp(join(tmpdir(), 'sdkts-trigger-integration-'));
const directories = [directory];
const cache = openBlobCache({ directory, maxBytes: 64 * 1024 * 1024 });
const worker = new Worker(registry, cache, { serverAddress });
const client = new Client(registry, cache, { serverAddress });
const event = (id, payload) => ({ id, occurredAt: new Date(), payload });

try {
  await worker.start();
  await worker.waitPullRegistered({ timeoutMs: 30_000 });

  // A repeated event starts one Flow: the event ID is the request ID and Flow ID reuse is disallowed.
  const startTarget = newDexFlowTriggerTarget(client, itemFlow, received => received.payload.channel === 'C1', received => `item-${suffix}-${received.payload.thread}`, received => received.payload, { logger: quietLogger });
  const first = event(`Ev-${suffix}-1`, { channel: 'C1', thread: '1.0' });
  await deliverTrigger(newTriggerContext(), startTarget, first, { logger: quietLogger });
  const startedRun = (await client.describeFlow(`item-${suffix}-1.0`)).runId;
  await deliverTrigger(newTriggerContext(), startTarget, first, { logger: quietLogger });
  await deliverTrigger(newTriggerContext(), startTarget, event(`Ev-${suffix}-other`, { channel: 'C1', thread: '1.0' }), { logger: quietLogger });
  const completed = await client.waitForFlow(`item-${suffix}-1.0`, 60_000);
  assert.equal(completed.status, 'completed');
  assert.equal((await client.describeFlow(`item-${suffix}-1.0`)).runId, startedRun, 'a duplicate start never creates a second run');
  await startTarget.handleTrigger(newTriggerContext(), event(`Ev-${suffix}-filtered`, { channel: 'C9', thread: '2.0' }));
  await assert.rejects(client.describeFlow(`item-${suffix}-2.0`), 'a filtered event starts no Flow');

  // The RPC target delivers to the running Flow and classifies handler and lifecycle failures.
  const approvalId = `approval-${suffix}`;
  await client.startFlow(approvalFlow, approvalId, {});
  const replyTarget = newDexRPCTriggerTarget(client, approvalFlow.receiveReply, () => true, received => received.payload.flowId, received => ({ eventId: received.id, kind: received.payload.kind }), { logger: quietLogger });
  const reply = event(`Ev-${suffix}-reply`, { flowId: approvalId, kind: 'approve' });
  await replyTarget.handleTrigger(newTriggerContext(), reply);
  await replyTarget.handleTrigger(newTriggerContext(), reply);
  assert.deepEqual(receivedReplies, [reply.id], 'the RPC treats a repeated event ID as a duplicate');
  await assert.rejects(replyTarget.handleTrigger(newTriggerContext(), event(`Ev-${suffix}-rejected`, { flowId: approvalId, kind: 'reject' })), error => isTriggerUndeliverable(error), 'a handler marker is undeliverable');
  await assert.rejects(replyTarget.handleTrigger(newTriggerContext(), event(`Ev-${suffix}-early`, { flowId: approvalId, kind: 'notReady' })), error => !isTriggerUndeliverable(error), 'another handler error stays retryable');
  await assert.rejects(replyTarget.handleTrigger(newTriggerContext(), event(`Ev-${suffix}-orphan`, { flowId: `missing-${suffix}`, kind: 'approve' })), error => isTriggerUndeliverable(error), 'a never-started Flow is undeliverable');

  // Every redelivery of an event carries one request ID, so Dex applies its Flow changes once.
  const ledgerId = `ledger-${suffix}`;
  await client.startFlow(ledgerFlow, ledgerId, {});
  const toLedgerEntry = received => ({ amount: received.payload.amount });
  const transactionalTarget = newDexRPCTriggerTarget(client, ledgerFlow.recordTransactional, () => true, () => ledgerId, toLedgerEntry, { logger: quietLogger });
  const nonTransactionalTarget = newDexRPCTriggerTarget(client, ledgerFlow.recordNonTransactional, () => true, () => ledgerId, toLedgerEntry, { logger: quietLogger });
  const payment = event(`Ev-${suffix}-payment`, { amount: 5 });
  await deliverTrigger(newTriggerContext(), transactionalTarget, payment, { logger: quietLogger });
  await deliverTrigger(newTriggerContext(), transactionalTarget, payment, { logger: quietLogger });
  assert.equal(ledgerHandlerCalls.recordTransactional, 1, 'a transactional repeat is answered from the recorded reply');
  assert.equal(await client.invokeRPC(ledgerFlow.getTotal, ledgerId), 5, 'a transactional redelivery applies once');
  // A mapper whose input changes on every call, as one reading the clock would, applies its first delivery
  // once. A redelivery maps the event to a different request, which Dex refuses for the event's one request
  // ID; the target logs the drop at ERROR and the durable inbox consumes the event as undeliverable.
  const bonus = event(`Ev-${suffix}-bonus`, { amount: 3 });
  let bonusMappings = 0;
  const toChangingBonus = received => ({ amount: received.payload.amount, mappedAt: ++bonusMappings });
  const conflictRecords = [];
  const conflictLogger = { isEnabled: () => true, log: (level, message, attributes) => { conflictRecords.push({ level, message, attributes }); } };
  const changingTarget = newDexRPCTriggerTarget(client, ledgerFlow.recordTransactional, () => true, () => ledgerId, toChangingBonus, { logger: conflictLogger });
  const bonusKey = { connectorId: 'sample', connectionName: 'workspace', triggerName: 'bonusCreated', bindingName: 'bonus' };
  const bonusInbox = new TriggerInbox(new InMemoryObjectStore(), { projectId: 'bonus', kind: 'live' }, bonusKey);
  const durableChangingTarget = newDurableTriggerTarget(bonusInbox, bonusKey, changingTarget, { logger: quietLogger });
  await durableChangingTarget.prepareTrigger(newTriggerContext(), bonus);
  await durableChangingTarget.handleTrigger(newTriggerContext(), bonus);
  assert.equal(ledgerHandlerCalls.recordTransactional, 2, 'the first changing delivery ran the handler');
  assert.equal(await client.invokeRPC(ledgerFlow.getTotal, ledgerId), 8, 'the first changing delivery applied once');
  await durableChangingTarget.prepareTrigger(newTriggerContext(), bonus);
  await durableChangingTarget.handleTrigger(newTriggerContext(), bonus);
  assert.deepEqual(await bonusInbox.pending(), [], 'the conflicting redelivery was consumed on its first attempt');
  const dropRecords = conflictRecords.filter(record => record.message === 'trigger rpc request conflict; event dropped');
  assert.equal(dropRecords.length, 1);
  assert.equal(dropRecords[0].level, 'error');
  assert.deepEqual(dropRecords[0].attributes, { target: 'rpc', event_id: bonus.id, request_id: triggerRPCRequestId('recordTransactional', bonus.id), rpc_name: 'recordTransactional', flow_id: ledgerId });
  const bonusRejection = await changingTarget.handleTrigger(newTriggerContext(), bonus).then(() => undefined, error => error);
  assert.ok(bonusRejection instanceof TriggerRPCRequestConflictError && isTriggerUndeliverable(bonusRejection), `a changed redelivery is an undeliverable conflict: ${bonusRejection}`);
  assert.match(bonusRejection.cause.detail, /was already used for a different request/);
  assert.equal(ledgerHandlerCalls.recordTransactional, 2, 'no changed redelivery ran the handler');
  assert.equal(await client.invokeRPC(ledgerFlow.getTotal, ledgerId), 8, 'the bonus applied once');

  // A real Client accepts the request ID in each RPC shape: a default-valued input with an inputCodec, and
  // an input-free RPC without a mapper, and both apply a redelivery once.
  const fee = event(`Ev-${suffix}-fee`, { amount: 2 });
  const defaultedTarget = newDexRPCTriggerTarget(client, ledgerFlow.recordDefaultedAmount, () => true, () => ledgerId, toLedgerEntry, { logger: quietLogger });
  await deliverTrigger(newTriggerContext(), defaultedTarget, fee, { logger: quietLogger });
  await deliverTrigger(newTriggerContext(), defaultedTarget, fee, { logger: quietLogger });
  assert.equal(ledgerHandlerCalls.recordDefaultedAmount, 1, 'a defaulted-input repeat is answered from the recorded reply');
  const tick = event(`Ev-${suffix}-tick`, {});
  const inputFreeTarget = newDexRPCTriggerTarget(client, ledgerFlow.incrementTotal, () => true, () => ledgerId, undefined, { logger: quietLogger });
  await deliverTrigger(newTriggerContext(), inputFreeTarget, tick, { logger: quietLogger });
  await deliverTrigger(newTriggerContext(), inputFreeTarget, tick, { logger: quietLogger });
  assert.equal(ledgerHandlerCalls.incrementTotal, 1, 'an input-free repeat is answered from the recorded reply');
  assert.equal(await client.invokeRPC(ledgerFlow.getTotal, ledgerId), 11, 'each shape applied its event once');
  // options.rpcName replaces the method name in the request ID, so the wire name keeps IDs stable.
  const wired = event(`Ev-${suffix}-wired`, { amount: 4 });
  const wiredTarget = newDexRPCTriggerTarget(client, ledgerFlow.recordWired, () => true, () => ledgerId, toLedgerEntry, { logger: quietLogger, rpcName: 'recordLedgerEntry' });
  await deliverTrigger(newTriggerContext(), wiredTarget, wired, { logger: quietLogger });
  assert.equal(await client.invokeRPCWithOptions(ledgerFlow.recordWired, ledgerId, { amount: 4 }, { requestId: triggerRPCRequestId('recordLedgerEntry', wired.id) }), 15, 'the wire-name request ID returns the recorded reply');
  assert.equal(ledgerHandlerCalls.recordWired, 1, 'the wire-name request ID matched the target delivery');
  // The same event to another RPC on the same Flow is a different request.
  await deliverTrigger(newTriggerContext(), nonTransactionalTarget, payment, { logger: quietLogger });
  await deliverTrigger(newTriggerContext(), nonTransactionalTarget, payment, { logger: quietLogger });
  assert.equal(ledgerHandlerCalls.recordNonTransactional, 2, 'a non-transactional repeat reruns its handler');
  assert.equal(await client.invokeRPC(ledgerFlow.getTotal, ledgerId), 20, 'each RPC applied the event once');

  // An earlier attempt with another input, such as one a crashed process sent, is still in flight: Dex
  // reports the same conflict, so the target drops the event. When that attempt then fails without
  // committing, this delivery of the event is lost: neither request applied it.
  const deposit = event(`Ev-${suffix}-deposit`, { amount: 7 });
  const gate = { entered: Promise.withResolvers(), settled: Promise.withResolvers() };
  ledgerGates.push(gate);
  const earlierAttempt = client.invokeRPCWithOptions(ledgerFlow.recordGated, ledgerId, { amount: 100 }, { requestId: triggerRPCRequestId('recordGated', deposit.id) });
  await gate.entered.promise;
  const gatedTarget = newDexRPCTriggerTarget(client, ledgerFlow.recordGated, () => true, () => ledgerId, toLedgerEntry, { logger: quietLogger });
  const inFlightRejection = await gatedTarget.handleTrigger(newTriggerContext(), deposit).then(() => undefined, error => error);
  assert.ok(inFlightRejection instanceof TriggerRPCRequestConflictError && isTriggerUndeliverable(inFlightRejection), `an in-flight conflict drops the event: ${inFlightRejection}`);
  assert.match(inFlightRejection.cause.detail, /was already used for a different request/);
  gate.settled.reject(new Error('earlier attempt failed'));
  await assert.rejects(earlierAttempt, 'the earlier attempt failed without applying the event');
  assert.equal(await client.invokeRPC(ledgerFlow.getTotal, ledgerId), 20, 'the dropped deposit was lost when the earlier attempt failed');
  // Only a later redelivery of the same event, such as a provider retry, can still apply it, because no request ID was recorded.
  await deliverTrigger(newTriggerContext(), gatedTarget, deposit, { logger: quietLogger });
  assert.equal(await client.invokeRPC(ledgerFlow.getTotal, ledgerId), 27, 'a later redelivery applied the deposit once');
  await client.stopFlow(ledgerId);

  // A local Dex Web inbox survives a real process restart: another process recorded the event and exited
  // before delivering it, and this process replays it from the files beside connections.json into Dex.
  const projectDirectory = await mkdtemp(join(tmpdir(), 'sdkts-trigger-project-'));
  directories.push(projectDirectory);
  const configurationFile = join(projectDirectory, 'connections.json');
  await writeFile(configurationFile, JSON.stringify({ schemaVersion: 'connectors.dex.dev/local-connections/v1alpha1', connections: [] }), { mode: 0o600 });
  await chmod(configurationFile, 0o600);
  const key = { connectorId: 'sample', connectionName: 'workspace', triggerName: 'itemCreated', bindingName: 'start' };
  const pendingId = `Ev-${suffix}-restart`;
  execFileSync(process.execPath, [fileURLToPath(new URL('./trigger_restart_child.mjs', import.meta.url)), configurationFile, pendingId, suffix], { stdio: 'inherit' });
  assert.equal((await readdir(join(projectDirectory, 'trigger-inboxes'))).length, 1, 'the stopped process left its inbox on disk');
  const restartedProject = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: configurationFile });
  assert.deepEqual((await restartedProject.triggerInbox(key).pending()).map(pending => pending.id), [pendingId]);
  const definition = { trigger: { connectorId: 'sample', triggerName: 'itemCreated' }, description: 'Receive a new item.' };
  const binding = { connection: { provider: 'sample', name: 'workspace' }, trigger: definition.trigger, name: 'start' };
  const restarted = newTrigger({ definition, binding, logger: quietLogger, source: { run: async () => undefined },
    target: newDurableTriggerTarget(restartedProject.triggerInbox(key), key, startTarget, { logger: quietLogger }) });
  await restarted.run(newTriggerContext());
  assert.equal((await client.waitForFlow(`item-${suffix}-restart-${suffix}`, 60_000)).status, 'completed');
  assert.deepEqual(await restartedProject.triggerInbox(key).pending(), []);

  // A relayed request signed with the scope token passes the webhook endpoint, the persisted inbox, and Dex.
  const scopeToken = `scope-token-${suffix}`;
  const endpoint = new WebhookEndpoint({
    connectorId: 'sample', triggerName: 'itemCreated', connection: binding.connection, maxBodyBytes: 1 << 20,
    credentials: new StaticCredentialProvider([[binding.connection, {}]]),
    verifyRequest: async request => {
      const verification = verifyRelaySignature(scopeToken, request.headers, request.body, request.receivedAt);
      if (!verification.isValid) throw new Error(verification.refusal);
    },
    decodeEvent: request => {
      const body = JSON.parse(Buffer.from(request.body).toString('utf8'));
      return { id: body.eventId, occurredAt: request.receivedAt, payload: { channel: body.channel, thread: body.thread } };
    },
  });
  const relayKey = { ...key, bindingName: 'relayed' };
  const relayRunner = newTrigger({ definition, binding: { ...binding, name: 'relayed' }, logger: quietLogger, source: endpoint.newSource(),
    target: newDurableTriggerTarget(restartedProject.triggerInbox(relayKey), relayKey, startTarget, { logger: quietLogger }) });
  const server = createServer((request, response) => { void endpoint.serveHTTP(request, response); });
  server.listen(0, '127.0.0.1');
  await once(server, 'listening');
  const relayStop = new globalThis.AbortController();
  const relayRunning = relayRunner.run(newTriggerContext(relayStop.signal)).catch(error => error);
  try {
    const post = async (token, thread) => {
      const body = Buffer.from(JSON.stringify({ eventId: `Ev-${suffix}-${thread}`, channel: 'C1', thread }));
      const signedAt = new Date();
      const response = await globalThis.fetch(`http://127.0.0.1:${server.address().port}/`, { method: 'POST', body,
        headers: { [RelayTimestampHeader]: String(Math.floor(signedAt.getTime() / 1000)), [RelaySignatureHeader]: signRelayedRequest(token, signedAt, body) } });
      return response.status;
    };
    for (let attempt = 0; endpoint.runningSourceCount() === 0 && attempt < 100; attempt++) await setTimeout(10);
    assert.equal(await post('forged-token', `forged-${suffix}`), 400);
    assert.equal(await post(scopeToken, `relayed-${suffix}`), 200);
    for (let attempt = 0; attempt < 200; attempt++) { try { await client.describeFlow(`item-${suffix}-relayed-${suffix}`); break; } catch { await setTimeout(50); } }
    assert.equal((await client.waitForFlow(`item-${suffix}-relayed-${suffix}`, 60_000)).status, 'completed');
    await assert.rejects(client.describeFlow(`item-${suffix}-forged-${suffix}`), 'a forged request starts no Flow');
    for (let attempt = 0; (await restartedProject.triggerInbox(relayKey).pending()).length > 0 && attempt < 100; attempt++) await setTimeout(50);
    assert.deepEqual(await restartedProject.triggerInbox(relayKey).pending(), []);
  } finally {
    relayStop.abort();
    await relayRunning;
    await new Promise(resolve => server.close(resolve));
  }

  // An inbox the Go Connector SDK wrote is delivered by TypeScript in order and left empty.
  const goKey = { connectorId: 'slack', connectionName: 'support', triggerName: 'threadReplyCreated', bindingName: 'reply' };
  const goObjects = new InMemoryObjectStore();
  const goInbox = new TriggerInbox(goObjects, { projectId: 'fixture', kind: 'live' }, goKey);
  await goObjects.createObject(goInbox.key, await readFile(new URL('../fixtures/go-trigger-inbox-three-events.json', import.meta.url)));
  const goFlowId = received => `go-inbox-${suffix}-${received.id.replace(/[^A-Za-z0-9]/g, '')}`;
  const goTarget = newDexFlowTriggerTarget(client, itemFlow, () => true, goFlowId, received => received.payload, { logger: quietLogger });
  // The fixture holds 9007199254740993, which the default decoder refuses; this decoder keeps it exact as text.
  const keepExactIntegers = payloadJSON => JSON.parse(payloadJSON, (_key, value, context) => typeof value === 'number' && !Number.isSafeInteger(value) && /^-?\d+$/.test(context?.source ?? '') ? context.source : value);
  const goReplay = newDurableTriggerTarget(goInbox, goKey, goTarget, { logger: quietLogger, decodePayload: keepExactIntegers });
  await goReplay.replayTriggerDeliveries(newTriggerContext());
  for (const id of ['Ev1<&>', 'Ev2', 'Ev3']) assert.equal((await client.waitForFlow(goFlowId({ id }), 60_000)).status, 'completed');
  assert.deepEqual((await client.waitForFlow(goFlowId({ id: 'Ev3' }), 60_000)).singleOutput(), { count: '9007199254740993', label: 'big' }, 'the Flow received the exact Go-written integer');
  assert.deepEqual(await goInbox.pending(), []);
  process.stdout.write('sdkts Trigger integration passed\n');
} finally {
  await client.close();
  await worker.close();
  cache.close();
  for (const created of directories) await rm(created, { recursive: true, force: true });
}
