// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { readFile } from 'node:fs/promises';
import { describe, expect, it } from 'vitest';
import { parse } from 'yaml';
import { markTriggerUndeliverable, newTrigger, newTriggerContext, type TriggerContext, type TriggerEvent, type TriggerTarget } from '@superdurable/connectors-sdk';
import {
  MessageReceivedTriggerConfigurationSchema, MessageReceivedTriggerDefinition, ReplyReceivedTriggerConfigurationSchema, ReplyReceivedTriggerDefinition,
  defineMessageReceivedTriggerBinding, defineReplyReceivedTriggerBinding, newMessageReceivedTrigger, newMessageTriggerRunner, newProjectMessageTriggerRunner, type MessageEvent,
} from '../src/index.js';
import { MessagePollingTriggerSource, messageMatcherMatches, nameBasedUUID, pollMessageTriggerRoutes } from '../src/trigger_source.js';
import { ConnectionReference, FakeGmail, RecordingLogger, fakeConnection, gmailProject, replyMessage, rootMessage } from './trigger_support.js';

/** Records delivered event IDs and fails or rejects events as scripted. */
class RecordingTarget implements TriggerTarget<MessageEvent> {
  readonly delivered: TriggerEvent<MessageEvent>[] = [];
  readonly attempts: string[] = [];
  readonly failures = new Map<string, number>();
  readonly undeliverable = new Set<string>();
  constructor(private readonly order?: string[]) {}
  async handleTrigger(_context: TriggerContext, event: TriggerEvent<MessageEvent>): Promise<void> {
    this.attempts.push(event.id);
    const remaining = this.failures.get(event.id) ?? 0;
    if (remaining > 0) { this.failures.set(event.id, remaining - 1); throw new Error('Dex is unavailable'); }
    if (this.undeliverable.has(event.id)) throw markTriggerUndeliverable(new Error('Flow is closed'));
    this.delivered.push(event);
    this.order?.push(event.id);
  }
  ids(): string[] { return this.delivered.map(event => event.id); }
}

function source(gmail: FakeGmail, logger: RecordingLogger, options: { triggerName?: string; searchQuery?: string; requiresReply?: boolean; matcher?: { messageContains?: string; senderEmails?: string[] } } = {}) {
  const connection = fakeConnection(gmail, { logger });
  const requiresReply = options.requiresReply ?? false;
  return new MessagePollingTriggerSource(connection.operations as never, ConnectionReference, options.triggerName ?? (requiresReply ? 'replyReceived' : 'messageReceived'), 'binding-1',
    options.searchQuery ?? '', options.matcher ?? {}, requiresReply);
}

const scanQuietly = async (poller: MessagePollingTriggerSource, context: TriggerContext, target: TriggerTarget<MessageEvent>) => { try { await poller.scan(context, target); } catch { /* the next scan retries */ } };

