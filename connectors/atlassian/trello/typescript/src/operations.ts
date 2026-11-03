// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { Fence } from '@superdurable/connectors-sdk/singledispatch';
import { BodyTooLargeError } from '@superdurable/connectors-sdk/providerhttp';
import type { ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { AddCommentDefinition, CreateCardDefinition, GetCardDefinition, ListCardsDefinition, UpdateCardDefinition } from './generated/definitions.js';
import type { AddCommentInput, AddCommentOutput, Card, CreateCardInput, CreateCardOutput, GetCardInput, ListCardsInput, ListCardsOutput, UpdateCardInput, UpdateCardOutput } from './generated/models.js';
import { TrelloClient, TrelloCredentialReflectionError, trelloFailure, trelloRateLimitMessage, trelloRetryDelay, trelloStatusKind, type TrelloExchange } from './client.js';

const idPattern = /^[0-9a-fA-F]{24}$/;
const shortLinkPattern = /^[0-9A-Za-z]{8}$/;
const summaryFields = 'id,shortLink,name,idBoard,idList,closed,due,dueComplete,start,idLabels,labels,idMembers,pos,url,shortUrl,dateLastActivity';
const detailFields = `${summaryFields},desc`;
const zeroCard: Card = { id: '', name: '', boardId: '', listId: '', isClosed: false, isDueComplete: false, position: 0 };
const zeroPage: ListCardsOutput = { cards: [], scannedCardCount: 0 };

type Classified = { kind: 'success' | 'retry' | 'notFound' | 'rejected' | 'invalid' | 'defect' | 'uncertain'; failure?: Failure; retryAfterMs: number; receipt: ReceiptDraft; response?: ProviderResponse; isProvablyNotSent?: boolean };

function classify(exchange: TrelloExchange, operation: string, subject: string, mode: 'read' | 'unkeyed' | 'repeatable'): Classified {
  const base = { receipt: exchange.receipt, retryAfterMs: 0 };
  if ('error' in exchange) {
    if (exchange.error instanceof TypeError && exchange.receipt.callId === undefined) return { ...base, kind: 'defect', failure: trelloFailure(operation, 'AUTHENTICATION', 'Trello connection credentials are unavailable') };
    if (exchange.error instanceof BodyTooLargeError && exchange.status && !(exchange.status >= 200 && exchange.status < 300)) {
      return classify({ response: { status: exchange.status, headers: exchange.headers ?? {}, body: Buffer.alloc(0) }, receipt: exchange.receipt }, operation, subject, mode);
    }
    if (exchange.error instanceof TrelloCredentialReflectionError) {
      if (exchange.status && !(exchange.status >= 200 && exchange.status < 300)) {
        return classify({ response: { status: exchange.status, headers: exchange.headers ?? {}, body: Buffer.alloc(0) }, receipt: exchange.receipt }, operation, subject, mode);
      }
      return { ...base, kind: mode === 'unkeyed' ? 'uncertain' : 'invalid', failure: trelloFailure(operation, 'PROTOCOL', `Trello ${subject} response contains credential material`) };
    }
    if (exchange.error instanceof BodyTooLargeError && exchange.status && exchange.status >= 200 && exchange.status < 300) {
      const kind = mode === 'unkeyed' ? 'uncertain' : 'invalid';
      return { ...base, kind, failure: trelloFailure(operation, 'RESPONSE_TOO_LARGE', `Trello ${subject} response exceeds the configured size limit`) };
    }
    const kind = mode === 'read' || mode === 'repeatable' || exchange.isProvablyNotSent ? 'retry' : 'uncertain';
    return { ...base, kind, isProvablyNotSent: exchange.isProvablyNotSent, failure: trelloFailure(operation, 'TRANSPORT', `Trello ${subject} outcome is unknown`) };
  }
  const { response } = exchange;
  const status = response.status;
  if (status >= 200 && status < 300) return { ...base, kind: 'success', response };
  if (status === 429) return { ...base, kind: 'retry', retryAfterMs: trelloRetryDelay(status, response.headers), failure: trelloFailure(operation, 'RATE_LIMIT', trelloRateLimitMessage(subject, response.body)) };
  if (status === 503 && mode === 'unkeyed') return { ...base, kind: 'retry', retryAfterMs: trelloRetryDelay(status, response.headers), failure: trelloFailure(operation, 'AVAILABILITY', `Trello ${subject} is temporarily unavailable`) };
  if (status === 404) return { ...base, kind: 'notFound', failure: trelloFailure(operation, 'NOT_FOUND', `Trello ${subject} was not found`) };
  if (status === 408 || status >= 500) {
    if (mode === 'unkeyed') return { ...base, kind: 'uncertain', failure: trelloFailure(operation, 'AVAILABILITY', `Trello ${subject} outcome is unknown after HTTP ${status}`) };
    return { ...base, kind: 'retry', retryAfterMs: trelloRetryDelay(status, response.headers), failure: trelloFailure(operation, 'AVAILABILITY', `Trello ${subject} is temporarily unavailable`) };
  }
  if (status >= 400 && status < 500) {
    if (status === 409 && mode === 'unkeyed') return { ...base, kind: 'uncertain', failure: trelloFailure(operation, 'CONFLICT', `Trello ${subject} conflicts with provider state and cannot be retried without a key`) };
    return { ...base, kind: 'rejected', failure: trelloFailure(operation, trelloStatusKind(status), `Trello rejected the ${subject} with HTTP ${status}`) };
  }
  return { ...base, kind: mode === 'unkeyed' ? 'uncertain' : 'invalid', failure: trelloFailure(operation, 'PROTOCOL', `Trello returned unexpected HTTP ${status} for the ${subject}`) };
}

function queryOutcome<T>(result: Classified, zero: T): QueryAttempt<T> {
  if (result.kind === 'retry') return newQueryRetry(result.failure!, result.retryAfterMs);
  const branch = result.kind === 'notFound' ? 'notFound' : result.kind === 'rejected' ? 'providerRejected' : result.kind === 'defect' ? 'defect' : 'invalidResponse';
  return newQueryBranch(branch, zero, result.receipt, result.failure);
}
function mutationOutcome<T>(result: Classified, output: T, hasNotFound: boolean): MutationAttempt<T> {
  if (result.kind === 'retry') return newMutationRetry(result.failure!, result.retryAfterMs);
  if (result.kind === 'uncertain') return newMutationUncertain(output, result.failure!, result.receipt);
  const branch = result.kind === 'notFound' ? (hasNotFound ? 'notFound' : 'providerRejected') : result.kind === 'rejected' ? 'providerRejected' : result.kind === 'defect' ? 'defect' : 'invalidResponse';
  return newMutationBranch(branch, output, result.receipt, result.failure);
}
function defect(operation: string, message: string): Failure { return trelloFailure(operation, 'VALIDATION', message); }
function requireId(value: string, field: string): string {
  const trimmed = value.trim();
  if (!idPattern.test(trimmed)) throw new TypeError(`${field} must be a Trello ID, 24 hexadecimal characters such as 5b6893f01cb3228998cf629e`);
  return trimmed;
}
function optionalId(value: string | undefined, field: string): string { return value?.trim() ? requireId(value, field) : ''; }
function idList(values: string[] | undefined, field: string): string[] {
  const ids = (values ?? []).map(value => requireId(value, field));
  if (ids.length > 50 || new Set(ids.map(value => value.toLowerCase())).size !== ids.length) throw new TypeError(`${field} must contain at most 50 distinct IDs`);
  return ids;
}
function plainText(value: string, field: string, required = false): void {
  const hasInvalidControl = [...value].some(character => {
    const code = character.codePointAt(0)!;
    return (code < 32 && code !== 9 && code !== 10 && code !== 13) || code === 127;
  });
  if ((required && !value.trim()) || [...value].length > 16_384 || hasInvalidControl) throw new TypeError(`${field} is invalid`);
}
function positionValue(value: string | undefined): number | string | undefined {
  const text = value?.trim() ?? '';
  if (!text) return undefined;
  if (text === 'top' || text === 'bottom') return text;
  const number = Number(text);
  if (!Number.isFinite(number) || number <= 0) throw new TypeError('position must be top, bottom, or a positive number');
  return number;
}
function timeValue(value: string | null | undefined): string | undefined {
  if (!value) return undefined;
  const instant = new Date(value);
  if (Number.isNaN(instant.getTime())) throw new TypeError('timestamp is not an ISO 8601 date-time');
  return instant.toISOString();
}
function parseObject(body: Uint8Array): Record<string, unknown> {
  const value: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!value || Array.isArray(value) || typeof value !== 'object') throw new TypeError('invalid Trello object');
  return value as Record<string, unknown>;
}
function string(value: unknown): string { return typeof value === 'string' ? value : ''; }
function bool(value: unknown): boolean { return value === true; }
function date(value: unknown): string | undefined {
  const timestamp = typeof value === 'string' && value ? timeValue(value) : undefined;
  return timestamp?.replace(/(\.\d*?)0+Z$/, '$1Z').replace(/\.Z$/, 'Z');
}
function labelIds(card: Card): string[] { return card.labelIds ?? []; }
function equalIds(first: string[], second: string[]): boolean { return first.length === second.length && first.every(value => second.some(other => other.toLowerCase() === value.toLowerCase())); }

