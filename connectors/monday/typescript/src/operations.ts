// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import emailAddresses from 'email-addresses';
import { AddUpdateDefinition, CreateItemDefinition, GetItemDefinition, ListItemsDefinition, UpdateItemColumnValuesDefinition } from './generated/definitions.js';
import type { AddUpdateInput, AddUpdateOutput, ColumnValue, CreateItemInput, CreateItemOutput, GetItemInput, Item, ItemColumnValue, ItemFilter, ItemFilterRule, ItemOrder, ListItemsInput, ListItemsOutput, UpdateItemColumnValuesInput, UpdateItemColumnValuesOutput } from './generated/models.js';
import type { MondayClient, MondayExchange, MondayRequest } from './client.js';

const itemFields = 'id name state created_at updated_at url creator_id board { id } group { id title } column_values(ids: $columnIds) { id type text value }';
const createdItemFields = 'id name state created_at updated_at url creator_id board { id } group { id title }';
const listDocument = `query ListBoardItems($boardIds: [ID!], $limit: Int!, $queryParams: ItemsQuery, $columnIds: [String!]) {\n  boards(ids: $boardIds) { id items_page(limit: $limit, query_params: $queryParams) { cursor items { ${itemFields} } } }\n}`;
const nextDocument = `query ListNextBoardItems($cursor: String!, $limit: Int!, $columnIds: [String!]) {\n  next_items_page(cursor: $cursor, limit: $limit) { cursor items { ${itemFields} } }\n}`;
const getDocument = `query GetItem($itemIds: [ID!], $excludeNonactive: Boolean, $columnIds: [String!]) {\n  items(ids: $itemIds, exclude_nonactive: $excludeNonactive) { ${itemFields} }\n}`;
const createDocument = `mutation CreateItem($boardId: ID!, $groupId: String, $itemName: String!, $columnValues: JSON, $createLabelsIfMissing: Boolean) {\n  create_item(board_id: $boardId, group_id: $groupId, item_name: $itemName, column_values: $columnValues, create_labels_if_missing: $createLabelsIfMissing) { ${createdItemFields} }\n}`;
const changeDocument = `mutation UpdateItemColumnValues($boardId: ID!, $itemId: ID, $columnValues: JSON!, $createLabelsIfMissing: Boolean, $columnIds: [String!]) {\n  change_multiple_column_values(board_id: $boardId, item_id: $itemId, column_values: $columnValues, create_labels_if_missing: $createLabelsIfMissing) { ${itemFields} }\n}`;
const addDocument = `mutation AddUpdate($itemId: ID, $body: String!) {\n  create_update(item_id: $itemId, body: $body) { id item_id created_at creator_id }\n}`;
const zeroItem: Item = { id: '', name: '' };