describe('Gmail message Trigger poller', () => {
  it('lists the inbox like Go, reads metadata, and delivers matching roots oldest first with the Go payload', async () => {
    const gmail = new FakeGmail();
    gmail.receive(rootMessage('m1', 't1', { internalDate: '1767312000120' }));
    gmail.receive(rootMessage('m2', 't2', { snippet: '' }));
    const logger = new RecordingLogger();
    const target = new RecordingTarget();
    await source(gmail, logger, { searchQuery: 'label:inbox is:unread' }).scan(newTriggerContext(), target);
    expect(target.ids()).toEqual(['m1', 'm2']);
    expect(target.delivered[0]).toEqual({ id: 'm1', occurredAt: new Date(1767312000120), payload: {
      primaryEmail: 'owner@example.com', messageId: 'm1', threadId: 't1', from: 'Ada <ada@example.com>', subject: 'Order question', snippet: 'Please check order 1042',
      receivedAt: '2026-01-02T00:00:00.12Z', isReply: false,
    } });
    expect(Object.keys(target.delivered[1]!.payload)).toEqual(['primaryEmail', 'messageId', 'threadId', 'from', 'subject', 'receivedAt', 'isReply']);
    expect(gmail.requests[0]!.url).toBe('http://127.0.0.1:9/gmail/v1/users/me/messages?labelIds=INBOX&maxResults=25&q=label%3Ainbox+is%3Aunread');
    expect(gmail.requests[1]!.url).toBe('http://127.0.0.1:9/gmail/v1/users/me/messages/m1?format=metadata&metadataHeaders=Message-ID&metadataHeaders=In-Reply-To&metadataHeaders=References&metadataHeaders=From&metadataHeaders=Reply-To&metadataHeaders=To&metadataHeaders=Subject');
    expect(gmail.requests.every(request => request.method === 'GET' && request.headers?.Authorization === 'Bearer SENTINEL-ACCESS')).toBe(true);
  });

  it('reads each listed message once while it stays on the page and ignores roots, replies, and mismatches with a reason', async () => {
    const gmail = new FakeGmail();
    gmail.receive(rootMessage('root', 'thread-1'));
    gmail.receive(replyMessage('reply-other', 'thread-1', { from: 'Eve <eve@example.com>' }));
    gmail.receive(replyMessage('reply-text', 'thread-1', { snippet: 'not yet' }));
    gmail.receive(replyMessage('reply-match', 'thread-1', { from: 'ADA@EXAMPLE.COM' }));
    const logger = new RecordingLogger();
    const target = new RecordingTarget();
    const poller = source(gmail, logger, { requiresReply: true, matcher: { messageContains: 'APPROVED', senderEmails: ['Ada <ada@example.com>'] } });
    await poller.scan(newTriggerContext(), target);
    await poller.scan(newTriggerContext(), target);
    expect(target.ids()).toEqual(['reply-match']);
    expect(gmail.reads()).toEqual(['root', 'reply-other', 'reply-text', 'reply-match']);
    expect(logger.find('trigger event ignored').map(record => [record.attributes.event_id, record.attributes.reason]).slice(0, 3))
      .toEqual([['root', 'not_a_reply'], ['reply-other', 'matcher_mismatch'], ['reply-text', 'matcher_mismatch']]);
    expect(logger.find('trigger event ignored')[0]!.attributes).toEqual({ connector: 'gmail', connection: 'gmail-inbox', trigger: 'replyReceived', binding: 'binding-1', thread_id: 'thread-1', event_id: 'root', reason: 'not_a_reply' });
  });

  it('consumes an undeliverable reply and delivers the rest of the page', async () => {
    const gmail = new FakeGmail();
    gmail.receive(replyMessage('r1', 't1'));
    gmail.receive(replyMessage('r2', 't2'));
    gmail.receive(replyMessage('r3', 't3'));
    const logger = new RecordingLogger();
    const target = new RecordingTarget();
    target.undeliverable.add('r2');
    const poller = source(gmail, logger, { requiresReply: true });
    await poller.scan(newTriggerContext(), target);
    await poller.scan(newTriggerContext(), target);
    expect(target.ids()).toEqual(['r1', 'r3']);
    expect(target.attempts).toEqual(['r1', 'r2', 'r3']);
    expect(logger.find('trigger event skipped: undeliverable', { event_id: 'r2', thread_id: 't2' })).toHaveLength(1);
  });

  it('retries a failed message first even after newer mail pushes it off the page, and logs the recovery', async () => {
    const gmail = new FakeGmail();
    const logger = new RecordingLogger();
    const connection = fakeConnection(gmail, { logger });
    const poller = new MessagePollingTriggerSource(connection.operations as never, ConnectionReference, 'messageReceived', '', '', {}, false);
    gmail.receive(rootMessage('old', 't-old'));
    const target = new RecordingTarget();
    target.failures.set('old', 1);
    await expect(poller.scan(newTriggerContext(), target)).rejects.toThrow('Dex is unavailable');
    for (let index = 0; index < 25; index++) gmail.receive(rootMessage(`new-${index}`, `t-${index}`));
    await poller.scan(newTriggerContext(), target);
    expect(target.ids()[0]).toBe('old');
    expect(target.ids()).toHaveLength(26);
    expect(logger.find('trigger delivery failed; retrying', { event_id: 'old', attempt: 1, delay_ms: 1000, error_type: 'Error' })).toHaveLength(1);
    expect(logger.find('trigger delivered after retry', { event_id: 'old', attempts: 2, thread_id: 't-old' })).toHaveLength(1);
    expect(logger.records.every(record => record.attributes.binding === undefined)).toBe(true);
  });

  it('does not report an event a runner consumed as undeliverable as delivered after its retry', async () => {
    const gmail = new FakeGmail();
    gmail.receive(rootMessage('m1', 't1'));
    const logger = new RecordingLogger();
    const target = new RecordingTarget();
    target.failures.set('m1', 1);
    const runner = newTrigger({ definition: MessageReceivedTriggerDefinition, binding: { connection: ConnectionReference, trigger: MessageReceivedTriggerDefinition.trigger, name: 'b' },
      source: { run: async (context, wrapped) => { const poller = source(gmail, logger); await scanQuietly(poller, context, wrapped); target.undeliverable.add('m1'); await poller.scan(context, wrapped); } },
      target, logger });
    await runner.run(newTriggerContext());
    expect(logger.find('trigger event skipped: undeliverable', { event_id: 'm1' })).toHaveLength(1);
    expect(logger.find('trigger delivered after retry')).toHaveLength(0);
  });

  it('counts consecutive failed polls, logs safe errors without the query or token, and resets after a good poll', async () => {
    const gmail = new FakeGmail();
    gmail.receive(rootMessage('m1', 't1'));
    gmail.listFailures.push(500, 503);
    gmail.readFailures.set('m1', 500);
    const logger = new RecordingLogger();
    const target = new RecordingTarget();
    const poller = source(gmail, logger, { searchQuery: 'from:SENTINEL-QUERY' });
    for (let index = 0; index < 3; index++) await scanQuietly(poller, newTriggerContext(), target);
    expect(target.ids()).toEqual([]);
    expect(logger.find('gmail poll failed; retrying').map(record => [record.attributes.attempt, record.attributes.error, record.attributes.event_id ?? '']))
      .toEqual([[1, 'Gmail message list is unavailable', ''], [2, 'Gmail message list is unavailable', ''], [3, 'Gmail message lookup failed', 'm1']]);
    await poller.scan(newTriggerContext(), target);
    gmail.listFailures.push(500);
    await scanQuietly(poller, newTriggerContext(), target);
    expect(logger.find('gmail poll failed; retrying').at(-1)!.attributes.attempt).toBe(1);
    expect(target.ids()).toEqual(['m1']);
    expect(JSON.stringify(logger.records)).not.toMatch(/SENTINEL|Order question|ada@example/);
  });

  it('reads credentials under the Go connector stable call ID', () => {
    expect(nameBasedUUID('https://superdurable.dev/dex-connectors/trigger-credential/v1/google/gmail-inbox/replyReceived')).toBe('87132e3f-e8be-557c-a0f9-f3e95b2f70ad');
  });
});

