// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectionRef, TriggerContext, TriggerEvent } from '@superdurable/connectors-sdk';
import { MaxChangesPerPage, markCursorInvalid, markRateLimited, newTriggerCredentialCall, waitForPollRequest, type ChangePage, type Cursor } from '@superdurable/connectors-sdk/pollingtrigger';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import { actionLookbackNanoseconds, compareASCII, decodeCardActionCursor, encodeCardActionCursor, formatGoJSONTime, formatTrelloTime, parseGoTime, seenActionIdsWithin, type CardActionCursor, type GoTime } from './card_action_cursor.js';
import { CardActionCommented, CardActionUpdated, CardChangedTriggerDefinition, WatchedCardActionTypes, cardChangeEventId, type ActionCard, type ActionList, type CardActionType, type CardChangedEvent, type CardChangedTriggerConfiguration } from './card_changed.js';
import { TrelloCredentialReflectionError, trelloRateLimitMessage, trelloRetryDelay, type TrelloClient, type TrelloExchange } from './client.js';
import type { MemberReference } from './generated/models.js';

const operation = 'cardChanged';
const subject = 'board action history';
const pageSize = MaxChangesPerPage;
/** Bounds one walk back through the history to 10,000 actions. */
const maximumPagesPerPoll = 20;
const trelloIdPattern = /^[0-9a-fA-F]{24}$/;
const int64Minimum = -(2n ** 63n), int64Maximum = 2n ** 63n - 1n;

/** A JSON number literal kept as written, so decoding applies Go's integer rules. */
class GoNumber { constructor(readonly source: string) {} }

interface CardAction { actionId: string; occurredAt: GoTime; event: CardChangedEvent }

/**
 * Reads one board's card actions for the poll Trigger kit, at parity with the Go connector. A poller owns it
 * and calls it one request at a time.
 */
export class CardActionFetcher {
  private readonly boardId: string;

  /** Bind the client, connection, and watched board; validate the configuration beforehand. */
  constructor(private readonly client: TrelloClient, private readonly connection: ConnectionRef, configuration: CardChangedTriggerConfiguration) {
    this.boardId = configuration.boardId ?? '';
  }

  /** Read the newest page and keep only its position, so earlier actions are never delivered. */
  async seedCursor(context: TriggerContext): Promise<Cursor> {
    const newest = await this.listActionPage(context, undefined, '');
    const position: CardActionCursor = { boardId: this.boardId, watermark: '', seenActionIds: [], floorActionId: '' };
    if (newest.length) {
      const watermark = latestAction(newest).occurredAt;
      position.watermark = formatTrelloTime(watermark);
      [position.seenActionIds, position.floorActionId] = seenActionIdsWithin([], '', newest.map(action => action.actionId), watermark);
    }
    return encodeCardActionCursor(position);
  }

  /**
   * Walk the actions after the lookback window, drop the delivered ones, and return the oldest page in time
   * order. An undecodable cursor, one for another board, a 400 naming since or before, or a backlog over 10,000
   * actions is marked invalid, so the kit records a new baseline.
   */
  async fetchChanges(context: TriggerContext, cursor: Cursor): Promise<ChangePage<TriggerEvent<CardChangedEvent>>> {
    let position: CardActionCursor;
    try { position = decodeCardActionCursor(cursor.token, this.boardId); }
    catch (error) { throw markCursorInvalid(error); }
    let watermark = position.watermark ? parseGoTime(position.watermark) : undefined;
    const since = watermark ? watermark.unixNanoseconds - actionLookbackNanoseconds : undefined;
    const listed = await this.walkActionsSince(context, since);
    const seen = new Set(position.seenActionIds);
    let fresh = listed.filter(action => {
      const actionId = action.actionId.toLowerCase();
      return !seen.has(actionId) && compareASCII(actionId, position.floorActionId) > 0 && (since === undefined || action.occurredAt.unixNanoseconds >= since);
    }).sort(compareActionsOldestFirst);
    const hasMorePages = fresh.length > pageSize;
    fresh = fresh.slice(0, pageSize);
    if (fresh.length) {
      const latest = latestAction(fresh).occurredAt;
      if (!watermark || !(watermark.unixNanoseconds > latest.unixNanoseconds)) watermark = latest;
      position.watermark = formatTrelloTime(watermark);
    }
    [position.seenActionIds, position.floorActionId] = seenActionIdsWithin(position.seenActionIds, position.floorActionId, fresh.map(action => action.actionId), watermark);
    const changes = fresh.map(action => ({
      id: cardChangeEventId(action.actionId), occurredAt: new Date(Number(action.occurredAt.unixNanoseconds / 1_000_000n)), payload: action.event,
    }));
    return { changes, next: encodeCardActionCursor(position), hasMorePages };
  }