/** Query a bounded page of board items. */
export class ListItemsOperation implements Query<ListItemsInput, ListItemsOutput> {
  /** Bind the client. */ constructor(private readonly client: MondayClient) {}
  /** Return the manifest definition. */ definition() { return ListItemsDefinition; }
  /** Validate, send, and decode one page. */
  async invoke(call: Call, input: ListItemsInput): Promise<QueryAttempt<ListItemsOutput>> {
    const output: ListItemsOutput = { boardId: input.boardId, items: [] };
    let request: MondayRequest;
    try {
      const boardID = numericID(input.boardId, 'boardId'); output.boardId = boardID;
      const limit = input.limit || 25;
      if (!Number.isInteger(limit) || limit < 1 || limit > 100) throw new Error('limit must be from 1 to 100');
      const columnIDs = validateColumnIDs(input.columnIds);
      if (input.cursor) {
        if (input.filter && ((input.filter.rules?.length ?? 0) > 0 || input.filter.combination) || input.order) throw new Error("a cursor continues its first page's filter and order, so set neither with it");
        if (Buffer.byteLength(input.cursor) > 4096 || !/^[!-~]+$/.test(input.cursor)) throw new Error('cursor must be the nextCursor value of a previous page');
        request = { document: nextDocument, variables: { cursor: input.cursor, limit, columnIds: columnIDs } };
      } else {
        const queryParams = encodeItemsQuery(input.filter, input.order);
        request = { document: listDocument, variables: { boardIds: [boardID], limit, columnIds: columnIDs, ...(queryParams ? { queryParams } : {}) } };
      }
    } catch (error) { return queryDefect('listItems', output, error); }
    const exchange = await this.client.exchange(call, 'listItems', request);
    const receipt = this.client.receipt(call, exchange, output.boardId);
    if (exchange.outcome !== 'succeeded') return queryExchange(exchange, output, receipt);
    try {
      const data = object(exchange.data);
      let page: Record<string, unknown>;
      if (input.cursor) page = object(data.next_items_page);
      else {
        const boards = data.boards === undefined || data.boards === null ? [] : array(data.boards);
        if (boards.length === 0 || boards[0] === null) return queryFailure('listItems', 'notFound', 'NOT_FOUND', output, receipt, 'monday.com found no board with this ID that the connection can see');
        if (boards.length !== 1) throw new Error('expected one board with an items_page');
        page = object(object(boards[0]).items_page);
      }
      const values = page.items === undefined || page.items === null ? [] : array(page.items);
      if (values.length > 100) throw new Error('page holds more than 100 items');
      const cursor = page.cursor === null || page.cursor === undefined ? '' : string(page.cursor);
      if (cursor && (Buffer.byteLength(cursor) > 4096 || !/^[!-~]+$/.test(cursor))) throw new Error('cursor is not a printable token');
      output.items = values.map(decodeItem);
      if (cursor) output.nextCursor = cursor;
      return newQueryBranch('listed', output, receipt);
    } catch (error) { return queryFailure('listItems', 'invalidResponse', 'PROTOCOL', output, receipt, `monday.com returned an invalid page of items: ${message(error)}`); }
  }
}

/** Query one board item. */
export class GetItemOperation implements Query<GetItemInput, Item> {
  /** Bind the client. */ constructor(private readonly client: MondayClient) {}
  /** Return the manifest definition. */ definition() { return GetItemDefinition; }
  /** Validate, send, and decode one item. */
  async invoke(call: Call, input: GetItemInput): Promise<QueryAttempt<Item>> {
    let itemID: string;
    let columnIDs: string[] | null;
    try { itemID = numericID(input.itemId, 'itemId'); columnIDs = validateColumnIDs(input.columnIds); }
    catch (error) { return queryDefect('getItem', zeroItem, error); }
    const exchange = await this.client.exchange(call, 'getItem', { document: getDocument, variables: { itemIds: [itemID], excludeNonactive: !input.includesInactive, columnIds: columnIDs } });
    const receipt = this.client.receipt(call, exchange, itemID);
    if (exchange.outcome !== 'succeeded') return queryExchange(exchange, zeroItem, receipt);
    let values: unknown[];
    try { const items = object(exchange.data).items; values = items === undefined || items === null ? [] : array(items); if (values.length > 1) throw new Error('invalid item list'); }
    catch { return queryFailure('getItem', 'invalidResponse', 'PROTOCOL', zeroItem, receipt, 'monday.com returned an invalid item list'); }
    if (values.length === 0 || values[0] === null) return queryFailure('getItem', 'notFound', 'NOT_FOUND', zeroItem, receipt, 'monday.com found no item with this ID that the connection can see');
    try { const item = decodeItem(values[0]); if (item.id !== itemID) throw new Error('item ID differs'); return newQueryBranch('found', item, receipt); }
    catch { return queryFailure('getItem', 'invalidResponse', 'PROTOCOL', zeroItem, receipt, 'monday.com returned an invalid item'); }
  }
}