function decodeCard(raw: Record<string, unknown>, detail: boolean): Card {
  const id = requireId(string(raw.id), 'card id');
  const boardId = requireId(string(raw.idBoard), 'card board id');
  const listId = requireId(string(raw.idList), 'card list id');
  const labels: NonNullable<Card['labels']> = [];
  const uniqueIds: string[] = [];
  let hasMoreLabels = false;
  for (const item of [...(Array.isArray(raw.idLabels) ? raw.idLabels : []), ...(Array.isArray(raw.labels) ? raw.labels : [])]) {
    const candidate = typeof item === 'string' ? item : item && typeof item === 'object' ? item as Record<string, unknown> : undefined;
    const labelId = typeof candidate === 'string' ? candidate : string(candidate?.id);
    if (!idPattern.test(labelId)) continue;
    if (typeof candidate === 'object' && candidate && !labels.some(label => label.id.toLowerCase() === labelId.toLowerCase()) && labels.length < 50) {
      labels.push({ id: labelId, ...(string(candidate.name) ? { name: string(candidate.name) } : {}), ...(string(candidate.color) ? { color: string(candidate.color) } : {}) });
    }
    if (uniqueIds.some(value => value.toLowerCase() === labelId.toLowerCase())) continue;
    if (uniqueIds.length >= 50) { hasMoreLabels = true; continue; }
    uniqueIds.push(labelId);
  }
  const memberIds = (Array.isArray(raw.idMembers) ? raw.idMembers : []).filter((value): value is string => typeof value === 'string' && idPattern.test(value)).slice(0, 50);
  const description = detail ? string(raw.desc) : '';
  const characters = [...description];
  const list = raw.list && typeof raw.list === 'object' && !Array.isArray(raw.list) ? raw.list as Record<string, unknown> : undefined;
  const members = detail && Array.isArray(raw.members) ? raw.members.filter((value): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value) && idPattern.test(string((value as Record<string, unknown>).id))).slice(0, 50).map(value => ({ id: string(value.id), ...(string(value.fullName) ? { fullName: string(value.fullName) } : {}), ...(string(value.username) ? { username: string(value.username) } : {}) })) : [];
  return {
    id, name: string(raw.name), boardId, listId, isClosed: bool(raw.closed), isDueComplete: bool(raw.dueComplete), position: typeof raw.pos === 'number' ? raw.pos : 0,
    ...(shortLinkPattern.test(string(raw.shortLink)) ? { shortLink: string(raw.shortLink) } : {}),
    ...(detail && description ? { description: characters.slice(0, 16_384).join('') } : {}),
    ...(detail && characters.length > 16_384 ? { isDescriptionTruncated: true } : {}),
    ...(detail && list && string(list.id) === listId && string(list.name) ? { listName: string(list.name) } : {}),
    ...(date(raw.due) ? { due: date(raw.due) } : {}), ...(date(raw.start) ? { start: date(raw.start) } : {}),
    ...(uniqueIds.length ? { labelIds: uniqueIds } : {}), ...(labels.length ? { labels } : {}), ...(hasMoreLabels ? { hasMoreLabels: true } : {}),
    ...(memberIds.length ? { memberIds } : {}), ...(members.length ? { members } : {}),
    ...(string(raw.url) ? { url: string(raw.url) } : {}), ...(string(raw.shortUrl) ? { shortUrl: string(raw.shortUrl) } : {}),
    ...(date(raw.dateLastActivity) ? { lastActivityAt: date(raw.dateLastActivity) } : {}),
  };
}

