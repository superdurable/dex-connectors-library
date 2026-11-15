// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Client } from '@superdurable/dex';
import type { TriggerLogger } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import { ConnectionStore, CredentialMaterial, InMemoryObjectStore, LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { expect, it } from 'vitest';
import { runInboundMessageWatcher } from '../../examples/message-received/app.js';
import { ConnectionName, MessageReceivedTriggerBinding, type InboundMessageOutcome } from '../../examples/message-received/flow/workflow.js';
import { TwilioMessagesFake } from '../twilio_messages_fake.js';

// Split literals keep synthetic SIDs from matching repository secret scanning.
const accountSid = 'AC' + '0123456789abcdef0123456789abcdef';
const authToken = 'integration-auth-token-0123456789';
const supportNumber = '+14155550100';
const salesNumber = '+14155550199';
const customerNumber = '+14155550123';
const scope = { projectId: 'twilio-message-received', kind: 'live' } as const;
const inboxKey = { connectorId: 'twilio-messaging', connectionName: ConnectionName, triggerName: 'messageReceived', bindingName: MessageReceivedTriggerBinding };

const dexAddress = () => {
  const address = process.env.DEX_FLOW_SERVICE_ADDRESS;
  if (!address) throw new Error('set DEX_FLOW_SERVICE_ADDRESS to a running dexcli dev server');
  return address;
};

/** Keeps the Flow ID of every "trigger event delivered" record, with whether the start was a duplicate. */
function newDeliveryLogger(): TriggerLogger & { deliveries: Array<{ flowId: string; isDuplicate: boolean }> } {
  const deliveries: Array<{ flowId: string; isDuplicate: boolean }> = [];
  return {
    deliveries, isEnabled: () => true,
    log: (_level, message, attributes) => { if (message === 'trigger event delivered') deliveries.push({ flowId: String(attributes.flow_id), isDuplicate: attributes.duplicate === true }); },
  };
}
const startedFlowIds = (logger: ReturnType<typeof newDeliveryLogger>) => logger.deliveries.filter(delivery => !delivery.isDuplicate).map(delivery => delivery.flowId);

/** Project storage that survives the application process; receivingNumber is read on every project load. */
async function newMessageReceivedProject() {
  const fake = new TwilioMessagesFake(accountSid, accountSid, authToken);
  const objects = new InMemoryObjectStore();
  await new ConnectionStore(objects, scope).replaceCredential({ connectorId: 'twilio-messaging', connectionName: ConnectionName }, 0,
    new CredentialMaterial({ auth_method: 'auth-token', auth_token: authToken }, undefined, 'auth-token'));
  const environment = {
    fake, receivingNumber: supportNumber,
    project: () => new LoadedProject({
      schemaVersion: 'connectors.dex.dev/project-configuration/v1alpha2', scope, revision: 1, operationConfigurations: [],
      connections: [{ connectorId: 'twilio-messaging', connectionName: ConnectionName, modulePath: 'twilio-messaging', provider: 'twilio', configuration: { accountSid, endpoint: 'http://127.0.0.1:24212/2010-04-01', pollInterval: 5_000_000_000 } }],
      triggerBindings: [{ connectorId: 'twilio-messaging', connectionName: ConnectionName, triggerName: 'messageReceived', bindingName: MessageReceivedTriggerBinding, configuration: { to: environment.receivingNumber } }],
    }, undefined, objects, undefined, undefined),
    cursorState: async () => (await environment.project().pollCursorStore(inboxKey).readPollCursor()).state,
    waitForSeed: async () => { await expect.poll(async () => (await environment.cursorState()) !== undefined, { timeout: 30_000 }).toBe(true); },
  };
  return environment;
}

/** One run of the example application; stopping it models a process exit. */
async function startWatcher(project: LoadedProject, fake: TwilioMessagesFake, logger: TriggerLogger) {
  const stop = new AbortController();
  let resolveReady!: (ready: { client: Client; runner: Runner }) => void;
  const ready = new Promise<{ client: Client; runner: Runner }>(resolve => { resolveReady = resolve; });
  const running = runInboundMessageWatcher(project, { serverAddress: dexAddress(), logger, twilioOptions: { send: fake.send }, onReady: resolveReady }, stop.signal);
  const { client, runner } = await ready;
  return { client, runner, stop: async () => { stop.abort(); await running; } };
}

/** Wait for every delivered Flow to complete and return the outcomes keyed by message SID. */
async function outcomesOf(client: Client, flowIds: string[]): Promise<Map<string, InboundMessageOutcome>> {
  const outcomes = new Map<string, InboundMessageOutcome>();
  for (const flowId of flowIds) {
    const result = await client.waitForFlow(flowId, 60_000);
    expect(result.status, result.errorMessage).toBe('completed');
    const outcome = result.singleOutput<InboundMessageOutcome>();
    outcomes.set(outcome.messageSid, outcome);
  }
  return outcomes;
}

it('starts one Flow per inbound message after the baseline, and delivers messages received while stopped after a restart', async () => {
  const environment = await newMessageReceivedProject();
  const { fake } = environment;
  fake.receive(customerNumber, supportNumber, 'before the baseline', new Date(Date.now() - 30_000));
  const logger = newDeliveryLogger();
  const first = await startWatcher(environment.project(), fake, logger);
  try {
    await environment.waitForSeed();
    const hello = fake.receive(customerNumber, supportNumber, 'hello', new Date());
    const stop = fake.receive(customerNumber, supportNumber, 'STOP', new Date());
    fake.sendOutbound(supportNumber, customerNumber, 'outbound reply', new Date());
    fake.receive(customerNumber, salesNumber, 'to sales', new Date());
    first.runner.wakePoller(MessageReceivedTriggerBinding);
    await expect.poll(() => startedFlowIds(logger).length, { timeout: 30_000 }).toBe(2);
    const outcomes = await outcomesOf(first.client, startedFlowIds(logger));
    expect(outcomes.get(hello)?.current.status, 'the Flow reads the message back from Twilio').toBe('received');
    expect([outcomes.get(hello)?.from, outcomes.get(hello)?.isOptOut, outcomes.get(stop)?.isOptOut]).toEqual([customerNumber, false, true]);
  } finally { await first.stop(); }

  const offline = fake.receive(customerNumber, supportNumber, 'sent while stopped', new Date());
  const second = await startWatcher(environment.project(), fake, logger);
  try {
    second.runner.wakePoller(MessageReceivedTriggerBinding);
    await expect.poll(() => startedFlowIds(logger).length, { timeout: 30_000 }).toBe(3);
    const outcomes = await outcomesOf(second.client, startedFlowIds(logger).slice(2));
    expect(outcomes.get(offline)?.to).toBe(supportNumber);
    // Two more polls finish every delivery the first one started.
    for (let poll = 0; poll < 2; poll++) {
      const listed = fake.listRequests.length;
      second.runner.wakePoller(MessageReceivedTriggerBinding);
      await expect.poll(() => fake.listRequests.length, { timeout: 30_000 }).toBeGreaterThan(listed);
    }
    expect(new Set(startedFlowIds(logger)).size, 'no message is delivered twice and the baseline is never delivered').toBe(3);
    expect(logger.deliveries.filter(delivery => delivery.isDuplicate)).toEqual([]);
  } finally { await second.stop(); }
}, 120_000);

it('pauses on a 429 and holds an MMS still in receiving until Twilio reports received', async () => {
  const environment = await newMessageReceivedProject();
  const { fake } = environment;
  const logger = newDeliveryLogger();
  const watcher = await startWatcher(environment.project(), fake, logger);
  try {
    await environment.waitForSeed();
    const photo = fake.receive(customerNumber, supportNumber, 'photo', new Date(), 'receiving');
    fake.throttleNextList(1);
    const requestsBefore = fake.listRequests.length;
    watcher.runner.wakePoller(MessageReceivedTriggerBinding);
    await expect.poll(() => fake.listRequests.length, { timeout: 30_000 }).toBeGreaterThanOrEqual(requestsBefore + 2);
    expect(startedFlowIds(logger), 'a message still in receiving is held').toEqual([]);
    fake.setStatus(photo, 'received');
    watcher.runner.wakePoller(MessageReceivedTriggerBinding);
    await expect.poll(() => startedFlowIds(logger).length, { timeout: 30_000 }).toBe(1);
    const outcomes = await outcomesOf(watcher.client, startedFlowIds(logger));
    expect(outcomes.get(photo)?.current.status).toBe('received');
  } finally { await watcher.stop(); }
}, 120_000);

it('records a new baseline for a changed receiving number without delivering the gap', async () => {
  const environment = await newMessageReceivedProject();
  const { fake } = environment;
  const logger = newDeliveryLogger();
  const first = await startWatcher(environment.project(), fake, logger);
  try { await environment.waitForSeed(); } finally { await first.stop(); }

  fake.receive(customerNumber, salesNumber, 'before the new baseline', new Date());
  environment.receivingNumber = salesNumber;
  const second = await startWatcher(environment.project(), fake, logger);
  try {
    await expect.poll(async () => (await environment.cursorState())?.resetCount, { timeout: 30_000 }).toBe(1);
    const after = fake.receive(customerNumber, salesNumber, 'after the reset', new Date());
    second.runner.wakePoller(MessageReceivedTriggerBinding);
    await expect.poll(() => startedFlowIds(logger).length, { timeout: 30_000 }).toBe(1);
    const outcomes = await outcomesOf(second.client, startedFlowIds(logger));
    expect(outcomes.get(after)?.to).toBe(salesNumber);
    expect(outcomes.size, 'messages before the new baseline are not delivered').toBe(1);
  } finally { await second.stop(); }
}, 120_000);
