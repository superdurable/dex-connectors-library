// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Internal: the messageReceived Messages list fetcher and cursor document; index.ts does not export it.
import { createHash } from 'node:crypto';
import type { ConnectionRef, TriggerContext, TriggerEvent } from '@superdurable/connectors-sdk';
import {
  MaxChangesPerPage, applyKeyedSnapshotChanges, markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest,
  type ChangeFetcher, type ChangePage, type Cursor, type KeyedItem, type KeyedSnapshotOptions,
} from '@superdurable/connectors-sdk/pollingtrigger';
import { encodeGoJSONValue, type KeyedSnapshot } from '@superdurable/connectors-sdk/projectconfig';
import { errorCode, parseTwilioJSON, retryAfter, type TwilioAuthorization, type TwilioClient } from './client.js';
import { MessageSchema, type Message } from './generated/models.js';
import { InboundMessageDirection, MessageReceivedTriggerDefinition, MessageStatusReceiving, type MessageReceivedEvent } from './trigger.js';

/** Keeps a page of full message resources well under the default response limit; Twilio allows 1 to 1000. */
const inboundListPageSize = 200;
/** Bounds one listing at 2,000 messages; a larger window is split by DateSent. */
const maximumWindowPages = 10;
/** Halves a window down to one second from more than 500 years. */
const maximumWindowNarrowings = 34;
/** Relists recent seconds, so a message Twilio dates slightly late is still found. */
const inboundLookbackSeconds = 120;
/** Bounds how long a message still in receiving holds the watermark back. */
const receivingHoldLimitMs = 10 * 60_000;
const zeroTime = '0001-01-01T00:00:00Z';
const int64Minimum = -(2n ** 63n), int64Maximum = 2n ** 63n - 1n;
const loneSurrogatePattern = /[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/g;
const resourceStringFields = ['sid', 'account_sid', 'messaging_service_sid', 'from', 'to', 'status', 'direction', 'price', 'price_unit', 'date_created', 'date_sent', 'date_updated', 'body'];

/** Bounds the keys of messages delivered within the lookback. */
export const inboundSnapshotOptions: KeyedSnapshotOptions = {};

/** Return the Trigger event ID of one message; a SID is assigned once, so the ID never changes. */
export function messageReceivedEventId(messageSid: string): string { return `twilio-message-${messageSid}`; }

/** Convert one inbound message; occurredAt is when Twilio received it, else created it, else now. */
export function newMessageReceivedTriggerEvent(message: Message, body: string, now: Date): TriggerEvent<MessageReceivedEvent> {
  const dated = [message.sentAt, message.createdAt].find(value => value !== zeroTime);
  const occurredAt = dated === undefined ? now : new Date(dated);
  return { id: messageReceivedEventId(message.sid ?? ''), occurredAt, payload: { message: MessageSchema.parse(message), ...(body ? { body } : {}) } };
}

/** One decoded list entry; sentAtSecond is zero while Twilio reports no date_sent. */
interface ListedMessage { message: Message; body: string; sentAtSecond: number }
/** A complete listing of [since, upperBound]; a zero upperBound is unbounded. */
interface InboundMessageWindow { messages: ListedMessage[]; upperBound: number }

/** Records one delivered message as "<date_sent Unix second>-<SID>", so pruning needs no values. */
export class InboundSnapshotKey implements KeyedItem {
  /** Wrap one dated message key. */ constructor(private readonly key: string) {}
  /** The dated message key. */ snapshotKey(): string { return this.key; }
  /** A hash of the key; a delivered message never changes its entry. */ snapshotHash(): string { return createHash('sha256').update(this.key, 'utf8').digest('hex').slice(0, 16); }
  /** Undefined because entries keep no values. */ snapshotValue(): Uint8Array | undefined { return undefined; }
}

function inboundSnapshotKeyFor(listed: ListedMessage): string {
  const second = listed.sentAtSecond < 0 ? `-${String(-listed.sentAtSecond).padStart(10, '0')}` : String(listed.sentAtSecond).padStart(11, '0');
  return `${second}-${listed.message.sid ?? ''}`;
}

/** Parse the key's second prefix as Go's strconv.ParseInt does, else undefined. */
function inboundSnapshotKeySecond(key: string): number | undefined {
  const separator = key.indexOf('-');
  if (separator < 0) return undefined;
  const prefix = key.slice(0, separator);
  if (!/^[+-]?[0-9]+$/.test(prefix)) return undefined;
  const second = BigInt(prefix);
  return second < int64Minimum || second > int64Maximum ? undefined : Number(second);
}

function latestSentAtSecond(messages: ListedMessage[]): number {
  return messages.reduce((latest, listed) => Math.max(latest, listed.sentAtSecond), 0);
}

function unixSecond(moment: Date): number { return Math.floor(moment.getTime() / 1000); }

function formatRFC3339Second(second: number): string { return new Date(second * 1000).toISOString().replace('.000Z', 'Z'); }

/** Escape as Go's url.QueryEscape. */
function goQueryEscape(value: string): string {
  return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replace(/%20/g, '+');
}

/** Encode as Go's url.Values.Encode: keys sorted, QueryEscape for keys and values. */
export function encodeGoQuery(query: Record<string, string>): string {
  return Object.keys(query).sort().map(name => `${goQueryEscape(name)}=${goQueryEscape(query[name] ?? '')}`).join('&');
}

/**
 * Decode the cursor token as Go's json.Unmarshal into inboundMessageCursor does: keys match
 * case-insensitively and null leaves a field unset. Undefined for a token Go rejects or a watermark that is not positive.
 */
export function decodeInboundMessageCursorToken(token: string): { accountSid: string; to: string; watermark: number } | undefined {
  const watermarkSources = new Map<object, string>();
  let position: unknown;
  try {
    const parseWithSource = JSON.parse as (text: string, reviver: (this: object, key: string, value: unknown, context: { source?: string }) => unknown) => unknown;
    position = parseWithSource(token, function (this: object, key: string, value: unknown, context: { source?: string }) {
      if (key.toLowerCase() === 'watermark' && typeof value === 'number') watermarkSources.set(this, context.source ?? '');
      return value;
    });
  } catch { return undefined; }
  if (position === null || typeof position !== 'object' || Array.isArray(position)) return undefined;
  let accountSid = '', to = '', watermark = 0;
  for (const [name, value] of Object.entries(position as Record<string, unknown>)) {
    const fieldName = name.toLowerCase();
    if (value === null || !['accountsid', 'to', 'watermark'].includes(fieldName)) continue;
    if (fieldName === 'watermark') {
      const source = watermarkSources.get(position) ?? '';
      if (typeof value !== 'number' || !/^-?(0|[1-9][0-9]*)$/.test(source)) return undefined;
      const parsed = BigInt(source);
      if (parsed < int64Minimum || parsed > int64Maximum) return undefined;
      watermark = Number(parsed);
    } else if (typeof value !== 'string') return undefined;
    else if (fieldName === 'accountsid') accountSid = value;
    else to = value;
  }
  return watermark > 0 ? { accountSid, to, watermark } : undefined;
}

/** Reads one account's Messages list for the poll Trigger kit, at parity with the Go fetcher. */
export class InboundMessageFetcher implements ChangeFetcher<TriggerEvent<MessageReceivedEvent>> {
  /** Bind the client, the connection whose credential each request uses, and the receiving address. */
  constructor(private readonly client: TwilioClient, private readonly connection: ConnectionRef, private readonly to: string) {}

  /** List the lookback before now and record every inbound message in it as already delivered. */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const now = unixSecond(this.client.currentTime());
    const since = now - inboundLookbackSeconds;
    const window = await this.listWindow(context, since, 0);
    if (window.upperBound !== 0) throw new Error(`Twilio listed more than ${inboundListPageSize * maximumWindowPages} messages within the baseline lookback`);
    const watermark = Math.max(now, latestSentAtSecond(window.messages));
    const delivered = window.messages.filter(listed => this.isReceivedHere(listed) && listed.sentAtSecond >= since).map(listed => new InboundSnapshotKey(inboundSnapshotKeyFor(listed)));
    return this.encodeCursor(watermark, undefined, delivered);
  }

  /**
   * Return, oldest first, up to MaxChangesPerPage inbound messages dated from the watermark minus the lookback.
   * A message still in receiving holds the watermark until received or ten minutes old. An undecodable cursor,
   * or one for another account or receiving address, is marked invalid.
   */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<MessageReceivedEvent>>> {
    const position = decodeInboundMessageCursorToken(cursor.token);
    if (!position) throw markCursorInvalid(new Error('Twilio messageReceived cursor is not decodable'));
    if (position.accountSid !== this.client.accountSid || position.to !== this.to) throw markCursorInvalid(new Error('Twilio messageReceived cursor belongs to another account or receiving address'));
    const since = position.watermark - inboundLookbackSeconds;
    // A narrowed window must still pass the watermark, or the poll would make no progress.
    const window = await this.listWindow(context, since, position.watermark + 1);
    const now = this.client.currentTime();
    const delivered = new Set((cursor.snapshot?.entries ?? []).map(entry => entry.key));
    let holdBound = 0;
    const holdAt = (second: number) => { if (holdBound === 0 || second < holdBound) holdBound = second; };
    let pending: ListedMessage[] = [];
    for (const listed of window.messages) {
      if (!this.isReceivedHere(listed)) continue;
      if (listed.sentAtSecond === 0) holdAt(position.watermark);
      else if (listed.sentAtSecond < since || delivered.has(inboundSnapshotKeyFor(listed))) continue;
      else if (listed.message.status === MessageStatusReceiving && now.getTime() - Date.parse(listed.message.sentAt) < receivingHoldLimitMs) holdAt(listed.sentAtSecond);
      else pending.push(listed);
    }
    pending.sort((left, right) => left.sentAtSecond - right.sentAtSecond || ((left.message.sid ?? '') < (right.message.sid ?? '') ? -1 : (left.message.sid ?? '') > (right.message.sid ?? '') ? 1 : 0));
    let watermark: number;
    let hasMorePages = false;
    const isPageFull = pending.length > MaxChangesPerPage;
    if (isPageFull) {
      pending = pending.slice(0, MaxChangesPerPage);
      watermark = pending[pending.length - 1]!.sentAtSecond; hasMorePages = true;
    } else if (window.upperBound !== 0) {
      watermark = window.upperBound; hasMorePages = true;
    } else watermark = latestSentAtSecond(window.messages);
    if (holdBound !== 0 && watermark > holdBound) {
      watermark = holdBound;
      // Relisting a held window returns the same messages, so only a full change page is worth refetching.
      hasMorePages = isPageFull;
    }
    watermark = Math.max(watermark, position.watermark);
    const changes = pending.map(listed => newMessageReceivedTriggerEvent(listed.message, listed.body, now));
    const upserted = pending.map(listed => new InboundSnapshotKey(inboundSnapshotKeyFor(listed)));
    return { changes, next: this.encodeCursor(watermark, cursor.snapshot, upserted), hasMorePages };
  }

  /** Report an inbound message to the watched address, or to any address when none is set. */
  private isReceivedHere(listed: ListedMessage): boolean {
    return listed.message.direction === InboundMessageDirection && (this.to === '' || listed.message.to === this.to);
  }

  /** List every message dated from since, halving the upper bound toward the floor when there are too many. */
  private async listWindow(context: TriggerContext, since: number, minimumUpperBound: number): Promise<InboundMessageWindow> {
    const floor = minimumUpperBound !== 0 ? Math.max(minimumUpperBound, since) : since;
    let span = Math.max(unixSecond(this.client.currentTime()) - floor, 0);
    let upperBound = 0;
    for (let narrowing = 0; narrowing <= maximumWindowNarrowings; narrowing++) {
      const { messages, isTruncated } = await this.listMessages(context, since, upperBound);
      if (!isTruncated) return { messages, upperBound };
      if (upperBound === floor) break;
      span = Math.floor(span / 2);
      upperBound = floor + span;
    }
    throw new Error(`Twilio listed more than ${inboundListPageSize * maximumWindowPages} messages within 2m0s of the watermark; the messageReceived Trigger cannot page through them`);
  }

  /**
   * Follow next_page_uri for at most ten pages and report whether more remain. Twilio lists newest first by
   * DateSent, so a page reaching a message dated before since ends the listing. Bounds are inclusive and also
   * applied locally, since Twilio documents them as dates.
   */
  private async listMessages(context: TriggerContext, since: number, upperBound: number): Promise<{ messages: ListedMessage[]; isTruncated: boolean }> {
    const query: Record<string, string> = { PageSize: String(inboundListPageSize), 'DateSent>': formatRFC3339Second(since) };
    if (upperBound !== 0) query['DateSent<'] = formatRFC3339Second(upperBound);
    if (this.to) query.To = this.to;
    let target = `${this.client.messagesURL}.json?${encodeGoQuery(query)}`;
    const messages: ListedMessage[] = [];
    for (let pageNumber = 0; pageNumber < maximumWindowPages; pageNumber++) {
      const { listed, nextPageURI } = await this.listPage(context, target);
      let hasReachedSince = false;
      for (const entry of listed) {
        if (entry.sentAtSecond !== 0 && entry.sentAtSecond < since) { hasReachedSince = true; continue; }
        if (entry.sentAtSecond !== 0 && upperBound !== 0 && entry.sentAtSecond > upperBound) continue;
        messages.push(entry);
      }
      if (!nextPageURI || hasReachedSince) return { messages, isTruncated: false };
      target = this.resolveNextPage(nextPageURI);
    }
    return { messages, isTruncated: true };
  }

  /** Accept only a next page of this account's Messages list on the configured host. */
  private resolveNextPage(nextPageURI: string): string {
    const base = new URL(`${this.client.messagesURL}.json`);
    const withoutFragment = nextPageURI.split('#')[0] ?? '';
    const queryStart = withoutFragment.indexOf('?');
    const path = queryStart < 0 ? withoutFragment : withoutFragment.slice(0, queryStart);
    const query = queryStart < 0 ? '' : withoutFragment.slice(queryStart + 1);
    if (/^[A-Za-z][A-Za-z0-9+.-]*:/.test(nextPageURI) || nextPageURI.startsWith('//') || path !== base.pathname || !query) {
      throw new Error('Twilio Messages list returned an unexpected next_page_uri');
    }
    return `${base.protocol}//${base.host}${path}?${query}`;
  }

  /** Send one Messages list request; a 429 pauses the connection for Retry-After or backs off exponentially. */
  private async listPage(context: TriggerContext, target: string): Promise<{ listed: ListedMessage[]; nextPageURI: string }> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.connection, MessageReceivedTriggerDefinition.trigger, context.signal);
    const resolved = await this.client.authorization(call, MessageReceivedTriggerDefinition.trigger.triggerName);
    if ('failure' in resolved) throw new Error(resolved.isTransient ? 'Twilio messageReceived credentials are unavailable' : resolved.failure.message);
    const exchange = await this.client.exchange(call, resolved.authorization, 'GET', target, undefined, context.signal);
    if ('error' in exchange) {
      if (context.signal.aborted) throw exchange.error;
      throw new Error('Twilio Messages list request failed before a response');
    }
    const { response } = exchange;
    if (response.status === 429) {
      throw markRateLimited(new Error(`Twilio Messages list returned HTTP 429 (error ${errorCode(response.body)})`), retryAfter(response, this.client.currentTime()));
    }
    if (response.status === 401 || response.status === 403) {
      throw new Error(`Twilio rejected the Messages list credentials (HTTP ${response.status}, error ${errorCode(response.body)}); update the connection or grant the API key Messages read access`);
    }
    if (response.status < 200 || response.status >= 300) throw new Error(`Twilio Messages list returned HTTP ${response.status} (error ${errorCode(response.body)})`);
    if (exchange.isBodyReadFailed) throw new Error('Twilio Messages list response was interrupted');
    if (exchange.isTooLarge) throw new Error('Twilio Messages list page exceeds maxResponseBytes; raise it on the connection');
    return this.decodePage(response.body, resolved.authorization);
  }

  private decodePage(contents: Uint8Array, authorization: TwilioAuthorization): { listed: ListedMessage[]; nextPageURI: string } {
    const invalidJSON = 'Twilio Messages list page is not valid JSON';
    let parsed: ReturnType<typeof parseTwilioJSON>;
    try { parsed = parseTwilioJSON(Buffer.from(contents).toString('utf8')); } catch { throw new Error(invalidJSON); }
    const page = parsed.value;
    if (page === null || typeof page !== 'object' || Array.isArray(page)) throw new Error(invalidJSON);
    const { messages: resources, next_page_uri: nextPageURI } = page as Record<string, unknown>;
    if (!Array.isArray(resources) || (nextPageURI !== undefined && nextPageURI !== null && typeof nextPageURI !== 'string')) throw new Error(invalidJSON);
    // Go's json.Unmarshal into the typed resource rejects the whole page for a mistyped string field.
    for (const resource of resources) {
      if (resource === null || typeof resource !== 'object' || Array.isArray(resource)) throw new Error(invalidJSON);
      const fields = resource as Record<string, unknown>;
      if (resourceStringFields.some(name => fields[name] !== undefined && fields[name] !== null && typeof fields[name] !== 'string')) throw new Error(invalidJSON);
    }
    const listed = resources.map((resource: Record<string, unknown>) => {
      let message: Message;
      try { message = this.client.convertMessageResource(resource, parsed.rawIntegerTokens, authorization); }
      catch { throw new Error('Twilio Messages list page has an invalid message'); }
      // Go decodes a lone surrogate escape as U+FFFD; a body that quotes the credential is dropped.
      let body = (typeof resource.body === 'string' ? resource.body : '').replace(loneSurrogatePattern, '�');
      if (authorization.password && body.includes(authorization.password)) body = '';
      const sentAtSecond = message.sentAt === zeroTime ? 0 : unixSecond(new Date(message.sentAt));
      return { message, body, sentAtSecond };
    });
    return { listed, nextPageURI: typeof nextPageURI === 'string' ? nextPageURI : '' };
  }

  /** Add upserted to the snapshot and drop keys dated before the new lookback. */
  private encodeCursor(watermark: number, previous: KeyedSnapshot | undefined, upserted: InboundSnapshotKey[]): Cursor {
    const cutoff = watermark - inboundLookbackSeconds;
    const expired = (previous?.entries ?? []).filter(entry => {
      const second = inboundSnapshotKeySecond(entry.key);
      return second === undefined || second < cutoff;
    }).map(entry => entry.key);
    let snapshot: KeyedSnapshot;
    try { snapshot = applyKeyedSnapshotChanges(previous, upserted, expired, inboundSnapshotOptions); }
    catch (error) { throw new Error(`Twilio messageReceived snapshot: ${error instanceof Error ? error.message : 'invalid'}`, { cause: error }); }
    const to = this.to ? `,"to":${encodeGoJSONValue(this.to)}` : '';
    return { token: `{"accountSid":${encodeGoJSONValue(this.client.accountSid)}${to},"watermark":${watermark}}`, snapshot };
  }
}