function detailQuery(): URLSearchParams { return new URLSearchParams({ fields: detailFields, members: 'true', member_fields: 'fullName,username', list: 'true' }); }
async function readCard(client: TrelloClient, call: Call, operation: string, id: string): Promise<{ card: Card; receipt: ReceiptDraft } | { classified: Classified }> {
  const exchange = await client.exchange(call, operation, 'GET', `/cards/${id}`, detailQuery());
  const classified = classify(exchange, operation, 'card', 'read');
  if (classified.kind !== 'success') return { classified: { ...classified, receipt: { ...classified.receipt, providerObjectId: id } } };
  const receipt = { ...classified.receipt, providerObjectId: id };
  try {
    const card = decodeCard(parseObject(classified.response!.body), true);
    if (card.id.toLowerCase() !== id.toLowerCase()) throw new TypeError('response describes another card');
    return { card, receipt };
  } catch { return { classified: { kind: 'invalid', retryAfterMs: 0, receipt, failure: trelloFailure(operation, 'PROTOCOL', 'Trello returned an invalid card') } }; }
}

/** Query one bounded board or list card page. */
export class ListCardsOperation implements Query<ListCardsInput, ListCardsOutput> {
  /** Bind the authenticated client. */ constructor(private readonly client: TrelloClient) {}
  /** Return stable operation metadata. */ definition() { return ListCardsDefinition; }
  /** Validate, read, and filter one Trello page. */
  async invoke(call: Call, input: ListCardsInput): Promise<QueryAttempt<ListCardsOutput>> {
    let path: string; let query: URLSearchParams; let pageSize: number;
    try {
      const boardId = optionalId(input.boardId, 'boardId'); const listId = optionalId(input.listId, 'listId');
      if (Boolean(boardId) === Boolean(listId)) throw new TypeError('set exactly one of boardId and listId');
      const status = input.status || 'open';
      if (!['open', 'closed', 'all'].includes(status)) throw new TypeError('status must be open, closed, or all');
      pageSize = input.pageSize || 50;
      if (!Number.isSafeInteger(pageSize) || pageSize < 1 || pageSize > 100) throw new TypeError('pageSize must be between 1 and 100');
      if (input.dueBefore && input.dueAfter && Date.parse(input.dueAfter) >= Date.parse(input.dueBefore)) throw new TypeError('dueAfter must be earlier than dueBefore');
      if ((input.dueBefore || input.dueAfter) && input.hasDueDate === false) throw new TypeError('due range needs a due date');
      if (input.dueBefore) timeValue(input.dueBefore);
      if (input.dueAfter) timeValue(input.dueAfter);
      path = boardId ? `/boards/${boardId}/cards` : `/lists/${listId}/cards`;
      query = new URLSearchParams({ filter: status, fields: summaryFields, limit: String(pageSize), sort: '-id' });
      if (input.before?.trim()) query.set('before', requireId(input.before, 'before'));
    } catch (error) { return newQueryBranch('defect', zeroPage, {}, defect('listCards', (error as Error).message)); }
    const classified = classify(await this.client.exchange(call, 'listCards', 'GET', path, query), 'listCards', 'card list', 'read');
    if (classified.kind !== 'success') return queryOutcome(classified, zeroPage);
    try {
      const raw: unknown = JSON.parse(Buffer.from(classified.response!.body).toString('utf8'));
      if (!Array.isArray(raw) || raw.length > pageSize) throw new TypeError('invalid card page');
      const cards = raw.map(value => decodeCard(value as Record<string, unknown>, false));
      const filtered = cards.filter(card => {
        const due = card.due ? Date.parse(card.due) : undefined;
        return (input.hasDueDate === undefined || input.hasDueDate === (due !== undefined)) &&
          (input.isDueComplete === undefined || input.isDueComplete === card.isDueComplete) &&
          (!input.dueBefore || (due !== undefined && due < Date.parse(input.dueBefore))) &&
          (!input.dueAfter || (due !== undefined && due >= Date.parse(input.dueAfter)));
      });
      const oldest = cards.reduce((id, card) => !id || card.id.toLowerCase() < id.toLowerCase() ? card.id : id, '');
      return newQueryBranch('listed', { cards: filtered, scannedCardCount: cards.length, ...(cards.length === pageSize ? { nextBefore: oldest } : {}) }, classified.receipt);
    } catch { return newQueryBranch('invalidResponse', zeroPage, classified.receipt, trelloFailure('listCards', 'PROTOCOL', 'Trello returned an invalid card page')); }
  }
}