/** Create one item with an attempt checkpoint and a stable idempotency key. */
export class CreateItemOperation implements Mutation<CreateItemInput, CreateItemOutput> {
  /** Bind the client. */ constructor(private readonly client: MondayClient) {}
  /** Return the manifest definition. */ definition() { return CreateItemDefinition; }
  /** Reuse the Dex Call ID. */ idempotencyKey(callID: string): string { return callID; }
  /** Validate, send, and decode one create attempt. */
  async invoke(call: Call, input: CreateItemInput): Promise<MutationAttempt<CreateItemOutput>> {
    const output: CreateItemOutput = { boardId: input.boardId.trim(), itemName: input.itemName, ...(input.groupId?.trim() ? { groupId: input.groupId.trim() } : {}) };
    let request: MondayRequest;
    try {
      const boardID = numericID(input.boardId, 'boardId'); output.boardId = boardID;
      if (output.groupId && !columnPattern.test(output.groupId)) throw new Error('groupId must be a monday.com group ID such as topics');
      const itemName = validateItemName(input.itemName); output.itemName = itemName;
      request = { document: createDocument, variables: { boardId: boardID, itemName, createLabelsIfMissing: !!input.createsLabelsIfMissing,
        ...(output.groupId ? { groupId: output.groupId } : {}), ...(input.columnValues && Object.keys(input.columnValues).length ? { columnValues: encodeColumnValues(input.columnValues) } : {}) }, isMutation: true, requiresCheckpoint: true };
    } catch (error) { return mutationDefect('createItem', output, error); }
    const exchange = await this.client.exchange(call, 'createItem', request);
    const receipt = this.client.receipt(call, exchange, '');
    const item = decodeMutationItem(exchange.data, 'create_item');
    if (item) return newMutationBranch('created', { ...output, itemId: item.id, item, ...(exchange.isReplayed ? { replayed: true } : {}) }, { ...receipt, providerObjectId: item.id });
    if (exchange.outcome !== 'succeeded') return mutationExchange(exchange, output, receipt, false);
    return newMutationUncertain(output, failure('createItem', 'PROTOCOL', 'monday.com accepted the item but returned an unusable item'), receipt);
  }
}

/** Replace typed column values on one item. */
export class UpdateItemColumnValuesOperation implements Mutation<UpdateItemColumnValuesInput, UpdateItemColumnValuesOutput> {
  /** Bind the client. */ constructor(private readonly client: MondayClient) {}
  /** Return the manifest definition. */ definition() { return UpdateItemColumnValuesDefinition; }
  /** Reuse the Dex Call ID. */ idempotencyKey(callID: string): string { return callID; }
  /** Validate, send, and decode one absolute replacement. */
  async invoke(call: Call, input: UpdateItemColumnValuesInput): Promise<MutationAttempt<UpdateItemColumnValuesOutput>> {
    const output: UpdateItemColumnValuesOutput = { boardId: input.boardId, itemId: input.itemId };
    let request: MondayRequest;
    try {
      const boardID = numericID(input.boardId, 'boardId'); const itemID = numericID(input.itemId, 'itemId');
      output.boardId = boardID; output.itemId = itemID;
      if (!input.columnValues || Object.keys(input.columnValues).length === 0) throw new Error('columnValues needs at least one column');
      request = { document: changeDocument, variables: { boardId: boardID, itemId: itemID, columnValues: encodeColumnValues(input.columnValues),
        createLabelsIfMissing: !!input.createsLabelsIfMissing, columnIds: Object.keys(input.columnValues).sort() }, isMutation: true };
    } catch (error) { return mutationDefect('updateItemColumnValues', output, error); }
    const exchange = await this.client.exchange(call, 'updateItemColumnValues', request);
    const receipt = this.client.receipt(call, exchange, output.itemId);
    const item = decodeMutationItem(exchange.data, 'change_multiple_column_values');
    if (item && item.id === output.itemId) return newMutationBranch('updated', { ...output, item, ...(exchange.isReplayed ? { replayed: true } : {}) }, receipt);
    if (exchange.outcome !== 'succeeded') return mutationExchange(exchange, output, receipt, true);
    return mutationFailure('updateItemColumnValues', 'invalidResponse', 'PROTOCOL', output, receipt, 'monday.com returned an unusable item, so the values may have been set');
  }
}

/** Post a plain text update to one item. */
export class AddUpdateOperation implements Mutation<AddUpdateInput, AddUpdateOutput> {
  /** Bind the client. */ constructor(private readonly client: MondayClient) {}
  /** Return the manifest definition. */ definition() { return AddUpdateDefinition; }
  /** Reuse the Dex Call ID. */ idempotencyKey(callID: string): string { return callID; }
  /** Validate, send, and decode one update attempt. */
  async invoke(call: Call, input: AddUpdateInput): Promise<MutationAttempt<AddUpdateOutput>> {
    const output: AddUpdateOutput = { itemId: input.itemId.trim() };
    let request: MondayRequest;
    try { const itemID = numericID(input.itemId, 'itemId'); request = { document: addDocument, variables: { itemId: itemID, body: updateBody(input.body) }, isMutation: true, requiresCheckpoint: true }; }
    catch (error) { return mutationDefect('addUpdate', output, error); }
    const exchange = await this.client.exchange(call, 'addUpdate', request);
    const receipt = this.client.receipt(call, exchange, '');
    const update = decodeCreatedUpdate(exchange.data, output.itemId);
    if (update) return newMutationBranch('added', { ...output, ...update, ...(exchange.isReplayed ? { replayed: true } : {}) }, { ...receipt, providerObjectId: update.updateId });
    if (exchange.outcome !== 'succeeded') return mutationExchange(exchange, output, receipt, false);
    return newMutationUncertain(output, failure('addUpdate', 'PROTOCOL', 'monday.com accepted the update but returned an unusable update'), receipt);
  }
}

