// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Replays a recording of the Go conversationReceived fetcher against the same fake and matches its cursors
// byte for byte: tokens, event IDs, and a SHA-256 over every snapshot entry's key, hash, and seen counter.
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { newTriggerContext } from '@superdurable/connectors-sdk';
import { expect, it } from 'vitest';
import { ConversationReceivedFetcher } from '../src/conversation_received_fetcher.js';
import { ConversationReceivedTriggerDefinition } from '../src/index.js';
import { CursorHarness, conversationIds } from './conversation_cursor_harness.js';
import { HiverConversationsFake, newFakeClient } from './hiver_conversations_fake.js';

interface RecordedStep { action: string; ids?: string[]; token?: string; entriesSha256?: string; entryCount?: number; delivered?: string[]; eventIds?: string[]; hasMorePages?: boolean; firstEntries?: string[] }
interface Recording { inboxId: string; initialFirstId: number; initialCount: number; pollPagesPerPoll: number; steps: RecordedStep[] }

it('matches the Go fetcher cursor documents byte for byte', async () => {
  const recording = JSON.parse(readFileSync(new URL('./fixtures/go-conversation-received-cases.json', import.meta.url), 'utf8')) as Recording;
  const fake = new HiverConversationsFake(recording.inboxId, 'support@acme.example.com');
  fake.addConversations(recording.inboxId, ...conversationIds(recording.initialFirstId, recording.initialCount));
  const binding = { connection: { provider: 'hiver', name: 'hiver-support' }, trigger: ConversationReceivedTriggerDefinition.trigger, name: 'new-conversations' };
  const fetcher = new ConversationReceivedFetcher(newFakeClient(fake, { pollPagesPerPoll: recording.pollPagesPerPoll }), binding, { inboxId: recording.inboxId });
  const harness = new CursorHarness(fetcher, recording.inboxId);
  await harness.seed();
  expect(harness.cursor.token).toBe(recording.steps[0]!.token);
  for (const [index, step] of recording.steps.entries()) {
    if (index === 0) continue;
    const ids = step.ids ?? [];
    if (step.action === 'add') fake.addConversations(recording.inboxId, ...ids);
    else if (step.action === 'delete') fake.deleteConversation(recording.inboxId, ids[0]!);
    else if (step.action === 'expire') fake.expirePageTokens();
    else if (step.action === 'oldestFirst') fake.listOldestFirst();
    else {
      const page = await fetcher.fetchChanges(newTriggerContext(), harness.cursor);
      harness.commit(page.next);
      const lines = (harness.cursor.snapshot?.entries ?? []).map(entry => `${entry.key}:${entry.hash}:${entry.seen}`);
      expect(harness.cursor.token, `step ${index} token`).toBe(step.token);
      expect(page.changes.map(event => event.payload.conversation.id), `step ${index}`).toEqual(step.delivered);
      expect(page.changes.map(event => event.id), `step ${index}`).toEqual(step.eventIds);
      expect(page.hasMorePages ?? false, `step ${index}`).toBe(step.hasMorePages);
      expect(lines.length, `step ${index}`).toBe(step.entryCount);
      expect(lines.slice(0, 3), `step ${index}`).toEqual(step.firstEntries);
      expect(createHash('sha256').update(lines.join('\n')).digest('hex'), `step ${index} snapshot`).toBe(step.entriesSha256);
    }
  }
});