/** Query one card with members, labels, and description. */
export class GetCardOperation implements Query<GetCardInput, Card> {
  /** Bind the authenticated client. */ constructor(private readonly client: TrelloClient) {}
  /** Return stable operation metadata. */ definition() { return GetCardDefinition; }
  /** Read one card by its Trello ID. */
  async invoke(call: Call, input: GetCardInput): Promise<QueryAttempt<Card>> {
    let id: string;
    try { id = requireId(input.cardId, 'cardId'); }
    catch (error) { return newQueryBranch('defect', zeroCard, {}, defect('getCard', (error as Error).message)); }
    const result = await readCard(this.client, call, 'getCard', id);
    return 'card' in result ? newQueryBranch('found', result.card, result.receipt) : queryOutcome(result.classified, zeroCard);
  }
}

async function singleDispatch<Output>(client: TrelloClient, call: Call, operation: string, subject: string, path: string, payload: unknown, requested: Output, successBranch: string, hasNotFound: boolean, decode: (response: ProviderResponse) => { output: Output; objectId: string }): Promise<MutationAttempt<Output>> {
  let fence: Fence;
  try { fence = new Fence(call, 30_000); }
  catch { return newMutationRetry(trelloFailure(operation, 'AVAILABILITY', 'Dex attempt cannot checkpoint the Trello write')); }
  const decision = fence.decide();
  if (decision.kind !== 'sendAllowed') return newMutationUncertain(requested, trelloFailure(operation, 'TRANSPORT', 'an earlier attempt of this Step may have sent the request, so it is not sent again'), client.receipt(call));
  let exchange: TrelloExchange;
  try { exchange = await fence.dispatch(signal => client.exchange(call, operation, 'POST', path, undefined, payload, signal)); }
  catch { return newMutationRetry(trelloFailure(operation, 'AVAILABILITY', 'Dex did not record the dispatch checkpoint, so nothing was sent to Trello')); }
  const classified = classify(exchange, operation, subject, 'unkeyed');
  if (classified.kind === 'retry' || classified.kind === 'defect') {
    try { await fence.recordCanResend(); } catch { /* A lost release cannot authorize a second send. */ }
  }
  if (classified.kind !== 'success') return mutationOutcome(classified, requested, hasNotFound);
  try {
    const result = decode(classified.response!);
    return newMutationBranch(successBranch, result.output, { ...classified.receipt, providerObjectId: result.objectId });
  } catch { return newMutationUncertain(requested, trelloFailure(operation, 'PROTOCOL', `Trello accepted the ${subject} but returned an unusable reference`), classified.receipt); }
}

