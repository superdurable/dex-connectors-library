// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { TriggerDeliveryAbortedError, withTriggerLoggerAttributes, type ConnectionRef, type TriggerContext, type TriggerEvent, type TriggerLogger, type TriggerRef } from '@superdurable/connectors-sdk';
import { markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangeFetcher, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import type { ReamazeClient } from './client.js';
import {
  ConversationCreatedCursorInvalidError, ConversationCreatedLateIndexWindowMs, MaxConversationCreatedPagesPerPoll, conversationCreatedEventId, conversationDigest,
  decodeConversationCreatedCursor, encodeConversationCreatedCursor, instantNanoseconds, unixMilliseconds,
  type ConversationCreatedCursor, type ConversationCreatedEvent, type ConversationCreatedTriggerConfiguration, type ConversationWalk, type SeenConversation,
} from './conversation_created_cursor.js';
import type { Conversation } from './generated/models.js';
import { decodeConversation, encodeQuery, page, parseBody } from './wire.js';

const maximumSearchPage = 10_000;
/** Keeps the cursor token well below the kit's 64 KiB bound. */
const maximumSeenConversations = 800;
const compareBytes = (left: string, right: string) => Buffer.compare(Buffer.from(left), Buffer.from(right));
const compareCreated = (left: Conversation, right: Conversation) => {
  const difference = instantNanoseconds(left.createdAt) - instantNanoseconds(right.createdAt);
  return difference < 0n ? -1 : difference > 0n ? 1 : 0;
};

/** One listed page of conversations with the page count Re:amaze reported. */
interface ListedPage { conversations: Conversation[]; pageCount: number }

/**
 * Move the watermark to the newest walked conversation and keep every one at or above the new floor as seen.
 * A walk that stopped at the page bound, such as a seed, raises the floor just past its oldest conversation,
 * and trimming the seen set raises it just past the newest entry dropped. Both raises exclude a whole
 * creation millisecond, so a conversation that shares it and was not read, or whose seen entry was dropped,
 * is never delivered later as new.
 */
export function advanceConversationCreatedCursor(position: ConversationCreatedCursor, walk: ConversationWalk): ConversationCreatedCursor {
  const next: ConversationCreatedCursor = { brand: position.brand, channel: position.channel, watermarkMs: position.watermarkMs, floorMs: 0, seen: [] };
  let oldestWalkedMs = 0;
  walk.conversations.forEach((conversation, index) => {
    const createdAtMs = unixMilliseconds(conversation.createdAt);
    next.watermarkMs = Math.max(next.watermarkMs, createdAtMs);
    if (index === 0 || createdAtMs < oldestWalkedMs) oldestWalkedMs = createdAtMs;
  });
  next.floorMs = Math.max(position.floorMs, next.watermarkMs - ConversationCreatedLateIndexWindowMs);
  if (!walk.reachesFloor && walk.conversations.length) next.floorMs = Math.max(next.floorMs, oldestWalkedMs + 1);
  const newestByDigest = new Map<string, number>();
  for (const entry of position.seen) newestByDigest.set(entry.digest, Math.max(newestByDigest.get(entry.digest) ?? 0, entry.createdAtMs));
  for (const conversation of walk.conversations) {
    const digest = conversationDigest(conversation.id);
    newestByDigest.set(digest, Math.max(newestByDigest.get(digest) ?? 0, unixMilliseconds(conversation.createdAt)));
  }
  let seen: SeenConversation[] = [...newestByDigest].filter(([, createdAtMs]) => createdAtMs >= next.floorMs).map(([digest, createdAtMs]) => ({ digest, createdAtMs }));
  seen.sort((left, right) => left.createdAtMs !== right.createdAtMs ? right.createdAtMs - left.createdAtMs : compareBytes(left.digest, right.digest));
  if (seen.length > maximumSeenConversations) {
    next.floorMs = Math.max(next.floorMs, seen[maximumSeenConversations]!.createdAtMs + 1);
    seen = seen.slice(0, maximumSeenConversations).filter(entry => entry.createdAtMs >= next.floorMs);
  }
  next.seen = seen;
  return next;
}

/**
 * Polls one brand's, or one channel's, GET /conversations for the poll Trigger kit at parity with the Go
 * connector. Re:amaze lists conversations by creation time with no change token, so the cursor holds the
 * newest creation time, a floor ten minutes behind it below which nothing is delivered, and digests of the
 * conversations already seen at or above the floor. Its cursor tokens match Go's byte for byte.
 */
export class ConversationCreatedFetcher implements ChangeFetcher<TriggerEvent<ConversationCreatedEvent>> {
  private readonly channel: string;
  private readonly logger: TriggerLogger;

  /** Bind the client and one binding's validated configuration; logger receives the backlog WARN record. */
  constructor(private readonly client: ReamazeClient, private readonly connection: ConnectionRef, private readonly trigger: TriggerRef, bindingName: string, configuration: ConversationCreatedTriggerConfiguration, logger?: TriggerLogger) {
    this.channel = configuration.channel ?? '';
    this.logger = withTriggerLoggerAttributes(logger, { connector: 'reamaze', trigger: trigger.triggerName, binding: bindingName });
  }

  /** Read only the newest page, so existing conversations become the baseline and never arrive. */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const walk = await this.walkNewestConversations(context, 0, true);
    return { token: encodeConversationCreatedCursor(advanceConversationCreatedCursor({ brand: this.client.brand, channel: this.channel, watermarkMs: 0, floorMs: 0, seen: [] }, walk)) };
  }

  /** Deliver every listed conversation at or above the floor that the cursor has not seen, oldest first; a foreign or undecodable cursor reseeds. */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<ConversationCreatedEvent>>> {
    let position: ConversationCreatedCursor;
    try { position = decodeConversationCreatedCursor(cursor.token); }
    catch (error) { throw markCursorInvalid(error); }
    if (!position.brand) throw markCursorInvalid(new ConversationCreatedCursorInvalidError());
    if (position.brand !== this.client.brand || position.channel !== this.channel) throw markCursorInvalid(new Error('Re:amaze conversationCreated cursor belongs to another brand or channel'));
    const walk = await this.walkNewestConversations(context, position.floorMs, false);
    if (!walk.reachesFloor) this.logger.log('warn', 'poll backlog exceeds page bound; older new conversations skipped', { pages: walk.pagesRead });
    const seen = new Set(position.seen.map(entry => entry.digest));
    const fresh = walk.conversations.filter(conversation => unixMilliseconds(conversation.createdAt) >= position.floorMs && !seen.has(conversationDigest(conversation.id)));
    fresh.sort((left, right) => compareCreated(left, right) || compareBytes(left.id, right.id));
    return {
      changes: fresh.map(conversation => this.event(conversation)),
      next: { token: encodeConversationCreatedCursor(advanceConversationCreatedCursor(position, walk)) },
    };
  }

  private event(conversation: Conversation): TriggerEvent<ConversationCreatedEvent> {
    const payload: ConversationCreatedEvent = { brand: this.client.brand, ...(this.channel ? { channel: this.channel } : {}), conversation };
    return { id: conversationCreatedEventId(this.client.brand, conversation.id), occurredAt: new Date(unixMilliseconds(conversation.createdAt)), payload };
  }

  /**
   * Read pages from the newest end of the listing until one holds a conversation below the floor, so every
   * conversation at the floor, ties across a page edge included, is read; the walk also stops at the end of
   * the listing or after the page bound, and a seed reads only the newest page. Re:amaze documents its
   * default order as creation time without a direction, so the walk detects it from the first page and
   * starts from the last page when it ascends.
   */
  private async walkNewestConversations(context: TriggerContext, floorMs: number, isSeed: boolean): Promise<ConversationWalk> {
    const first = await this.listConversationPage(context, 1);
    const walk: ConversationWalk = { conversations: [], reachesFloor: false, pagesRead: 1 };
    const listed = first.conversations;
    const isAscending = listed.length > 1 && compareCreated(listed[0]!, listed[listed.length - 1]!) < 0;
    let pageNumber = 1, step = 1, current = first;
    if (isAscending && first.pageCount > 1) {
      if (first.pageCount > maximumSearchPage) throw new Error(`Re:amaze lists ${first.pageCount} conversation pages oldest first, more than the ${maximumSearchPage} a poll can reach`);
      pageNumber = first.pageCount; step = -1;
      current = await this.listConversationPage(context, pageNumber);
      walk.pagesRead++;
    }
    const listedIds = new Set<string>();
    for (;;) {
      let isBelowFloor = false;
      for (const conversation of current.conversations) {
        if (unixMilliseconds(conversation.createdAt) < floorMs) isBelowFloor = true;
        if (!listedIds.has(conversation.id)) { listedIds.add(conversation.id); walk.conversations.push(conversation); }
      }
      const nextPageNumber = pageNumber + step;
      const isLastPage = !current.conversations.length || nextPageNumber < 1 || nextPageNumber > Math.min(current.pageCount, maximumSearchPage);
      if (isBelowFloor || isLastPage) { walk.reachesFloor = true; return walk; }
      if (isSeed || walk.pagesRead >= MaxConversationCreatedPagesPerPoll) return walk;
      pageNumber = nextPageNumber;
      current = await this.listConversationPage(context, pageNumber);
      walk.pagesRead++;
    }
  }

  /** Send one GET /conversations request for every view, so archived and spam conversations are listed too. */
  private async listConversationPage(context: TriggerContext, pageNumber: number): Promise<ListedPage> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.connection, this.trigger, context.signal);
    const credentials = await this.client.resolveCredentials(call);
    if (credentials.kind !== 'resolved') throw new Error('Re:amaze conversationCreated poll: connection credentials are unavailable');
    const query = encodeQuery({ filter: 'all', page: String(pageNumber), ...(this.channel ? { category: this.channel } : {}) });
    const result = await this.client.exchange(call, 'conversationCreated', credentials, 'GET', '/conversations', query, undefined, context.signal);
    if (context.signal.aborted) throw new TriggerDeliveryAbortedError();
    if (result.kind === 'rateLimited') {
      // Re:amaze limits requests per minute per API token; the limit is shared with operation Steps.
      throw markRateLimited(new Error(`Re:amaze conversationCreated poll: ${result.failure.message}`), result.retryAfterMs);
    }
    if (result.kind !== 'success') throw new Error(`Re:amaze conversationCreated poll: ${result.failure.message}`);
    try {
      const wire = parseBody(result.response.body);
      const listing = page(wire, wire.conversations);
      return { conversations: listing.values.map(value => decodeConversation(value, true)), pageCount: listing.pageCount };
    } catch (error) {
      throw new Error(`Re:amaze conversationCreated poll: invalid conversation page: ${error instanceof Error ? error.message : 'unknown error'}`);
    }
  }
}
