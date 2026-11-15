// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// conversationCreated runners, ported from the Go connector's conversation_created_trigger_runner tests.
import { SecretString, StaticCredentialProvider, TriggerDeliveryAbortedError, newTriggerContext, type TriggerEvent, type TriggerLogger, type TriggerTarget } from '@superdurable/connectors-sdk';
import { describe, expect, it } from 'vitest';
import {
  ReamazeClient, conversationCreatedEventId, decodeConversationCreatedEventPayload, newConnection, newConversationCreatedTrigger, newConversationTriggerRunner,
  newProjectConnection, newProjectConversationTriggerRunner, type Connection, type ConversationCreatedEvent,
} from '../src/index.js';
import { FakeEmail, FakeToken, ReamazeConversationFake, newTestProject } from './reamaze_conversations_fake.js';

const connectionName = 'reamaze-brand';
const reference = { provider: 'reamaze', name: connectionName };
const key = { connectorId: 'reamaze', connectionName, triggerName: 'conversationCreated', bindingName: 'newConversations' };
const start = Date.UTC(2026, 2, 2, 9);

/** Consumes each event into a list the test awaits. */
class QueueTarget implements TriggerTarget<ConversationCreatedEvent> {
  readonly events: Array<TriggerEvent<ConversationCreatedEvent>> = [];
  async handleTrigger(_context: unknown, event: TriggerEvent<ConversationCreatedEvent>): Promise<void> { this.events.push(event); }
  async nextEvent(index: number): Promise<TriggerEvent<ConversationCreatedEvent>> {
    await eventually(() => this.events.length > index);
    return this.events[index]!;
  }
}

async function eventually(condition: () => Promise<boolean> | boolean, timeoutMs = 10_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!(await condition())) { if (Date.now() > deadline) throw new Error('condition did not converge'); await new Promise(resolve => setTimeout(resolve, 10)); }
}

function running(runner: { run(context: ReturnType<typeof newTriggerContext>): Promise<void> }) {
  const stop = new AbortController();
  const result = runner.run(newTriggerContext(stop.signal));
  return { stop: async () => { stop.abort(); await expect(result).rejects.toBeInstanceOf(TriggerDeliveryAbortedError); } };
}

function fakeConnection(fake: ReamazeConversationFake, pollIntervalMs = 10_000): Connection {
  const client = new ReamazeClient({ brand: 'acme', pollIntervalMs }, new StaticCredentialProvider([[reference, { email: FakeEmail, apiToken: new SecretString(FakeToken) }]]), { apiBaseURL: fake.baseURL, send: fake.send });
  return newConnection(client, reference);
}