function failure(operation: string, kind: Failure['kind'], messageText: string): Failure { return { kind, provider: 'monday', operation, message: messageText }; }
function message(error: unknown): string { return error instanceof Error ? error.message : 'invalid value'; }
function queryDefect<T>(operation: string, output: T, error: unknown): QueryAttempt<T> { return queryFailure(operation, 'defect', 'VALIDATION', output, {}, message(error)); }
function mutationDefect<T>(operation: string, output: T, error: unknown): MutationAttempt<T> { return mutationFailure(operation, 'defect', 'VALIDATION', output, {}, message(error)); }
function queryFailure<T>(operation: string, branch: string, kind: Failure['kind'], output: T, receipt: ReceiptDraft, text: string): QueryAttempt<T> { return newQueryBranch(branch, output, receipt, failure(operation, kind, text)); }
function mutationFailure<T>(operation: string, branch: string, kind: Failure['kind'], output: T, receipt: ReceiptDraft, text: string): MutationAttempt<T> { return newMutationBranch(branch, output, receipt, failure(operation, kind, text)); }
function queryExchange<T>(exchange: MondayExchange, output: T, receipt: ReceiptDraft): QueryAttempt<T> {
  if (exchange.outcome === 'retry') return newQueryRetry(exchange.failure!, exchange.retryAfterMs);
  const branch = exchange.outcome === 'notFound' ? 'notFound' : exchange.outcome === 'invalid' ? 'invalidResponse' : exchange.outcome === 'defect' ? 'defect' : 'providerRejected';
  return newQueryBranch(branch, output, receipt, exchange.failure);
}
function mutationExchange<T>(exchange: MondayExchange, output: T, receipt: ReceiptDraft, repeatSafe: boolean): MutationAttempt<T> {
  if (exchange.outcome === 'retry') return newMutationRetry(exchange.failure!, exchange.retryAfterMs);
  if (exchange.outcome === 'uncertain') return repeatSafe ? newMutationRetry(exchange.failure!) : newMutationUncertain(output, exchange.failure!, receipt);
  if (exchange.outcome === 'invalid' && !repeatSafe) return newMutationUncertain(output, exchange.failure!, receipt);
  const branch = exchange.outcome === 'notFound' ? 'notFound' : exchange.outcome === 'invalid' ? 'invalidResponse' : exchange.outcome === 'defect' ? 'defect' : 'providerRejected';
  return newMutationBranch(branch, output, receipt, exchange.failure);
}

