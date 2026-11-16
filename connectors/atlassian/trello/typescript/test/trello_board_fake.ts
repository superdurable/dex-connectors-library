// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ProviderRequest, ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { parseGoTime } from '../src/card_action_cursor.js';

/**
 * One Trello board's action history and cards, as the Go connector's internal/trelloboardfake serves them:
 * GET /1/boards/{board}/actions honors filter, limit, since (exclusive), and before, newest first, and GET
 * /1/cards/{card} returns a card. It requires the test key and token. Pass send to TrelloClient's send option.
 */

/** The only credentials the fake accepts. */ export const APIKey = 'fake-trello-key';
/** The only token the fake accepts. */ export const Token = 'fake-trello-token';
/** The member every action is recorded for. */ export const MemberID = '5a1e4c7b2f0d3e6a9b8c7d6e';
/** The board's To do list. */ export const TodoListID = '5b6893f01cb3228998cf62a1';
/** The board's Done list. */ export const DoneListID = '5b6893f01cb3228998cf62a2';
const listNames: Record<string, string> = { [TodoListID]: 'To do', [DoneListID]: 'Done' };

/** One request the board answered: its path, decoded query, and status. */
export interface RecordedRequest { path: string; query: Record<string, string>; status: number }

interface StoredAction { document: Record<string, unknown>; actionId: string; actionType: string; occurredNanoseconds: bigint }
type StoredCard = Record<string, unknown> & { id: string; idList: string; name: string; idShort: number };

/** A fake board; tests drive it while the connector polls it. */
export class TrelloBoard {
  /** The board's 24-character ID; tests may replace it to model a changed binding. */ boardId: string;
  /** Stored actions in append order. */ readonly actions: StoredAction[] = [];
  /** Current cards by ID. */ readonly cards = new Map<string, StoredCard>();
  private sequence: bigint = BigInt(Date.now()) * 1_000_000n;
  private pendingRateLimits = 0;
  private shouldRejectNextCursor = false;
  private readonly recorded: RecordedRequest[] = [];

  /** Create a board with a unique ID, or the given one. */
  constructor(boardId = '') { this.boardId = boardId || this.newObjectId(Date.now() / 1000); }

  /** Mimic Trello's Mongo IDs: four bytes of creation seconds, then a counter. */
  newObjectId(createdSeconds: number): string {
    this.sequence++;
    return (Math.floor(createdSeconds) >>> 0).toString(16).padStart(8, '0') + (this.sequence & 0xffffffffffffffffn).toString(16).padStart(16, '0');
  }

  /** Append one raw action document, as the Go fixtures record them. */
  addDocument(document: Record<string, unknown>): void {
    const moment = parseGoTime(document.date as string)!;
    this.actions.push({ document, actionId: document.id as string, actionType: document.type as string, occurredNanoseconds: moment.unixNanoseconds });
  }

  /** Add a card to the To do list and record createCard; returns the card ID. */
  createCard(name: string): string {
    const now = Date.now();
    const id = this.newObjectId(now / 1000);
    const number = this.cards.size + 1;
    const card: StoredCard = { id, shortLink: `Sh${String(number).padStart(6, '0')}`, idShort: number, name, desc: '', idBoard: this.boardId, idList: TodoListID, closed: false, pos: 16384 };
    this.cards.set(id, card);
    this.record('createCard', now, card, { list: list(TodoListID) });
    return id;
  }

  /** Move a card to Done and record updateCard with listBefore and listAfter. */
  moveCard(cardId: string): void {
    const card = this.cards.get(cardId)!;
    const before = card.idList;
    card.idList = DoneListID;
    this.record('updateCard', Date.now(), card, { listBefore: list(before), listAfter: list(DoneListID), old: { idList: before } });
  }

  /** Rename a card and record updateCard with the old name. */
  renameCard(cardId: string, name: string): void {
    const card = this.cards.get(cardId)!;
    const old = card.name;
    card.name = name;
    this.record('updateCard', Date.now(), card, { old: { name: old } });
  }

  /** Record commentCard with text. */
  commentOnCard(cardId: string, text: string): void { this.record('commentCard', Date.now(), this.cards.get(cardId)!, { text }); }