/** Create one card after a Dex single-dispatch checkpoint. */
export class CreateCardOperation implements Mutation<CreateCardInput, CreateCardOutput> {
  /** Bind the authenticated client. */ constructor(private readonly client: TrelloClient) {}
  /** Return stable operation metadata. */ definition() { return CreateCardDefinition; }
  /** Derive the receipt key from the Dex Call ID; Trello receives no key. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate and create one card without replaying an ambiguous write. */
  invoke(call: Call, input: CreateCardInput): Promise<MutationAttempt<CreateCardOutput>> {
    const requested: CreateCardOutput = { name: input.name.trim(), listId: input.listId.trim() };
    let payload: Record<string, unknown>;
    try {
      const listId = requireId(input.listId, 'listId');
      plainText(input.name, 'name', true);
      const name = input.name.trim();
      if (/[\r\n\t]/.test(name)) throw new TypeError('name must be one line');
      plainText(input.description ?? '', 'description');
      const labelIDs = idList(input.labelIds, 'labelIds');
      const memberIDs = idList(input.memberIds, 'memberIds');
      const position = positionValue(input.position);
      payload = { idList: listId, name,
        ...(input.description?.trim() ? { desc: input.description } : {}),
        ...(input.due ? { due: timeValue(input.due) } : {}),
        ...(labelIDs.length ? { idLabels: labelIDs.join(',') } : {}),
        ...(memberIDs.length ? { idMembers: memberIDs.join(',') } : {}),
        ...(position !== undefined ? { pos: position } : {}),
      };
    } catch (error) { return Promise.resolve(newMutationBranch('defect', requested, {}, defect('createCard', (error as Error).message))); }
    return singleDispatch(this.client, call, 'createCard', 'card', '/cards', payload, requested, 'created', false, response => {
      const body = parseObject(response.body);
      const cardId = requireId(string(body.id), 'created card id');
      return { objectId: cardId, output: {
        ...requested, cardId,
        ...(idPattern.test(string(body.idBoard)) ? { boardId: string(body.idBoard) } : {}),
        ...(shortLinkPattern.test(string(body.shortLink)) ? { shortLink: string(body.shortLink) } : {}),
        ...(string(body.url) ? { url: string(body.url) } : {}),
        ...(string(body.shortUrl) ? { shortUrl: string(body.shortUrl) } : {}),
      } };
    });
  }
}

