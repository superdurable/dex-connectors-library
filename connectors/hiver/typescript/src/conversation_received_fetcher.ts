// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { TriggerDeliveryAbortedError, withTriggerLoggerAttributes, type TriggerBindingRef, type TriggerContext, type TriggerEvent, type TriggerLogger } from '@superdurable/connectors-sdk';
import {
  applyKeyedSnapshotChanges, markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest,
  type ChangeFetcher, type ChangePage, type Cursor, type KeyedSnapshotOptions,
} from '@superdurable/connectors-sdk/pollingtrigger';
import { MaxKeyedSnapshotEntries, type KeyedSnapshot } from '@superdurable/connectors-sdk/projectconfig';
import { MaximumPageSize, type HiverClient } from './client.js';
import {
  ConversationSnapshotItem, conversationReceivedEventId, decodeConversationReceivedCursorToken, encodeConversationReceivedCursorToken,
  type ConversationReceivedCursorToken, type ConversationReceivedEvent, type ConversationReceivedTriggerConfiguration,
} from './conversation_received_cursor.js';
import { decodeConversation, decodeListPage, inboxPath } from './decode.js';
import type { Conversation } from './generated/models.js';

const conversationReceivedOperation = 'conversationReceived';
/** Bounds the conversation IDs one binding remembers; a larger inbox pauses its binding as a bound fault. */
export const MaxTrackedConversations = 50_000;
/** Pages one baseline step reads; the baseline delivers nothing. */
const seedPagesPerFetch = 10;
/** Caps the wait after failed polls and between bound-fault retries. */
export const ConversationReceivedMaxBackoffMs = 30 * 60_000;
/** Makes the kit refuse, as a bound fault, a snapshot over MaxTrackedConversations. */
export const ConversationSnapshotOptions: KeyedSnapshotOptions = { maxEntries: MaxTrackedConversations };
const mergeOptions: KeyedSnapshotOptions = { maxEntries: MaxKeyedSnapshotEntries };
const rejectedPageTokenStatuses = new Set([400, 404, 410, 422]);
// Go's unicode.IsSpace set, which strings.TrimSpace trims.
const goSpace = /^[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$/g;

/** Trim as Go's strings.TrimSpace does. */
export function trimGoSpace(value: string): string { return value.replace(goSpace, ''); }

/** A definite Hiver rejection of one poll request, kept with its HTTP status. */
export class PollRequestError extends Error {
  /** Wrap the secret-free failure message with its status. */
  constructor(readonly status: number, message: string) { super(`Hiver conversationReceived poll: ${message}`); this.name = 'PollRequestError'; }
}

interface ConversationPage { conversations: Conversation[]; nextPageToken: string }

/** Reads one shared inbox's conversation list; Hiver documents no timestamp, filter, or order for it. */
export class ConversationReceivedFetcher implements ChangeFetcher<TriggerEvent<ConversationReceivedEvent>> {
  private readonly inboxId: string;
  private readonly logger: TriggerLogger;

  /** Bind the client, binding identity, and validated configuration; logger receives walk-restart records. */
  constructor(private readonly client: HiverClient, private readonly binding: TriggerBindingRef, configuration: ConversationReceivedTriggerConfiguration, logger?: TriggerLogger) {
    this.inboxId = trimGoSpace(configuration.inboxId);
    this.logger = withTriggerLoggerAttributes(logger, { connector: 'hiver', connection: binding.connection.name, trigger: binding.trigger.triggerName, binding: binding.name });
  }

  /** Start the baseline walk without a request; fetchChanges records existing conversations. */
  async seedCursor(): Promise<Cursor> {
    return { token: encodeConversationReceivedCursorToken({ inboxId: this.inboxId, isBaselineComplete: false, sweepPageToken: '', sweepStartSeen: 0, previousSweepStartSeen: 0 }), snapshot: { entries: [] } };
  }

  /** Continue the baseline walk, then read the first page plus sweep pages, delivering untracked IDs. */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<ConversationReceivedEvent>>> {
    let token: ConversationReceivedCursorToken;
    try { token = decodeConversationReceivedCursorToken(cursor.token, this.inboxId); } catch (error) { throw markCursorInvalid(error); }
    if (!token.isBaselineComplete) return this.continueBaseline(context, token, cursor.snapshot);
    return this.pollForNewConversations(context, token, cursor.snapshot);
  }

  private async continueBaseline(context: TriggerContext, token: ConversationReceivedCursorToken, snapshot: KeyedSnapshot | undefined): Promise<ChangePage<TriggerEvent<ConversationReceivedEvent>>> {
    const listed: Conversation[] = [];
    let pageToken = token.sweepPageToken;
    for (let pages = 0; pages < seedPagesPerFetch; pages++) {
      let read: ConversationPage;
      try { read = await this.readPage(context, pageToken); } catch (error) {
        if (!pageToken || !isRejectedPageToken(error)) throw error;
        this.logWalkRestart('baseline');
        pageToken = '';
        break;
      }
      listed.push(...read.conversations);
      pageToken = read.nextPageToken;
      if (!pageToken) { token.isBaselineComplete = true; break; }
    }
    const next = mergeSnapshot(snapshot, listed, []);
    token.sweepPageToken = pageToken;
    return { changes: [], next: { token: encodeConversationReceivedCursorToken(token), snapshot: next }, hasMorePages: !token.isBaselineComplete };
  }

  private async pollForNewConversations(context: TriggerContext, token: ConversationReceivedCursorToken, snapshot: KeyedSnapshot | undefined): Promise<ChangePage<TriggerEvent<ConversationReceivedEvent>>> {
    const isSweepStart = !token.sweepPageToken;
    if (isSweepStart) token.sweepStartSeen = highestSeen(snapshot) + 1;
    const head = await this.readPage(context, '');
    const listed = [...head.conversations];
    let sweepPageToken = isSweepStart ? head.nextPageToken : token.sweepPageToken;
    let isSweepComplete = !sweepPageToken, isSweepRestarted = false;
    for (let pages = 1; !isSweepComplete && pages < this.client.pollPagesPerPoll; pages++) {
      let read: ConversationPage;
      try { read = await this.readPage(context, sweepPageToken); } catch (error) {
        if (!isRejectedPageToken(error)) throw error;
        this.logWalkRestart('sweep');
        sweepPageToken = '';
        isSweepRestarted = true;
        break;
      }
      listed.push(...read.conversations);
      sweepPageToken = read.nextPageToken;
      isSweepComplete = !sweepPageToken;
    }
    const changes = this.newConversationEvents(snapshot, listed);
    let removedKeys: string[] = [];
    if (isSweepComplete && !isSweepRestarted) {
      removedKeys = unlistedSinceSweepStart(snapshot, token.previousSweepStartSeen, listed);
      token.previousSweepStartSeen = token.sweepStartSeen;
    }
    const next = mergeSnapshot(snapshot, listed, removedKeys);
    token.sweepPageToken = sweepPageToken;
    return { changes, next: { token: encodeConversationReceivedCursorToken(token), snapshot: next } };
  }

  /** Return one event per listed conversation ID the snapshot lacks, in list order. */
  private newConversationEvents(snapshot: KeyedSnapshot | undefined, listed: readonly Conversation[]): TriggerEvent<ConversationReceivedEvent>[] {
    const tracked = new Set((snapshot?.entries ?? []).map(entry => entry.key));
    const occurredAt = this.client.currentTime();
    const events: TriggerEvent<ConversationReceivedEvent>[] = [];
    for (const conversation of listed) {
      if (tracked.has(conversation.id)) continue;
      tracked.add(conversation.id);
      events.push({ id: conversationReceivedEventId(this.inboxId, conversation.id), occurredAt, payload: { inboxId: this.inboxId, conversation } });
    }
    return events;
  }

  /** Wait for the runner's limiter and the connection's request slot, then read one page of 100. */
  private async readPage(context: TriggerContext, pageToken: string): Promise<ConversationPage> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.binding.connection, this.binding.trigger, context.signal);
    const key = await this.client.resolveKey(call);
    if (!key) throw new Error('Hiver conversationReceived poll: connection credentials are unavailable');
    if (!await this.client.waitForSlot(context.signal)) throw new TriggerDeliveryAbortedError();
    const query = new URLSearchParams([['limit', String(MaximumPageSize)], ...(pageToken ? [['next_page', pageToken] as [string, string]] : [])]);
    const result = await this.client.exchange(call, conversationReceivedOperation, key, { method: 'GET', path: `${inboxPath(this.inboxId)}/conversations`, query }, context.signal, true);
    if (result.kind === 'success') return this.decodePage(result.response.body);
    if (context.signal.aborted) throw new TriggerDeliveryAbortedError();
    const message = `Hiver conversationReceived poll: ${result.failure.message}`;
    // Without Retry-After the runner backs off exponentially; the client pacer also holds its requests.
    if (result.kind === 'rateLimited') throw markRateLimited(new Error(message), result.retryAfterMs);
    if (result.kind === 'notFound' || result.kind === 'rejected') throw new PollRequestError(result.status ?? 0, result.failure.message);
    throw new Error(message);
  }

  /** Validate one list page; a page with more conversations than requested is invalid. */
  private decodePage(body: Uint8Array): ConversationPage {
    let page: { results: unknown[]; nextPageToken: string };
    try { page = decodeListPage(body); } catch (error) { throw new Error(`Hiver conversationReceived poll: invalid conversation page: ${errorMessage(error)}`); }
    if (page.results.length > MaximumPageSize) throw new Error('Hiver conversationReceived poll: the page lists more conversations than requested');
    const conversations = page.results.map((raw, index) => {
      try { return decodeConversation(raw, this.inboxId).conversation; } catch (error) { throw new Error(`Hiver conversationReceived poll: invalid conversation at index ${index}: ${errorMessage(error)}`); }
    });
    return { conversations, nextPageToken: page.nextPageToken };
  }

  private logWalkRestart(walk: string): void {
    this.logger.log('warn', 'hiver page token rejected; restarting walk from the first page', { walk });
  }
}

