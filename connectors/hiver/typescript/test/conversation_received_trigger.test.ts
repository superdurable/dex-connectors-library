// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// conversationReceived runners, configuration, and client limits, as the Go runner tests check them.
import { SecretString, TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import { newMemoryCursorStore } from '@superdurable/connectors-sdk/pollingtrigger';
import { describe, expect, it } from 'vitest';
import { HiverClient } from '../src/client.js';
import { conversationReceivedPollSchedule } from '../src/conversation_received_trigger.js';
import {
  decodeConversationReceivedEventPayload, newConnectionWithOperations, newConversationReceivedTrigger, newConversationReceivedTriggerRunner, newProjectConnection,
  newProjectConversationReceivedTriggerRunner, validateConversationReceivedTriggerConfiguration, type ConversationReceivedEvent,
} from '../src/index.js';
import { HiverConversationsFake, newFakeConnection, newTestProject } from './hiver_conversations_fake.js';

const inboxId = '105902';
const connectionName = 'hiver-support';
const key = { connectorId: 'hiver', connectionName, triggerName: 'conversationReceived', bindingName: 'new-conversations' };

/** Consumes each event into a list the test awaits. */
class QueueTarget implements TriggerTarget<ConversationReceivedEvent> {
  readonly events: Array<TriggerEvent<ConversationReceivedEvent>> = [];
  async handleTrigger(_context: unknown, event: TriggerEvent<ConversationReceivedEvent>): Promise<void> { this.events.push(event); }
}

async function eventually(condition: () => Promise<boolean> | boolean): Promise<void> {
  const deadline = Date.now() + 10_000;
  while (!(await condition())) { if (Date.now() > deadline) throw new Error('condition did not converge'); await new Promise(resolve => setTimeout(resolve, 10)); }
}

function running(runner: { run(context: ReturnType<typeof newTriggerContext>): Promise<void> }) {
  const stop = new AbortController();
  const result = runner.run(newTriggerContext(stop.signal));
  return { stop: async () => { stop.abort(); await expect(result).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); } };
}