/** Add one comment after a Dex single-dispatch checkpoint. */
export class AddCommentOperation implements Mutation<AddCommentInput, AddCommentOutput> {
  /** Bind the authenticated client. */ constructor(private readonly client: TrelloClient) {}
  /** Return stable operation metadata. */ definition() { return AddCommentDefinition; }
  /** Derive the receipt key from the Dex Call ID; Trello receives no key. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate and add one comment without replaying an ambiguous write. */
  invoke(call: Call, input: AddCommentInput): Promise<MutationAttempt<AddCommentOutput>> {
    const requested: AddCommentOutput = { cardId: input.cardId.trim() };
    let cardId: string;
    try { cardId = requireId(input.cardId, 'cardId'); plainText(input.text, 'text', true); }
    catch (error) { return Promise.resolve(newMutationBranch('defect', requested, {}, defect('addComment', (error as Error).message))); }
    return singleDispatch(this.client, call, 'addComment', 'comment', `/cards/${cardId}/actions/comments`, { text: input.text }, requested, 'added', true, response => {
      const body = parseObject(response.body);
      const commentId = requireId(string(body.id), 'comment id');
      return { objectId: commentId, output: { ...requested, commentId,
        ...(idPattern.test(string(body.idMemberCreator)) ? { authorMemberId: string(body.idMemberCreator) } : {}),
        ...(date(body.date) ? { createdAt: date(body.date) } : {}),
      } };
    });
  }
}