  /** Page from the newest action back with before=<smallest ID> until a short page. */
  private async walkActionsSince(context: TriggerContext, since: bigint | undefined): Promise<CardAction[]> {
    const collected: CardAction[] = [];
    let before = '';
    for (let page = 0; page < maximumPagesPerPoll; page++) {
      const listed = await this.listActionPage(context, since, before);
      collected.push(...listed);
      if (listed.length < pageSize) return collected;
      // Trello compares before against action IDs, so page from the smallest ID.
      const oldest = listed.reduce((smallest, action) => compareASCII(action.actionId.toLowerCase(), smallest.actionId.toLowerCase()) < 0 ? action : smallest).actionId;
      if (oldest === before) throw new Error('Trello actions page did not move past its before cursor');
      before = oldest;
    }
    throw markCursorInvalid(new Error(`Trello board has more than ${maximumPagesPerPoll * pageSize} card actions since the last poll`));
  }

  /** Send one GET /boards/{board}/actions request for the watched action types, newest first. */
  private async listActionPage(context: TriggerContext, since: bigint | undefined, before: string): Promise<CardAction[]> {
    await waitForPollRequest(context);
    const call = newTriggerCredentialCall(this.connection, CardChangedTriggerDefinition.trigger, context.signal);
    const query = new URLSearchParams({
      fields: 'id,idMemberCreator,type,date,data', filter: WatchedCardActionTypes.join(','), limit: String(pageSize),
      memberCreator: 'true', memberCreator_fields: 'fullName,username',
    });
    if (since !== undefined) query.set('since', formatTrelloTime({ unixNanoseconds: since, offsetSeconds: 0 }));
    if (before) query.set('before', before);
    const exchange = await this.client.exchange(call, operation, 'GET', `/boards/${encodeURIComponent(this.boardId)}/actions`, query, undefined, context.signal);
    const body = responseBodyOrThrow(exchange);
    let listed: unknown;
    try {
      listed = JSON.parse(Buffer.from(body).toString('utf8'), (_key, value: unknown, parse?: { source?: string }) =>
        typeof value === 'number' ? new GoNumber(parse?.source ?? String(value)) : value);
      if (!Array.isArray(listed)) throw new TypeError('not an array');
      listed = listed.map(decodeAction);
    } catch { throw new Error('Trello actions page is not a JSON array of actions'); }
    const resources = listed as ActionResource[];
    if (resources.length > pageSize) throw new Error(`Trello actions page has ${resources.length} actions, more than the requested ${pageSize}`);
    return resources.map(validateAction);
  }
}