  /** Remove a card and record deleteCard, which keeps only its ID, short ID, and list. */
  deleteCard(cardId: string): void {
    const card = this.cards.get(cardId)!;
    this.cards.delete(cardId);
    this.record('deleteCard', Date.now(), { id: card.id, idShort: card.idShort, idList: card.idList, name: '' }, {});
  }

  /** Answer the next count action requests with 429 API_TOKEN_LIMIT_EXCEEDED. */ rateLimitNextActionRequests(count: number): void { this.pendingRateLimits = count; }
  /** Answer the next action request that sends since with 400 invalid value for since. */ rejectNextCursor(): void { this.shouldRejectNextCursor = true; }
  /** Return the ID of the newest recorded action. */ lastActionId(): string { return this.actions[this.actions.length - 1]!.actionId; }
  /** Return the requests answered so far. */ requests(): RecordedRequest[] { return [...this.recorded]; }

  /** Serve one provider request. */
  readonly send = async (request: ProviderRequest): Promise<ProviderResponse> => {
    const url = new URL(request.url);
    const query = Object.fromEntries(url.searchParams.entries());
    const response = this.respond(request, url.pathname, query);
    this.recorded.push({ path: url.pathname, query, status: response.status });
    return response;
  };

  private respond(request: ProviderRequest, path: string, query: Record<string, string>): ProviderResponse {
    if (request.headers?.Authorization !== `OAuth oauth_consumer_key="${APIKey}", oauth_token="${Token}"`) return text(401, 'invalid key');
    const segments = path.replace(/^\/1\//, '').split('/');
    if (request.method === 'GET' && segments.length === 3 && segments[0] === 'boards' && segments[2] === 'actions') return this.serveActions(segments[1]!, query);
    if (request.method === 'GET' && segments.length === 2 && segments[0] === 'cards') {
      const card = this.cards.get(segments[1]!);
      return card ? json(200, card) : text(404, 'The requested resource was not found.');
    }
    return text(404, 'Not Found');
  }

  private serveActions(boardId: string, query: Record<string, string>): ProviderResponse {
    if (boardId !== this.boardId) return text(404, 'The requested resource was not found.');
    if (this.pendingRateLimits > 0) { this.pendingRateLimits--; return json(429, { error: 'API_TOKEN_LIMIT_EXCEEDED', message: 'Rate limit exceeded' }); }
    if (this.shouldRejectNextCursor && 'since' in query) { this.shouldRejectNextCursor = false; return text(400, 'invalid value for since'); }
    const filters = (query.filter ?? '').split(',');
    const since = query.since !== undefined ? parseGoTime(query.since) : undefined;
    const before = query.before;
    const listed = this.actions.filter(action => filters.includes(action.actionType) &&
      (!since || action.occurredNanoseconds > since.unixNanoseconds) && (before === undefined || action.actionId < before));
    listed.sort((left, right) => left.occurredNanoseconds === right.occurredNanoseconds
      ? (right.actionId < left.actionId ? -1 : right.actionId > left.actionId ? 1 : 0)
      : (right.occurredNanoseconds > left.occurredNanoseconds ? 1 : -1));
    return json(200, listed.slice(0, Number(query.limit)).map(action => action.document));
  }

  private record(actionType: string, milliseconds: number, card: Partial<StoredCard>, extra: Record<string, unknown>): void {
    const cardData: Record<string, unknown> = {};
    for (const key of ['id', 'name', 'idShort', 'shortLink', 'idList']) if (card[key] !== undefined && card[key] !== '') cardData[key] = card[key];
    this.addDocument({
      id: this.newObjectId(milliseconds / 1000), idMemberCreator: MemberID, type: actionType, date: new Date(milliseconds).toISOString(),
      data: { board: { id: this.boardId, name: 'Support queue', shortLink: 'Supp0rtQ' }, card: cardData, ...extra },
      memberCreator: { id: MemberID, fullName: 'Grace Hopper', username: 'gracehopper' },
    });
  }
}

function list(listId: string): { id: string; name: string } { return { id: listId, name: listNames[listId]! }; }
function json(status: number, value: unknown): ProviderResponse { return { status, headers: { 'content-type': 'application/json' }, body: Buffer.from(JSON.stringify(value)) }; }
function text(status: number, value: string): ProviderResponse { return { status, headers: {}, body: Buffer.from(value) }; }