const numericPattern = /^[1-9][0-9]{0,19}$/;
const columnPattern = /^[A-Za-z0-9_-]{1,100}$/;
function numericID(value: string, label: string): string { const result = value.trim(); if (!numericPattern.test(result)) throw new Error(`${label} must be a numeric monday.com ID such as 1234567890`); return result; }
function validateColumnIDs(input: string[] | undefined): string[] | null {
  if (!input?.length) return null;
  if (input.length > 50) throw new Error('columnIds accepts at most 50 column IDs');
  if (input.some(column => !columnPattern.test(column))) throw new Error('columnIds must be monday.com column IDs such as status or date4');
  return [...new Set(input)];
}
function validateItemName(value: string): string { const name = value.trim(); if (!name) throw new Error('itemName is required'); if ([...name].length > 255) throw new Error('itemName cannot exceed 255 characters'); if ([...name].some(character => character.codePointAt(0)! < 32 || character.codePointAt(0) === 127)) throw new Error('itemName must be one line without control characters'); return name; }
function updateBody(value: string): string {
  const trimmed = value.trim(); if (!trimmed) throw new Error('body is required');
  if ([...value].length > 10000) throw new Error('body is at most 10000 characters');
  if ([...value].some(character => { const code = character.codePointAt(0)!; return code === 127 || code < 32 && ![9, 10, 13].includes(code); })) throw new Error('body cannot contain control characters other than line breaks and tabs');
  return trimmed.replace(/\r\n/g, '\n').split('\n').map(line => line.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&#34;').replace(/'/g, '&#39;')).join('<br>');
}
function object(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('expected object'); return value as Record<string, unknown>; }
function array(value: unknown): unknown[] { if (!Array.isArray(value)) throw new Error('expected list'); return value; }
function string(value: unknown): string { if (typeof value !== 'string') throw new Error('expected string'); return value; }
function optionalString(value: unknown): string { if (value === null || value === undefined) return ''; return string(value); }
function decodeMutationItem(data: unknown, field: string): Item | undefined { try { const value = object(data)[field]; return value === null || value === undefined ? undefined : decodeItem(value); } catch { return undefined; } }
function decodeCreatedUpdate(data: unknown, itemID: string): Pick<AddUpdateOutput, 'updateId' | 'creatorId' | 'createdAt'> | undefined {
  try {
    const update = object(object(data).create_update);
    const updateID = string(update.id); if (!numericPattern.test(updateID)) return undefined;
    const returnedItemID = optionalString(update.item_id); if (returnedItemID && returnedItemID !== itemID) return undefined;
    const creatorID = optionalString(update.creator_id);
    const createdAt = mondayTime(optionalString(update.created_at));
    return { updateId: updateID, ...(numericPattern.test(creatorID) ? { creatorId: creatorID } : {}), ...(createdAt ? { createdAt } : {}) };
  } catch { return undefined; }
}
function mondayTime(value: string): string | undefined {
  if (!value) return undefined;
  const iso = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|([+-])(\d{2}):?(\d{2}))$/.exec(value);
  const plain = /^(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2}):(\d{2})(?: (UTC|GMT))?$/.exec(value);
  const match = iso ?? plain;
  if (!match) return undefined;
  const [, yearText, monthText, dayText, hourText, minuteText, secondText] = match;
  const dateText = `${yearText}-${monthText}-${dayText}`;
  try { calendarDate(dateText); } catch { return undefined; }
  const hour = Number(hourText); const minute = Number(minuteText); const second = Number(secondText);
  if (hour > 23 || minute > 59 || second > 59) return undefined;
  const offsetHour = Number(iso?.[10] ?? 0); const offsetMinute = Number(iso?.[11] ?? 0);
  if (offsetHour > 99 || offsetMinute > 59) return undefined;
  const instant = new Date(0);
  instant.setUTCFullYear(Number(yearText), Number(monthText) - 1, Number(dayText));
  instant.setUTCHours(hour, minute, second, 0);
  const offset = (offsetHour * 60 + offsetMinute) * (iso?.[9] === '-' ? -1 : 1);
  instant.setTime(instant.getTime() - offset * 60_000);
  const fraction = (iso?.[7] ?? '').slice(0, 9).replace(/0+$/, '');
  return instant.toISOString().slice(0, 19) + (fraction ? `.${fraction}` : '') + 'Z';
}
function decodeItem(value: unknown): Item {
  const resource = object(value); const id = string(resource.id); if (!numericPattern.test(id)) throw new Error('item ID is not a monday.com numeric ID');
  const name = optionalString(resource.name); const state = optionalString(resource.state);
  if (state && !['active', 'archived', 'deleted'].includes(state)) throw new Error('item state is not a monday.com state');
  const board = resource.board === null || resource.board === undefined ? undefined : object(resource.board);
  const boardID = board ? string(board.id) : ''; if (board && !numericPattern.test(boardID)) throw new Error('item board ID is not a monday.com numeric ID');
  const group = resource.group === null || resource.group === undefined ? undefined : object(resource.group);
  const groupID = group ? string(group.id) : ''; if (group && !columnPattern.test(groupID)) throw new Error('item group ID is not a monday.com group ID');
  const creatorID = optionalString(resource.creator_id); if (creatorID && !numericPattern.test(creatorID)) throw new Error('item creator ID is not a monday.com numeric ID');
  const urlText = optionalString(resource.url); let safeURL = '';
  try { const url = new URL(urlText); if (url.protocol === 'https:' && !url.username && !url.password) safeURL = urlText; } catch { /* Missing provider URL is allowed. */ }
  const createdAt = mondayTime(optionalString(resource.created_at)); const updatedAt = mondayTime(optionalString(resource.updated_at));
  const columnValues = resource.column_values === null || resource.column_values === undefined ? [] : array(resource.column_values).map(decodeColumnValue);
  return { id, name, ...(state ? { state } : {}), ...(boardID ? { boardId: boardID } : {}), ...(group ? { group: { id: groupID, ...(optionalString(group.title) ? { title: string(group.title) } : {}) } } : {}),
    ...(creatorID ? { creatorId: creatorID } : {}), ...(safeURL ? { url: safeURL } : {}), ...(createdAt ? { createdAt } : {}), ...(updatedAt ? { updatedAt } : {}), ...(columnValues.length ? { columnValues } : {}) };
}
function decodeColumnValue(value: unknown): ItemColumnValue {
  const resource = object(value); const id = string(resource.id); const type = string(resource.type);
  if (!columnPattern.test(id) || !/^[a-z][a-z0-9_]{0,63}$/.test(type)) throw new Error('invalid column identity');
  const text = optionalString(resource.text); const encodedText = Buffer.from(text); let display = text; let textTruncated = false;
  if (encodedText.length > 4096) { display = encodedText.subarray(0, 4096).toString(); while (Buffer.byteLength(display) > 4096) display = display.slice(0, -1); textTruncated = true; }
  let raw = resource.value;
  if (typeof raw === 'string') { try { raw = JSON.parse(raw); } catch { throw new Error(`column ${id} value is not JSON`); } }
  const encoded = raw === null || raw === undefined ? '' : JSON.stringify(raw);
  return { id, type, ...(display ? { text: display } : {}), ...(textTruncated ? { textTruncated: true } : {}),
    ...(encoded && Buffer.byteLength(encoded) > 16384 ? { valueOmitted: true } : encoded ? { value: raw } : {}) };
}