/** Map a failed exchange to a poll error: 429 pauses the connection, and a 400 naming since or before reseeds. */
function responseBodyOrThrow(exchange: TrelloExchange): Uint8Array {
  if ('error' in exchange) {
    if (exchange.error instanceof TypeError && exchange.receipt.callId === undefined) {
      throw new Error('Trello cardChanged credentials are unavailable; save an API key and a token for the connection');
    }
    const status = exchange.status;
    const isFailedStatus = status !== undefined && !(status >= 200 && status < 300);
    if (isFailedStatus && (exchange.error instanceof BodyTooLargeError || exchange.error instanceof TrelloCredentialReflectionError)) {
      return responseBodyOrThrow({ response: { status, headers: exchange.headers ?? {}, body: Buffer.alloc(0) }, receipt: exchange.receipt });
    }
    if (exchange.error instanceof BodyTooLargeError) throw new Error(`Trello ${subject} response exceeds the configured size limit`);
    if (exchange.error instanceof TrelloCredentialReflectionError) throw new Error(`Trello ${subject} response contains credential material`);
    throw new Error(`Trello ${subject} is temporarily unavailable`);
  }
  const { status, headers, body } = exchange.response;
  if (status >= 200 && status < 300) return body;
  if (status === 429) {
    // Trello sends no Retry-After, so the pause defaults to one rate-limit window.
    throw markRateLimited(new Error(trelloRateLimitMessage(subject, body)), trelloRetryDelay(status, headers));
  }
  if (status === 404) throw new Error(`Trello ${subject} was not found`);
  if (status === 408 || status >= 500) throw new Error(`Trello ${subject} is temporarily unavailable`);
  if (status >= 400) {
    const message = `Trello rejected the ${subject} with HTTP ${status}`;
    const text = Buffer.from(body).toString('latin1').toLowerCase();
    if (status === 400 && (text.includes('since') || text.includes('before'))) throw markCursorInvalid(new Error(message));
    throw new Error(message);
  }
  throw new Error(`Trello returned unexpected HTTP ${status} for the ${subject}`);
}

interface ActionResource {
  actionId: string; actionType: string; date: string; memberCreatorId: string; memberCreator: MemberReference | undefined;
  card: ActionCard | undefined; boardId: string; boardName: string;
  list: ActionList | undefined; listBefore: ActionList | undefined; listAfter: ActionList | undefined;
  changedFields: string[] | undefined; text: string;
}

/** Decode one action with Go's encoding/json type rules; throws for a field of the wrong JSON type. */
function decodeAction(value: unknown): ActionResource {
  const resource = value === null ? {} : requireObject(value);
  const data = objectField(resource, 'data') ?? {};
  const cardResource = objectField(data, 'card');
  let card: ActionCard | undefined;
  if (cardResource) {
    const name = stringField(cardResource, 'name'), shortLink = stringField(cardResource, 'shortLink');
    const shortId = int64Field(cardResource, 'idShort'), listId = stringField(cardResource, 'idList'), isClosed = boolField(cardResource, 'closed');
    card = {
      id: stringField(cardResource, 'id'), ...(name ? { name } : {}), ...(shortLink ? { shortLink } : {}),
      ...(shortId ? { shortId } : {}), ...(listId ? { listId } : {}), ...(isClosed !== undefined ? { isClosed } : {}),
    };
  }
  const board = objectField(data, 'board'), old = objectField(data, 'old'), creator = objectField(resource, 'memberCreator');
  let memberCreator: MemberReference | undefined;
  if (creator) {
    const id = stringField(creator, 'id'), fullName = stringField(creator, 'fullName'), username = stringField(creator, 'username');
    if (id) memberCreator = { id, ...(fullName ? { fullName } : {}), ...(username ? { username } : {}) };
  }
  return {
    actionId: stringField(resource, 'id'), actionType: stringField(resource, 'type'), date: stringField(resource, 'date'),
    memberCreatorId: stringField(resource, 'idMemberCreator'), memberCreator, card,
    boardId: board ? stringField(board, 'id') : '', boardName: board ? stringField(board, 'name') : '',
    list: actionList(data, 'list'), listBefore: actionList(data, 'listBefore'), listAfter: actionList(data, 'listAfter'),
    changedFields: old ? Object.keys(old).sort(compareASCII) : undefined, text: stringField(data, 'text'),
  };
}

