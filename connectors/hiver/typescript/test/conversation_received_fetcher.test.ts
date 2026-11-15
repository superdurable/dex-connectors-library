// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// conversationReceived fetcher behavior, ported from the Go connector's conversation_received_trigger tests.
import { newTriggerContext } from '@superdurable/connectors-sdk';
import { RateLimitedError, isCursorInvalid } from '@superdurable/connectors-sdk/pollingtrigger';
import { describe, expect, it } from 'vitest';
import { decodeConversationReceivedCursorToken, encodeConversationReceivedCursorToken } from '../src/conversation_received_cursor.js';
import { ConversationReceivedFetcher } from '../src/conversation_received_fetcher.js';
import { CursorHarness, binding, conversationIds, inboxId } from './conversation_cursor_harness.js';
import { APIKey, HiverConversationsFake, newFakeClient } from './hiver_conversations_fake.js';

const context = newTriggerContext();

async function newHarness(fake: HiverConversationsFake, pollPagesPerPoll = 0, configuredInboxId = ` ${inboxId} `): Promise<CursorHarness> {
  const harness = new CursorHarness(new ConversationReceivedFetcher(newFakeClient(fake, { pollPagesPerPoll }), binding, { inboxId: configuredInboxId }));
  await harness.seed();
  return harness;
}

const newFake = () => new HiverConversationsFake(inboxId, 'support@acme.example.com');