describe('conversationCreated runners', () => {
  it('rejects a poll interval under ten seconds and incomplete routes', () => {
    const credentials = new StaticCredentialProvider([[reference, { email: FakeEmail, apiToken: new SecretString(FakeToken) }]]);
    expect(() => new ReamazeClient({ brand: 'acme', pollIntervalMs: 9_999 }, credentials)).toThrow(/at least 10s/);
    const connection = fakeConnection(new ReamazeConversationFake(FakeEmail, FakeToken)), target = new QueueTarget();
    for (const routes of [[], [{ bindingName: ' ', target }], [{ bindingName: 'a', target: undefined as unknown as QueueTarget }],
      [{ bindingName: 'a', target, configuration: { channel: 'a/b' } }], [{ bindingName: 'a', target }, { bindingName: 'a', target }]]) {
      expect(() => newConversationTriggerRunner(connection, routes)).toThrow();
    }
    expect(() => newConversationTriggerRunner(newConnection({} as never, reference), [{ bindingName: 'a', target }])).toThrow(/ReamazeClient/);
  });

  it('loads the saved channel and commits after recording each conversation', async () => {
    const fake = new ReamazeConversationFake(FakeEmail, FakeToken);
    fake.insert('existing', 'Existing', { channel: 'support', createdAt: new Date(start) });
    const { project } = await newTestProject(connectionName, [{ bindingName: 'newConversations', configuration: { channel: 'support' } }]);
    const target = new QueueTarget();
    const runner = newProjectConversationTriggerRunner(project, newProjectConnection(project, connectionName, { apiBaseURL: fake.baseURL, send: fake.send }), [{ bindingName: 'newConversations', target }]);
    const cursor = project.pollCursorStore(key);
    const commits = async () => (await cursor.readPollCursor()).state?.commitCount ?? 0;
    const process = running(runner);
    try {
      await eventually(async () => await commits() >= 1);
      expect(JSON.parse((await cursor.readPollCursor()).state!.token).channel).toBe('support');
      fake.insert('elsewhere', 'Sales', { channel: 'sales', createdAt: new Date(start + 60_000) });
      fake.insert('refund', 'Refund <now>', { channel: 'support', createdAt: new Date(start + 120_000) });
      runner.wakePoller('newConversations');
      const event = await target.nextEvent(0);
      expect(event.id).toBe(conversationCreatedEventId('acme', 'refund'));
      expect(event.payload.channel).toBe('support');
      expect(event.payload.conversation.firstMessage).toBe('Opening message of Refund <now>');
      await eventually(async () => await commits() >= 2);
      expect(target.events).toHaveLength(1);
      expect(fake.listedQueries.every(query => query.category === 'support' && query.filter === 'all')).toBe(true);
      await eventually(async () => (await project.triggerInbox(key).pending()).length === 0);
    } finally { await process.stop(); }
  });

  it('watches every channel for an unsaved binding and rejects an invalid saved one', async () => {
    const fake = new ReamazeConversationFake(FakeEmail, FakeToken);
    const { project } = await newTestProject(connectionName, []);
    const target = new QueueTarget();
    const runner = newProjectConversationTriggerRunner(project, newProjectConnection(project, connectionName, { apiBaseURL: fake.baseURL, send: fake.send }), [{ bindingName: 'newConversations', target }]);
    const process = running(runner);
    try {
      await eventually(() => fake.listRequestCount() >= 1);
      fake.insert('sales-1', 'Sales', { channel: 'sales', createdAt: new Date(start) });
      runner.wakePoller('newConversations');
      const event = await target.nextEvent(0);
      expect(event.payload.channel).toBeUndefined();
      expect(event.payload.conversation.channel.slug).toBe('sales');
      expect(fake.listedQueries[0]).not.toHaveProperty('category');
    } finally { await process.stop(); }
    for (const configuration of [{ channel: 'a/b' }, { channel: 'support', unknown: true }, { channel: 1 }]) {
      const saved = await newTestProject(connectionName, [{ bindingName: 'newConversations', configuration }]);
      expect(() => newProjectConversationTriggerRunner(saved.project, newProjectConnection(saved.project, connectionName), [{ bindingName: 'newConversations', target }])).toThrow(/newConversations/);
    }
  });

  it('baselines and delivers the next conversation with the single in-memory Trigger', async () => {
    const fake = new ReamazeConversationFake(FakeEmail, FakeToken);
    fake.insert('existing', 'Existing', { createdAt: new Date(start) });
    const connection = fakeConnection(fake);
    // A short interval keeps the test fast; the client rejects one under the 10s minimum.
    Object.assign(connection.operations as ReamazeClient, { pollIntervalMs: 1_000 });
    expect(() => newConversationCreatedTrigger({ connection, target: new QueueTarget(), configuration: { channel: 'a b' } })).toThrow(/channel/);
    const target = new QueueTarget();
    const process = running(newConversationCreatedTrigger({ connection, target }));
    try {
      await eventually(() => fake.listRequestCount() >= 1);
      fake.insert('new', 'New', { createdAt: new Date(start + 1_000) });
      const event = await target.nextEvent(0);
      expect(event.payload.conversation.id).toBe('new');
      expect(event.occurredAt.getTime()).toBe(start + 1_000);
    } finally { await process.stop(); }
  });

  it('keeps a rejected credential out of every Trigger log record', async () => {
    const records: string[] = [];
    const logger: TriggerLogger = { isEnabled: () => true, log: (level, message, attributes) => { records.push(`${level} ${message} ${JSON.stringify(attributes)}`); } };
    const process = running(newConversationTriggerRunner(fakeConnection(new ReamazeConversationFake(FakeEmail, 'reamazeOther' + 'Token0123456789')), [{ bindingName: 'newConversations', target: new QueueTarget() }], { logger }));
    try { await eventually(() => records.some(record => record.startsWith('warn') || record.startsWith('error'))); } finally { await process.stop(); }
    expect(records.some(record => record.includes(FakeToken) || record.includes(FakeEmail))).toBe(false);
  });

  it('decodes a Go-written inbox payload', () => {
    const goPayload = '{"brand":"acme","channel":"support","conversation":{"id":"refund","subject":"Refund \\u003cnow\\u003e","status":4,"tags":[],'
      + '"channel":{"slug":"support","name":"Support","type":1},"requester":{"name":"Jane Smith","email":"jane@acme.example.com"},'
      + '"firstMessage":"Opening message","createdAt":"2026-03-02T09:01:00.123Z"}}';
    const event = decodeConversationCreatedEventPayload(goPayload);
    expect(event.conversation.subject).toBe('Refund <now>');
    expect(event.conversation.createdAt).toBe('2026-03-02T09:01:00.123Z');
    expect(() => decodeConversationCreatedEventPayload('{"brand":"acme","conversation":[]}')).toThrow(TypeError);
  });
});