describe('Gmail message Trigger runner', () => {
  it('delivers every root before any reply in one poll, and a root that arrives during the poll before its reply', async () => {
    const gmail = new FakeGmail();
    const logger = new RecordingLogger();
    const order: string[] = [];
    const roots = new RecordingTarget(order);
    const replies = new RecordingTarget(order);
    const connection = fakeConnection(gmail, { logger });
    const client = connection.operations as never;
    const rootRoute = { source: new MessagePollingTriggerSource(client, ConnectionReference, 'messageReceived', 'start', '', {}, false), target: roots };
    const replyRoute = { source: new MessagePollingTriggerSource(client, ConnectionReference, 'replyReceived', 'reply', '', {}, true), target: replies };
    gmail.receive(rootMessage('root-1', 't1'));
    gmail.receive(replyMessage('reply-1', 't1'));
    gmail.receive(rootMessage('root-2', 't2'));
    await pollMessageTriggerRoutes(newTriggerContext(), [rootRoute], [replyRoute]);
    expect(order).toEqual(['root-1', 'root-2', 'reply-1']);
    // A reply that arrives after the reply listing waits for the next poll, after its root.
    let isInjected = false;
    gmail.receive(rootMessage('root-3', 't3'));
    const listedBefore = gmail.listCount;
    await pollMessageTriggerRoutes(newTriggerContext(), [rootRoute], [{ ...replyRoute, source: { list: async (context: TriggerContext) => {
      const listing = await replyRoute.source.list(context);
      if (!isInjected) { isInjected = true; gmail.receive(replyMessage('reply-3', 't3')); }
      return listing;
    }, deliver: (context: TriggerContext, target: TriggerTarget<MessageEvent>, listing: never) => replyRoute.source.deliver(context, target, listing) } as never }]);
    expect(gmail.listCount - listedBefore).toBe(2);
    expect(order).toEqual(['root-1', 'root-2', 'reply-1', 'root-3']);
    await pollMessageTriggerRoutes(newTriggerContext(), [rootRoute], [replyRoute]);
    expect(order).toEqual(['root-1', 'root-2', 'reply-1', 'root-3', 'reply-3']);
  });

  it('stops a poll at the first root failure so no reply overtakes it', async () => {
    const gmail = new FakeGmail();
    const order: string[] = [];
    const roots = new RecordingTarget(order);
    const replies = new RecordingTarget(order);
    roots.failures.set('root-1', 1);
    const runner = newMessageTriggerRunner({ connection: fakeConnection(gmail, { logger: new RecordingLogger() }),
      messageReceivedRoutes: [{ bindingName: 'start', configuration: {}, target: roots }], replyReceivedRoutes: [{ bindingName: 'reply', configuration: {}, target: replies }] });
    gmail.receive(rootMessage('root-1', 't1'));
    gmail.receive(replyMessage('reply-1', 't1'));
    const stop = new AbortController();
    const running = runner.run(newTriggerContext(stop.signal));
    await expect.poll(() => order, { timeout: 5_000 }).toEqual(['root-1', 'reply-1']);
    stop.abort();
    await expect(running).rejects.toThrow('Trigger delivery was aborted');
    expect(roots.attempts).toEqual(['root-1', 'root-1']);
  });

  it('runs one standalone Trigger until the context ends', async () => {
    const gmail = new FakeGmail();
    gmail.receive(rootMessage('m1', 't1'));
    const target = new RecordingTarget();
    const connection = fakeConnection(gmail, { logger: new RecordingLogger() });
    const trigger = newMessageReceivedTrigger({ connection, connectionName: 'gmail-inbox', bindingName: 'start', configuration: { messageMatcher: { messageContains: 'order' } }, target });
    expect(trigger.binding()).toEqual({ connection: ConnectionReference, trigger: MessageReceivedTriggerDefinition.trigger, name: 'start' });
    const stop = new AbortController();
    const running = trigger.run(newTriggerContext(stop.signal));
    await expect.poll(() => target.ids()).toEqual(['m1']);
    stop.abort();
    await expect(running).rejects.toThrow('Trigger delivery was aborted');
    expect(() => newMessageReceivedTrigger({ connection, connectionName: 'other', bindingName: 'start', configuration: {}, target })).toThrow('must equal its connection');
  });

  it('rejects duplicate bindings, missing routes, and invalid or duplicate sender emails', () => {
    const connection = fakeConnection(new FakeGmail());
    const target = new RecordingTarget();
    expect(() => newMessageTriggerRunner({ connection })).toThrow('Gmail message Trigger routes are required');
    expect(() => newMessageTriggerRunner({ connection, messageReceivedRoutes: [{ bindingName: 'a', configuration: {}, target }, { bindingName: ' a ', configuration: {}, target }] })).toThrow('"a" is duplicated');
    expect(() => newMessageTriggerRunner({ connection, messageReceivedRoutes: [{ bindingName: ' ', configuration: {}, target }] })).toThrow('binding name and target are required');
    expect(newMessageTriggerRunner({ connection, messageReceivedRoutes: [{ bindingName: 'a', configuration: {}, target }], replyReceivedRoutes: [{ bindingName: 'a', configuration: {}, target }] })).toBeDefined();
    expect(MessageReceivedTriggerConfigurationSchema.safeParse({ messageMatcher: { senderEmails: ['not an address'] } }).error?.issues[0]?.message).toBe('Gmail sender email is invalid');
    expect(ReplyReceivedTriggerConfigurationSchema.safeParse({ replyMatcher: { senderEmails: ['Ada <ada@example.com>', 'ADA@example.com'] } }).error?.issues[0]?.message).toBe('Gmail sender emails must be unique');
    expect(ReplyReceivedTriggerConfigurationSchema.parse({ searchQuery: 'in:inbox', replyMatcher: { senderEmails: null } })).toEqual({ searchQuery: 'in:inbox', replyMatcher: { senderEmails: null } });
    expect(() => newMessageTriggerRunner({ connection: { operations: {}, reference: ConnectionReference } as never, messageReceivedRoutes: [{ bindingName: 'a', configuration: {}, target }] })).toThrow('bound to a GmailClient');
  });

  it('matches like the Go matcher: subject or snippet text, then canonical sender addresses', () => {
    const message = { subject: 'Order 1042', snippet: 'Has it SHIPPED?', from: '"Ada L." <Ada@Example.com>' };
    expect(messageMatcherMatches({ messageContains: 'shipped', senderEmails: ['ada@example.com'] }, message)).toBe(true);
    expect(messageMatcherMatches({ messageContains: '1042\nhas' }, message)).toBe(true);
    expect(messageMatcherMatches({ senderEmails: ['eve@example.com'] }, message)).toBe(false);
    expect(messageMatcherMatches({ senderEmails: ['ada@example.com'] }, { ...message, from: 'not a mailbox' })).toBe(false);
  });

  it('declares the manifest Triggers and binding factories', async () => {
    const manifest = parse(await readFile(new URL('../../connector.yaml', import.meta.url), 'utf8')) as { spec: { triggers: Array<{ name: string; description: string }>; languages: { typescript: { triggers: string[] } } } };
    expect(manifest.spec.languages.typescript.triggers).toEqual(['messageReceived', 'replyReceived']);
    expect([MessageReceivedTriggerDefinition, ReplyReceivedTriggerDefinition].map(definition => [definition.trigger.triggerName, definition.description]))
      .toEqual(manifest.spec.triggers.map(trigger => [trigger.name, trigger.description]));
    const configurationUI = { units: [{ id: 'query', unitId: 'searchQueryInput', label: 'Query', required: false, description: 'Enter a Gmail search query.', bindings: [{ port: 'query', jsonPointer: '/searchQuery' }] }] };
    expect(defineMessageReceivedTriggerBinding({ connectionName: 'gmail-inbox', bindingName: 'start', configurationUI })).toEqual({ definition: MessageReceivedTriggerDefinition, connectionName: 'gmail-inbox', bindingName: 'start', configurationUI });
    expect(defineReplyReceivedTriggerBinding({ connectionName: 'gmail-inbox', bindingName: 'reply', configurationUI }).definition).toEqual(ReplyReceivedTriggerDefinition);
    expect(() => defineReplyReceivedTriggerBinding({ connectionName: '', bindingName: 'reply', configurationUI })).toThrow();
  });
});