const supportedOperators = new Set(['any_of', 'not_any_of', 'is_empty', 'is_not_empty', 'greater_than', 'greater_than_or_equals', 'lower_than', 'lower_than_or_equal', 'between', 'contains_text', 'not_contains_text', 'contains_terms', 'starts_with', 'ends_with', 'within_the_next', 'within_the_last']);
const singleTextOperators = new Set(['contains_text', 'not_contains_text', 'contains_terms', 'starts_with', 'ends_with']);
function encodeItemsQuery(filter: ItemFilter | undefined, order: ItemOrder | undefined): Record<string, unknown> | undefined {
  if (!filter?.rules?.length && !filter?.combination && !order) return undefined;
  const query: Record<string, unknown> = {};
  const rules = filter?.rules ?? []; if (rules.length > 10) throw new Error('filter accepts at most 10 rules');
  if (filter?.combination) { if (!['and', 'or'].includes(filter.combination)) throw new Error('filter combination must be and or or'); if (!rules.length) throw new Error('filter combination needs at least one rule'); query.operator = filter.combination; }
  if (rules.length) query.rules = rules.map(encodeItemFilterRule);
  if (order) { if (!columnPattern.test(order.columnId)) throw new Error('order columnId must be a monday.com column ID or __creation_log__ or __last_updated__'); query.order_by = [{ column_id: order.columnId, direction: order.descending ? 'desc' : 'asc' }]; }
  return query;
}
function encodeItemFilterRule(rule: ItemFilterRule): Record<string, unknown> {
  if (!columnPattern.test(rule.columnId)) throw new Error('columnId must be a monday.com column ID such as status, date4, or name');
  const operator = rule.operator || 'any_of'; if (!supportedOperators.has(operator)) throw new Error('operator must be a monday.com items_page rule operator such as any_of or contains_text');
  const values = rule.values ?? []; const numbers = rule.numbers ?? [];
  if (values.length && numbers.length) throw new Error('set at most one of values and numbers');
  if (values.some(value => !value.trim() || [...value].length > 1000)) throw new Error('compare value is invalid');
  if (numbers.some(number => !Number.isFinite(number))) throw new Error('numbers must be finite');
  const count = values.length + numbers.length;
  let compareValue: string | string[] | number[];
  if (operator === 'is_empty' || operator === 'is_not_empty') { if (count) throw new Error(`${operator} takes no compare value`); compareValue = []; }
  else if (singleTextOperators.has(operator)) { if (values.length !== 1) throw new Error(`${operator} takes exactly one string value`); compareValue = values[0]!; }
  else { if (operator === 'between' && count !== 2) throw new Error('between takes exactly two compare values'); if (!count || count > 50) throw new Error(`${operator} takes 1 to 50 compare values`); compareValue = numbers.length ? numbers : values; }
  if (rule.compareAttribute && !columnPattern.test(rule.compareAttribute)) throw new Error('compareAttribute must be letters, digits, underscores, or hyphens');
  return { column_id: rule.columnId, operator, compare_value: compareValue, ...(rule.compareAttribute ? { compare_attribute: rule.compareAttribute } : {}) };
}
function encodeColumnValues(values: Record<string, ColumnValue>): string {
  const ids = Object.keys(values).sort(); if (ids.length > 50) throw new Error('columnValues holds at most 50 columns');
  const output: Record<string, unknown> = {};
  for (const id of ids) { if (id === 'name') throw new Error('columnValues cannot set the name column; use the item name instead'); if (!columnPattern.test(id)) throw new Error('columnValues keys must be monday.com column IDs such as status or date4'); output[id] = encodeColumnValue(values[id]!); }
  return JSON.stringify(sortedJSON(output));
}
function sortedJSON(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sortedJSON);
  if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).sort(([left], [right]) => left.localeCompare(right)).map(([key, item]) => [key, sortedJSON(item)]));
  return value;
}
const columnFields: Record<string, string[]> = {
  text: ['text'], long_text: ['text'], numbers: ['number'], status: ['statusLabel', 'statusIndex'], date: ['date', 'time'],
  people: ['personIds', 'teamIds'], checkbox: ['checked'], email: ['email', 'text'], link: ['url', 'text'], phone: ['phone', 'countryCode'],
  dropdown: ['dropdownLabels', 'dropdownLabelIds'], timeline: ['date', 'endDate'],
};
function encodeColumnValue(value: ColumnValue): unknown {
  const allowed = columnFields[value.type]; if (!allowed) throw new Error('type must be a supported monday.com column type such as text, status, or date');
  for (const [field, item] of Object.entries(value)) {
    if (field === 'type' || field === 'cleared') continue;
    const present = Array.isArray(item) ? item.length > 0 : item !== undefined && item !== null && item !== '';
    if (present && (value.cleared || !allowed.includes(field))) throw new Error(value.cleared ? `a cleared value cannot set ${field}` : `${field} does not apply to a ${value.type} column`);
  }
  if (value.cleared) return null;
  switch (value.type) {
    case 'text': return columnText(value.text);
    case 'long_text': return { text: columnText(value.text) };
    case 'numbers': if (value.number === undefined || !Number.isFinite(value.number)) throw new Error('a numbers value needs a finite number'); return goFloatDecimal(value.number);
    case 'status': {
      if (!!value.statusLabel === (value.statusIndex !== undefined)) throw new Error('a status value sets exactly one of statusLabel and statusIndex');
      if (value.statusIndex !== undefined) { if (!Number.isSafeInteger(value.statusIndex) || value.statusIndex < 0) throw new Error('statusIndex cannot be negative'); return { index: value.statusIndex }; }
      return { label: columnLabel(value.statusLabel, 'statusLabel') };
    }
    case 'date': return { date: calendarDate(value.date), ...(value.time ? { time: clockTime(value.time) } : {}) };
    case 'people': {
      const people = value.personIds ?? []; const teams = value.teamIds ?? [];
      if (!people.length && !teams.length) throw new Error('a people value lists at least one person or team; use ClearedValue to empty it');
      if (people.length + teams.length > 50 || [...people, ...teams].some(id => !Number.isSafeInteger(id) || id <= 0)) throw new Error('people and team IDs must be positive monday.com IDs');
      return { personsAndTeams: [...people.map(id => ({ id, kind: 'person' })), ...teams.map(id => ({ id, kind: 'team' }))] };
    }
    case 'checkbox': if (value.checked === undefined) throw new Error('a checkbox value needs checked'); return value.checked ? { checked: 'true' } : null;
    case 'email': {
      const email = value.email ?? ''; const address = emailAddresses.parseOneAddress({ input: email, rfc6532: true });
      const literal = address?.type === 'mailbox' && address.domain?.startsWith('[') ? address.domain.slice(1, -1) : undefined;
      if (address?.type !== 'mailbox' || address.name !== null || address.address !== email || !email.includes('@') || literal !== undefined && isIP(literal) !== 4) throw new Error('email must be one bare address such as jane@example.com');
      return { email, text: value.text ? columnLabel(value.text, 'text') : email };
    }
    case 'link': {
      const address = value.url ?? ''; try { const parsed = new URL(address); if (!['http:', 'https:'].includes(parsed.protocol) || address.length > 2000) throw new Error('invalid'); }
      catch { throw new Error('url must be an absolute http or https URL'); }
      return { url: address, text: value.text ? columnLabel(value.text, 'text') : address };
    }
    case 'phone': if (!/^\+?[0-9]{4,20}$/.test(value.phone ?? '') || !/^[A-Z]{2}$/.test(value.countryCode ?? '')) throw new Error('a phone value needs 4 to 20 digits with an optional + and a country code such as US'); return { phone: value.phone, countryShortName: value.countryCode };
    case 'dropdown': {
      const labels = value.dropdownLabels ?? []; const ids = value.dropdownLabelIds ?? [];
      if (!!labels.length === !!ids.length) throw new Error('a dropdown value sets exactly one of dropdownLabels and dropdownLabelIds; use ClearedValue to empty it');
      if (labels.length + ids.length > 50) throw new Error('a dropdown value lists at most 50 labels');
      if (ids.length) { if (ids.some(id => !Number.isSafeInteger(id) || id < 0)) throw new Error('dropdownLabelIds cannot be negative'); return { ids }; }
      return { labels: labels.map(label => columnLabel(label, 'dropdownLabels')) };
    }
    case 'timeline': {
      const start = calendarDate(value.date); const end = calendarDate(value.endDate);
      if (end < start) throw new Error('a timeline ends on or after it starts'); return { from: start, to: end };
    }
    default: throw new Error('type must be a supported monday.com column type such as text, status, or date');
  }
}
function columnText(value: string | undefined): string { if (!value) throw new Error('a text value needs text; use ClearedValue to empty the column'); if ([...value].length > 2000) throw new Error('text is at most 2000 characters'); return value; }
function goFloatDecimal(value: number): string {
  if (Object.is(value, -0)) return '-0';
  const text = String(value);
  if (!/[eE]/.test(text)) return text;
  const [mantissa, exponentText] = text.toLowerCase().split('e') as [string, string];
  const sign = mantissa.startsWith('-') ? '-' : '';
  const digits = mantissa.replace('-', '').replace('.', '');
  const decimalPosition = mantissa.replace('-', '').indexOf('.') < 0 ? digits.length : mantissa.replace('-', '').indexOf('.');
  const position = decimalPosition + Number(exponentText);
  if (position <= 0) return `${sign}0.${'0'.repeat(-position)}${digits}`;
  if (position >= digits.length) return sign + digits + '0'.repeat(position - digits.length);
  return `${sign}${digits.slice(0, position)}.${digits.slice(position)}`;
}
function columnLabel(value: string | undefined, field: string): string { if (!value?.trim()) throw new Error(`${field} cannot be blank`); if ([...value].length > 255 || [...value].some(character => character.codePointAt(0)! < 32 || character.codePointAt(0) === 127)) throw new Error(`${field} must be one line without control characters`); return value; }
function calendarDate(value: string | undefined): string { if (!value || !/^\d{4}-\d{2}-\d{2}$/.test(value)) throw new Error('date must be a YYYY-MM-DD date such as 2026-02-18'); const [year, month, day] = value.split('-').map(Number) as [number, number, number]; const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0); const last = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31][month - 1]; if (last === undefined || day < 1 || day > last) throw new Error('date must be a real calendar date'); return value; }
function clockTime(value: string): string { if (!/^(?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d$/.test(value)) throw new Error('time must be an HH:MM:SS UTC time such as 09:00:00'); return value; }