describe('ConversationReceivedFetcher', () => {
  it('records existing conversations as a baseline without events', async () => {
    const fake = newFake();
    fake.addConversations(inboxId, ...conversationIds(573741000, 1250));
    const harness = await newHarness(fake);
    expect(fake.conversationListRequests, 'seeding sends no request').toBe(0);
    const { page } = await harness.fetch();
    expect(page.hasMorePages, 'a baseline of 13 pages continues after 10').toBe(true);
    expect(fake.conversationListRequests).toBe(10);
    await harness.completeBaseline();
    expect(fake.conversationListRequests).toBe(13);
    expect(harness.trackedKeys()).toHaveLength(1250);
    expect(harness.cursor.token).toBe('{"v":1,"inboxId":"105902","baselineComplete":true}');
  });

  it('delivers new conversations from the first page in list order', async () => {
    const fake = newFake();
    fake.addConversations(inboxId, ...conversationIds(573741000, 250));
    const harness = await newHarness(fake);
    await harness.completeBaseline();
    fake.addConversations(inboxId, '573742001', '573742002');
    const before = fake.conversationListRequests;
    const { delivered, page } = await harness.fetch();
    expect(delivered, 'newest-first list order').toEqual(['573742002', '573742001']);
    expect(page.hasMorePages ?? false, 'a poll never continues at once after the baseline').toBe(false);
    expect(fake.conversationListRequests - before, 'the first page plus two sweep pages').toBe(3);
    expect(page.changes[0]!.payload.conversation.status).toBe('open');
    expect((await harness.fetch()).delivered, 'a tracked conversation is never delivered again').toEqual([]);
  });

  it('finds conversations listed on later pages through the sweep', async () => {
    const fake = newFake();
    fake.addConversations(inboxId, ...conversationIds(573741000, 450));
    fake.listOldestFirst();
    const harness = await newHarness(fake);
    await harness.completeBaseline();
    fake.addConversations(inboxId, '573749999');
    expect((await harness.fetch()).delivered, 'pages 1 to 3 of 5 do not list it').toEqual([]);
    expect((await harness.fetch()).delivered, 'the sweep reaches the last page').toEqual(['573749999']);
    expect(harness.token().sweepPageToken).toBe('');
    for (let poll = 0; poll < 3; poll++) expect((await harness.fetch()).delivered).toEqual([]);
  });

  it('stops tracking conversations two whole sweeps never listed', async () => {
    const fake = newFake();
    fake.addConversations(inboxId, '573741001', '573741002', '573741003');
    const harness = await newHarness(fake);
    await harness.completeBaseline();
    fake.deleteConversation(inboxId, '573741002');
    expect((await harness.fetch()).delivered).toEqual([]);
    expect(harness.trackedKeys(), 'one complete sweep without the ID keeps tracking it').toHaveLength(3);
    expect((await harness.fetch()).delivered).toEqual([]);
    expect(harness.trackedKeys(), 'two complete sweeps never listed it').toEqual(['573741001', '573741003']);
  });

  it('never redelivers a conversation shifted past a sweep page', async () => {
    const fake = newFake();
    fake.addConversations(inboxId, ...conversationIds(573741000, 250));
    const harness = await newHarness(fake, 2, inboxId);
    await harness.completeBaseline();
    expect((await harness.fetch()).delivered, 'the sweep reads offsets 0 to 199').toEqual([]);
    // Newest first, offset 200 lists 573741049 until a deletion above it shifts it to offset 199.
    fake.deleteConversation(inboxId, '573741249');
    expect((await harness.fetch()).delivered, 'the last page starts at offset 200 and skips 573741049').toEqual([]);
    expect(harness.token().sweepPageToken, 'the sweep completed').toBe('');
    expect(harness.trackedKeys()).toContain('573741049');
    for (let poll = 0; poll < 4; poll++) expect((await harness.fetch()).delivered, 'a conversation there all along is never delivered').toEqual([]);
    expect(harness.trackedKeys()).not.toContain('573741249');
    expect(harness.trackedKeys()).toHaveLength(249);
  });

  it('restarts the sweep when Hiver rejects its page token', async () => {
    const fake = newFake();
    fake.addConversations(inboxId, ...conversationIds(573741000, 450));
    fake.listOldestFirst();
    const harness = await newHarness(fake);
    await harness.completeBaseline();
    await harness.fetch();
    expect(harness.token().sweepPageToken).not.toBe('');
    fake.expirePageTokens();
    fake.addConversations(inboxId, '573749999');
    expect((await harness.fetch()).delivered).toEqual([]);
    expect(harness.token().sweepPageToken, 'the rejected token restarts the sweep').toBe('');
    expect(harness.trackedKeys(), 'a restart keeps every tracked conversation').toHaveLength(450);
    expect((await harness.fetch()).delivered).toEqual([]);
    expect((await harness.fetch()).delivered, 'the restarted sweep finds it without a gap').toEqual(['573749999']);
  });

  it('restarts the baseline when Hiver rejects its page token', async () => {
    const fake = newFake();
    fake.addConversations(inboxId, ...conversationIds(573741000, 1250));
    const harness = await newHarness(fake);
    await harness.fetch();
    fake.expirePageTokens();
    const { page } = await harness.fetch();
    expect(page.hasMorePages).toBe(true);
    expect(harness.token()).toEqual({ inboxId, isBaselineComplete: false, sweepPageToken: '', sweepStartSeen: 0, previousSweepStartSeen: 0 });
    await harness.completeBaseline();
    expect(harness.trackedKeys()).toHaveLength(1250);
  });

  it('maps rate limits and invalid cursors', async () => {
    const fake = newFake();
    fake.addConversations(inboxId, '573741001');
    const harness = await newHarness(fake);
    await harness.completeBaseline();
    fake.rateLimitNext(2);
    const limited = await harness.fetcher.fetchChanges(context, harness.cursor).catch((error: unknown) => error);
    expect(limited).toBeInstanceOf(RateLimitedError);
    expect((limited as RateLimitedError).retryAfterMs).toBe(2000);
    expect(String(limited)).not.toContain(APIKey);
    fake.rateLimitNext(0);
    const unbounded = await harness.fetcher.fetchChanges(context, harness.cursor).catch((error: unknown) => error);
    expect((unbounded as RateLimitedError).retryAfterMs, 'without Retry-After the runner backs off exponentially').toBe(0);
    for (const [name, token] of Object.entries({
      'not JSON': 'row-12', 'other version': '{"v":2,"inboxId":"105902"}', 'other inbox': '{"v":1,"inboxId":"202","baselineComplete":true}',
      'unknown field': '{"v":1,"inboxId":"105902","cursor":"x"}', 'bad page': '{"v":1,"inboxId":"105902","sweepPageToken":"a b"}',
      'fractional seen': '{"v":1,"inboxId":"105902","sweepStartSeen":1.5}', 'previous after start': '{"v":1,"inboxId":"105902","sweepStartSeen":1,"previousSweepStartSeen":2}',
    })) {
      const error = await harness.fetcher.fetchChanges(context, { token, snapshot: { entries: [] } }).catch((caught: unknown) => caught);
      expect(isCursorInvalid(error), name).toBe(true);
    }
  });

  it('decodes cursor tokens as Go json.Decoder does', () => {
    const token = decodeConversationReceivedCursorToken('{"V":1,"INBOXID":"105902","baselineComplete":null,"sweepPageToken":"e0.o100","sweepStartSeen":7,"previousSweepStartSeen":3,"v":1}', inboxId);
    expect(token).toEqual({ inboxId, isBaselineComplete: false, sweepPageToken: 'e0.o100', sweepStartSeen: 7, previousSweepStartSeen: 3 });
    expect(encodeConversationReceivedCursorToken(token)).toBe('{"v":1,"inboxId":"105902","baselineComplete":false,"sweepPageToken":"e0.o100","sweepStartSeen":7,"previousSweepStartSeen":3}');
  });

  it('reports a missing inbox as a retried failure', async () => {
    const harness = await newHarness(new HiverConversationsFake('999', 'other@acme.example.com'));
    const error = await harness.fetcher.fetchChanges(context, harness.cursor).catch((caught: unknown) => caught);
    expect(String(error)).toContain('HTTP 404');
    expect(isCursorInvalid(error), 'a missing inbox is retried, never rebaselined').toBe(false);
  });
});