/** Set absolute Trello card fields and confirm the final card by read-back. */
export class UpdateCardOperation implements Mutation<UpdateCardInput, UpdateCardOutput> {
  /** Bind the authenticated client. */ constructor(private readonly client: TrelloClient) {}
  /** Return stable operation metadata. */ definition() { return UpdateCardDefinition; }
  /** Derive the receipt key from the Dex Call ID; repeatable writes send no provider key. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate, apply, and confirm a repeat-safe card update. */
  async invoke(call: Call, input: UpdateCardInput): Promise<MutationAttempt<UpdateCardOutput>> {
    const output: UpdateCardOutput = { cardId: input.cardId.trim() };
    let cardId: string; let fields: Record<string, unknown>; let addLabels: string[]; let removeLabels: string[];
    try {
      cardId = requireId(input.cardId, 'cardId');
      const listId = optionalId(input.listId, 'listId'); const boardId = optionalId(input.boardId, 'boardId');
      if (boardId && !listId) throw new TypeError('boardId requires listId');
      if (input.due && input.shouldClearDue) throw new TypeError('set at most one of due and shouldClearDue');
      addLabels = idList(input.addLabelIds ?? undefined, 'addLabelIds'); removeLabels = idList(input.removeLabelIds ?? undefined, 'removeLabelIds');
      if (addLabels.some(value => removeLabels.some(other => other.toLowerCase() === value.toLowerCase()))) throw new TypeError('label is both added and removed');
      if (input.labelIds != null && (addLabels.length || removeLabels.length)) throw new TypeError('set either labelIds or label changes');
      fields = {
        ...(listId ? { idList: listId } : {}), ...(boardId ? { idBoard: boardId } : {}),
        ...(positionValue(input.position) !== undefined ? { pos: positionValue(input.position) } : {}),
        ...(input.isClosed !== undefined ? { closed: input.isClosed } : {}),
        ...(input.due ? { due: timeValue(input.due) } : {}), ...(input.shouldClearDue ? { due: null } : {}),
        ...(input.isDueComplete !== undefined ? { dueComplete: input.isDueComplete } : {}),
        ...(input.labelIds !== undefined && input.labelIds !== null ? { idLabels: idList(input.labelIds, 'labelIds').join(',') } : {}),
        ...(input.memberIds !== undefined && input.memberIds !== null ? { idMembers: idList(input.memberIds, 'memberIds').join(',') } : {}),
      };
      if (!Object.keys(fields).length && !addLabels.length && !removeLabels.length) throw new TypeError('set at least one card change');
    } catch (error) { return newMutationBranch('defect', output, {}, defect('updateCard', (error as Error).message)); }
    if (addLabels.length || removeLabels.length) {
      const current = await readCard(this.client, call, 'updateCard', cardId);
      if ('classified' in current) return mutationOutcome(current.classified, output, true);
      if (current.card.hasMoreLabels) return newMutationBranch('defect', output, current.receipt, defect('updateCard', 'card has too many labels for an incremental change'));
      const labels = labelIds(current.card).filter(value => !removeLabels.some(other => other.toLowerCase() === value.toLowerCase()));
      for (const value of addLabels) if (!labels.some(other => other.toLowerCase() === value.toLowerCase())) labels.push(value);
      if (labels.length > 50) return newMutationBranch('defect', output, current.receipt, defect('updateCard', 'card would have too many labels'));
      fields.idLabels = labels.join(',');
    }
    const written = classify(await this.client.exchange(call, 'updateCard', 'PUT', `/cards/${cardId}`, undefined, fields), 'updateCard', 'card update', 'repeatable');
    if (written.kind !== 'success') return mutationOutcome({ ...written, receipt: { ...written.receipt, providerObjectId: cardId } }, output, true);
    const current = await readCard(this.client, call, 'updateCard', cardId);
    if ('classified' in current) return mutationOutcome(current.classified, output, true);
    const card = current.card;
    const matches = (!fields.idList || String(fields.idList).toLowerCase() === card.listId.toLowerCase()) &&
      (!fields.idBoard || String(fields.idBoard).toLowerCase() === card.boardId.toLowerCase()) &&
      (fields.closed === undefined || fields.closed === card.isClosed) &&
      (fields.due === undefined || (fields.due === null ? !card.due : Boolean(card.due && Date.parse(card.due) === Date.parse(String(fields.due))))) &&
      (fields.dueComplete === undefined || fields.dueComplete === card.isDueComplete) &&
      (fields.idLabels === undefined || (!card.hasMoreLabels && equalIds(labelIds(card), String(fields.idLabels).split(',').filter(Boolean)))) &&
      (fields.idMembers === undefined || equalIds(card.memberIds ?? [], String(fields.idMembers).split(',').filter(Boolean)));
    if (!matches) return newMutationBranch('invalidResponse', output, current.receipt, trelloFailure('updateCard', 'PROTOCOL', 'Trello accepted the update but the read-back did not show the requested change'));
    return newMutationBranch('updated', { ...output, card }, current.receipt);
  }
}