describe('Gmail project message Trigger runner', () => {
  const bindings = [
    { triggerName: 'messageReceived', bindingName: 'start', configuration: { messageMatcher: { messageContains: 'order' } } },
    { triggerName: 'replyReceived', bindingName: 'reply', configuration: { replyMatcher: { senderEmails: ['ada@example.com'] } } },
  ];

  it('keeps a failed event in the binding inbox and replays it before polling after a restart', async () => {
    const project = await gmailProject(bindings);
    const gmail = new FakeGmail();
    gmail.receive(rootMessage('root-1', 't1'));
    const logger = new RecordingLogger();
    const failing = new RecordingTarget();
    failing.failures.set('root-1', Number.MAX_SAFE_INTEGER);
    const stopFirst = new AbortController();
    const first = newProjectMessageTriggerRunner(project, 'gmail-inbox', { messageReceivedRoutes: [{ bindingName: 'start', target: failing }] }, { transport: gmail.transport, logger });
    const firstRun = first.run(newTriggerContext(stopFirst.signal));
    await expect.poll(() => failing.attempts.length).toBeGreaterThan(0);
    stopFirst.abort();
    await expect(firstRun).rejects.toThrow('Trigger delivery was aborted');
    expect(await project.triggerInbox({ connectorId: 'gmail', connectionName: 'gmail-inbox', triggerName: 'messageReceived', bindingName: 'start' }).pending()).toMatchObject([{ id: 'root-1' }]);

    gmail.inbox = [];
    const order: string[] = [];
    const roots = new RecordingTarget(order);
    const replies = new RecordingTarget(order);
    const stopSecond = new AbortController();
    const second = newProjectMessageTriggerRunner(project, 'gmail-inbox', { messageReceivedRoutes: [{ bindingName: 'start', target: roots }], replyReceivedRoutes: [{ bindingName: 'reply', target: replies }] },
      { transport: gmail.transport, logger });
    const listsBefore = gmail.listCount;
    let listsAtReplay = -1;
    const handleRoot = roots.handleTrigger.bind(roots);
    roots.handleTrigger = async (context, event) => { if (listsAtReplay < 0) listsAtReplay = gmail.listCount; await handleRoot(context, event); };
    const secondRun = second.run(newTriggerContext(stopSecond.signal));
    await expect.poll(() => roots.ids()).toEqual(['root-1']);
    expect(listsAtReplay).toBe(listsBefore);
    expect(roots.delivered[0]!.payload).toMatchObject({ messageId: 'root-1', threadId: 't1', primaryEmail: 'owner@example.com', isReply: false });
    gmail.receive(replyMessage('reply-1', 't1'));
    await expect.poll(() => order).toEqual(['root-1', 'reply-1']);
    stopSecond.abort();
    await expect(secondRun).rejects.toThrow('Trigger delivery was aborted');
    expect(JSON.stringify(logger.records)).not.toMatch(/SENTINEL/);
  });

  it('names a binding missing from the project configuration', async () => {
    const project = await gmailProject(bindings);
    expect(() => newProjectMessageTriggerRunner(project, 'gmail-inbox', { replyReceivedRoutes: [{ bindingName: 'absent', target: new RecordingTarget() }] }))
      .toThrow('gmail replyReceived binding "absent" configuration');
  });
});