/** Check the fields cardChanged relies on, as the Go connector's cardActionResource.validate does. */
function validateAction(resource: ActionResource): CardAction {
  const actionId = resource.actionId;
  const invalid = (problem: string) => new Error(`Trello actions page has an invalid action: ${problem}`);
  if (!trelloIdPattern.test(actionId)) throw invalid('action id is not a Trello ID');
  if (!(WatchedCardActionTypes as readonly string[]).includes(resource.actionType)) throw invalid(`action ${actionId} has unrequested type ${JSON.stringify(resource.actionType)}`);
  const parsed = parseGoTime(resource.date);
  if (!parsed) throw invalid(`action ${actionId} date is not an ISO 8601 time`);
  if (!resource.card || !trelloIdPattern.test(resource.card.id)) throw invalid(`action ${actionId} has no card ID`);
  const occurredAt: GoTime = { unixNanoseconds: parsed.unixNanoseconds, offsetSeconds: 0 };
  const actionType = resource.actionType as CardActionType;
  const changedFields = actionType === CardActionUpdated ? resource.changedFields ?? [] : [];
  const commentText = actionType === CardActionCommented ? resource.text : '';
  const event: CardChangedEvent = {
    boardId: resource.boardId, ...(resource.boardName ? { boardName: resource.boardName } : {}),
    actionId, actionType, occurredAt: formatGoJSONTime(occurredAt.unixNanoseconds),
    ...(resource.memberCreatorId ? { memberCreatorId: resource.memberCreatorId } : {}),
    ...(resource.memberCreator ? { memberCreator: resource.memberCreator } : {}),
    card: resource.card,
    ...(resource.list ? { list: resource.list } : {}), ...(resource.listBefore ? { listBefore: resource.listBefore } : {}),
    ...(resource.listAfter ? { listAfter: resource.listAfter } : {}),
    ...(changedFields.length ? { changedFields } : {}), ...(commentText ? { commentText } : {}),
  };
  return { actionId, occurredAt, event };
}

function compareActionsOldestFirst(left: CardAction, right: CardAction): number {
  const order = left.occurredAt.unixNanoseconds - right.occurredAt.unixNanoseconds;
  return order < 0n ? -1 : order > 0n ? 1 : compareASCII(left.actionId.toLowerCase(), right.actionId.toLowerCase());
}

function latestAction(actions: readonly CardAction[]): CardAction {
  return actions.reduce((latest, action) => compareActionsOldestFirst(action, latest) > 0 ? action : latest);
}

function requireObject(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value) || value instanceof GoNumber) throw new TypeError('not an object');
  return value as Record<string, unknown>;
}

/** Return the value Go's encoding/json assigns to the field: the last exact or case-folded key. */
function field(document: Record<string, unknown>, name: string): unknown {
  let value: unknown = null;
  const folded = name.toLowerCase();
  for (const [key, item] of Object.entries(document)) if (key === name || (/^[\x20-\x7e]*$/.test(key) && key.toLowerCase() === folded)) value = item;
  return value;
}

function stringField(document: Record<string, unknown>, name: string): string {
  const value = field(document, name);
  if (value !== null && typeof value !== 'string') throw new TypeError(name);
  return (value ?? '') as string;
}

function int64Field(document: Record<string, unknown>, name: string): number {
  const value = field(document, name);
  if (value === null) return 0;
  if (!(value instanceof GoNumber) || !/^-?(?:0|[1-9][0-9]*)$/.test(value.source)) throw new TypeError(name);
  const integer = BigInt(value.source);
  // Go holds any int64; a magnitude beyond 2^53 cannot be represented exactly here and is rejected.
  if (integer < int64Minimum || integer > int64Maximum || !Number.isSafeInteger(Number(integer))) throw new TypeError(name);
  return Number(integer);
}

function boolField(document: Record<string, unknown>, name: string): boolean | undefined {
  const value = field(document, name);
  if (value !== null && typeof value !== 'boolean') throw new TypeError(name);
  return (value ?? undefined) as boolean | undefined;
}

function objectField(document: Record<string, unknown>, name: string): Record<string, unknown> | undefined {
  const value = field(document, name);
  return value === null ? undefined : requireObject(value);
}

function actionList(data: Record<string, unknown>, name: string): ActionList | undefined {
  const resource = objectField(data, name);
  if (!resource) return undefined;
  const id = stringField(resource, 'id'), listName = stringField(resource, 'name');
  return id ? { id, ...(listName ? { name: listName } : {}) } : undefined;
}