describe('conversationReceived runners', () => {
  it('validates configuration and client limits as Go does', () => {
    expect(() => validateConversationReceivedTriggerConfiguration({ inboxId })).not.toThrow();
    expect(() => validateConversationReceivedTriggerConfiguration({ inboxId: '' })).toThrow('inboxId');
    expect(() => validateConversationReceivedTriggerConfiguration({ inboxId: '../inboxes' })).toThrow();
    const credentials = { resolve: async () => ({ apiKey: new SecretString('k') }) };
    for (const pollPagesPerPoll of [1, 6]) expect(() => new HiverClient({ pollPagesPerPoll }, credentials)).toThrow('pollPagesPerPoll must be from 2 to 5');
    expect(() => new HiverClient({ pollIntervalMs: 1 }, credentials)).toThrow('pollInterval');
    const client = new HiverClient({}, credentials);
    expect([client.pollIntervalMs, client.pollPagesPerPoll]).toEqual([300_000, 3]);
    expect(conversationReceivedPollSchedule(client)).toEqual({ intervalMs: 300_000, maxBackoffMs: 1_800_000 });
    expect(conversationReceivedPollSchedule(new HiverClient({ pollIntervalMs: 3_600_000 }, credentials)).maxBackoffMs).toBe(3_600_000);
  });

  it('rejects incomplete routes and a connection without a HiverClient', () => {
    const connection = newFakeConnection(new HiverConversationsFake(inboxId, 'support@acme.example.com')), target = new QueueTarget(), cursor = newMemoryCursorStore();
    const configuration = { inboxId };
    expect(() => newConversationReceivedTriggerRunner(connection, [])).toThrow();
    expect(() => newConversationReceivedTriggerRunner(connection, [{ bindingName: '', configuration, cursor, target }])).toThrow();
    expect(() => newConversationReceivedTriggerRunner(connection, [{ bindingName: 'a', configuration, cursor: undefined as never, target }])).toThrow();
    expect(() => newConversationReceivedTriggerRunner(connection, [{ bindingName: 'a', configuration, cursor, target: undefined as never }])).toThrow();
    expect(() => newConversationReceivedTriggerRunner(connection, [{ bindingName: 'a', configuration: { inboxId: ' ' }, cursor, target }])).toThrow('inboxId');
    const mock = newConnectionWithOperations({} as Parameters<typeof newConnectionWithOperations>[0], { provider: 'hiver', name: connectionName });
    expect(() => newConversationReceivedTriggerRunner(mock, [{ bindingName: 'a', configuration, cursor, target }])).toThrow('HiverClient');
  });

  it('loads the saved inbox, records each conversation before committing, and drains the inbox', async () => {
    const fake = new HiverConversationsFake(inboxId, 'support@acme.example.com');
    fake.addConversations(inboxId, '573741001', '573741002');
    const { project } = await newTestProject(connectionName, [{ bindingName: 'new-conversations', configuration: { inboxId } }]);
    const connection = newProjectConnection(project, connectionName, { send: fake.send, apiBaseURL: 'https://hiver.example/v1' });
    const target = new QueueTarget();
    const runner = newProjectConversationReceivedTriggerRunner(project, connection, [{ bindingName: 'new-conversations', target }]);
    const cursor = project.pollCursorStore(key);
    const process = running(runner);
    try {
      await eventually(async () => (await cursor.readPollCursor()).state?.token.includes('"baselineComplete":true') ?? false);
      fake.addConversations(inboxId, '573741003');
      runner.wakePoller('new-conversations');
      await eventually(() => target.events.length === 1);
      expect(target.events[0]!.id).toBe('conversation-105902-573741003');
      const inbox = project.triggerInbox(key);
      await eventually(async () => (await inbox.pending()).length === 0);
    } finally { await process.stop(); }
    expect((await cursor.readPollCursor()).state!.snapshot!.entries.map(entry => entry.key)).toEqual(['573741001', '573741002', '573741003']);
    expect(target.events, 'the baseline delivers nothing').toHaveLength(1);
  });

  it('reads connection settings as strictly as Go: pollInterval only as integer nanoseconds', async () => {
    const fake = new HiverConversationsFake(inboxId, 'support@acme.example.com');
    const { project } = await newTestProject(connectionName, [], undefined, { pollInterval: 120_000_000_000, pollPagesPerPoll: 4 });
    const client = newProjectConnection(project, connectionName, { send: fake.send }).operations as HiverClient;
    expect([client.pollIntervalMs, client.pollPagesPerPoll]).toEqual([120_000, 4]);
    for (const configuration of [{ pollInterval: '5m' }, { pollInterval: 1_000_000_000.5 }, { unknownSetting: 1 }]) {
      const { project: rejected } = await newTestProject(connectionName, [], undefined, configuration);
      expect(() => newProjectConnection(rejected, connectionName, { send: fake.send }), JSON.stringify(configuration)).toThrow();
    }
  });

  it('rejects a missing or invalid saved configuration', async () => {
    const fake = new HiverConversationsFake(inboxId, 'support@acme.example.com');
    for (const bindings of [[], [{ bindingName: 'new-conversations', configuration: { inboxId, unknown: true } }], [{ bindingName: 'new-conversations', configuration: {} }]]) {
      const { project } = await newTestProject(connectionName, bindings);
      const connection = newProjectConnection(project, connectionName, { send: fake.send });
      expect(() => newProjectConversationReceivedTriggerRunner(project, connection, [{ bindingName: 'new-conversations', target: new QueueTarget() }])).toThrow('new-conversations');
    }
  });

  it('baselines a single in-memory Trigger and delivers the next conversation', async () => {
    const fake = new HiverConversationsFake(inboxId, 'support@acme.example.com');
    fake.addConversations(inboxId, '573741001');
    const target = new QueueTarget();
    const trigger = newConversationReceivedTrigger({ connection: newFakeConnection(fake, connectionName, { pollIntervalMs: 1000 }), bindingName: 'new-conversations', configuration: { inboxId }, target });
    const process = running(trigger);
    try {
      await eventually(() => fake.conversationListRequests >= 1);
      fake.addConversations(inboxId, '573741002');
      await eventually(() => target.events.length === 1);
      expect(target.events[0]!.payload.conversation.id).toBe('573741002');
    } finally { await process.stop(); }
  });

  it('decodes a Go-written inbox payload', () => {
    const goPayload = '{"inboxId":"105902","conversation":{"id":"573741003","inboxId":"105902","status":"open","assignee":{"type":"user","id":"4401"},"tagIds":["77"],"gmailThreadId":"19cf573741003","privatePermalink":"https://app.hiverhq.com/conversation/573741003"}}';
    const event = decodeConversationReceivedEventPayload(goPayload);
    expect([event.inboxId, event.conversation.id, event.conversation.tagIds]).toEqual([inboxId, '573741003', ['77']]);
    expect(() => decodeConversationReceivedEventPayload('{"inboxId":"105902"}')).toThrow();
  });
});