/** Refresh every listed ID as seen by this commit and drop removedKeys; the kit enforces MaxTrackedConversations. */
function mergeSnapshot(snapshot: KeyedSnapshot | undefined, listed: readonly Conversation[], removedKeys: string[]): KeyedSnapshot {
  const seenIds = new Set<string>();
  const items: ConversationSnapshotItem[] = [];
  for (const conversation of listed) {
    if (seenIds.has(conversation.id)) continue;
    seenIds.add(conversation.id);
    items.push(new ConversationSnapshotItem(conversation.id));
  }
  return applyKeyedSnapshotChanges(snapshot, items, removedKeys, mergeOptions);
}

/** The latest commit that refreshed an entry; zero for an empty snapshot. */
function highestSeen(snapshot: KeyedSnapshot | undefined): number {
  return (snapshot?.entries ?? []).reduce((highest, entry) => Math.max(highest, entry.seen), 0);
}

/** Tracked IDs that no page listed since the commit counter sweepStartSeen. */
function unlistedSinceSweepStart(snapshot: KeyedSnapshot | undefined, sweepStartSeen: number, listed: readonly Conversation[]): string[] {
  const listedNow = new Set(listed.map(conversation => conversation.id));
  return (snapshot?.entries ?? []).filter(entry => !listedNow.has(entry.key) && entry.seen < sweepStartSeen).map(entry => entry.key);
}

function isRejectedPageToken(error: unknown): boolean {
  return error instanceof PollRequestError && rejectedPageTokenStatuses.has(error.status);
}

function errorMessage(error: unknown): string { return error instanceof Error ? error.message : String(error); }
