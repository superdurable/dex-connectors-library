// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newTriggerContext, type TriggerEvent } from '@superdurable/connectors-sdk';
import type { ChangePage, Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { expect } from 'vitest';
import { decodeConversationReceivedCursorToken, type ConversationReceivedCursorToken, type ConversationReceivedEvent } from '../src/conversation_received_cursor.js';
import type { ConversationReceivedFetcher } from '../src/conversation_received_fetcher.js';
import { ConversationReceivedTriggerDefinition } from '../src/index.js';

export const inboxId = '105902';
const context = newTriggerContext();
export const binding = { connection: { provider: 'hiver', name: 'hiver-support' }, trigger: ConversationReceivedTriggerDefinition.trigger, name: 'new-conversations' };

/** Return count numeric IDs from first, such as 573741000. */
export function conversationIds(first: number, count: number): string[] { return Array.from({ length: count }, (_, index) => String(first + index)); }

/** Stands in for the kit's commits: it stamps seen zero with the next commit count. */
export class CursorHarness {
  cursor: Cursor = { token: '' };
  commits = 0;
  constructor(readonly fetcher: ConversationReceivedFetcher, readonly expectedInboxId = inboxId) {}
  async seed(): Promise<void> { this.commit(await this.fetcher.seedCursor()); }
  commit(cursor: Cursor): void {
    this.commits++;
    this.cursor = { token: cursor.token, snapshot: { entries: (cursor.snapshot?.entries ?? []).map(entry => ({ ...entry, seen: entry.seen || this.commits })) } };
  }
  async fetch(): Promise<{ delivered: string[]; page: ChangePage<TriggerEvent<ConversationReceivedEvent>> }> {
    const page = await this.fetcher.fetchChanges(context, this.cursor);
    this.commit(page.next);
    for (const event of page.changes) {
      expect(event.id).toBe(`conversation-${this.expectedInboxId}-${event.payload.conversation.id}`);
      expect(event.payload.inboxId).toBe(this.expectedInboxId);
    }
    return { delivered: page.changes.map(event => event.payload.conversation.id), page };
  }
  async completeBaseline(): Promise<void> {
    for (;;) {
      const { delivered, page } = await this.fetch();
      expect(delivered, 'the baseline delivers no existing conversation').toEqual([]);
      if (!page.hasMorePages) return;
    }
  }
  token(): ConversationReceivedCursorToken { return decodeConversationReceivedCursorToken(this.cursor.token, this.expectedInboxId); }
  trackedKeys(): string[] { return (this.cursor.snapshot?.entries ?? []).map(entry => entry.key); }
}

