// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { isIP } from 'node:net';
import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type Failure, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { newFence } from '@superdurable/connectors-sdk/singledispatch';
import type { NotionClient, NotionExchange } from './client.js';
import { failure } from './client.js';
import { CreatePageDefinition, GetPageDefinition, QueryDatabaseDefinition, SearchDefinition, UpdatePagePropertiesDefinition } from './generated/definitions.js';
import type { CreatePageInput, CreatePageOutput, GetPageInput, GetPageOutput, Page, PageContent, PageProperty, Parent, PropertyValue, QueryDatabaseInput, QueryDatabaseOutput, QueryFilter, QuerySort, SearchInput, SearchMatch, SearchOutput, UpdatePagePropertiesInput, UpdatePagePropertiesOutput } from './generated/models.js';

const emptyParent: Parent = { type: '' };
const emptyPage: Page = { id: '', title: '', parent: emptyParent, inTrash: false, archived: false, properties: {} };
const emptyContent: PageContent = { text: '', blockCount: 0, hasMoreBlocks: false, textTruncated: false, nestedBlockCount: 0, skippedBlockCount: 0 };
const emptySearch: SearchOutput = { matches: [], hasMore: false };
const emptyQuery: QueryDatabaseOutput = { dataSourceId: '', pages: [], hasMore: false, resultLimitReached: false };
const emptyGet: GetPageOutput = { page: emptyPage, content: emptyContent };
const emptyUpdate: UpdatePagePropertiesOutput = { page: emptyPage };
const emptyCreate: CreatePageOutput = {};

/** Search shared pages and data sources by title. */
export class SearchOperation implements Query<SearchInput, SearchOutput> {
  /** Bind one Notion client. */ constructor(private readonly client: NotionClient) {}
  /** Return the manifest definition. */ definition() { return SearchDefinition; }
  /** Validate and search one bounded page. */
  async invoke(call: Call, input: SearchInput): Promise<QueryAttempt<SearchOutput>> {
    let request: Record<string, unknown>;
    try {
      const query = (input.query ?? '').trim();
      if ([...query].length > 1000) throw new Error('query is too long');
      if (input.objectType && input.objectType !== 'page' && input.objectType !== 'data_source') throw new Error('invalid objectType');
      if (input.sortDirection && input.sortDirection !== 'ascending' && input.sortDirection !== 'descending') throw new Error('invalid sortDirection');
      const pageSize = input.pageSize || 20;
      if (!Number.isInteger(pageSize) || pageSize < 1 || pageSize > 100) throw new Error('invalid pageSize');
      const cursor = validateCursor(input.startCursor ?? '');
      request = { ...(query ? { query } : {}), ...(input.objectType ? { filter: { property: 'object', value: input.objectType } } : {}), ...(input.sortDirection ? { sort: { timestamp: 'last_edited_time', direction: input.sortDirection } } : {}), ...(cursor ? { start_cursor: cursor } : {}), page_size: pageSize };
    } catch { return queryBranch('search', 'defect', emptySearch, failure('search', 'VALIDATION', 'invalid search input')); }
    const result = await this.client.exchange(call, 'search', 'search', 'POST', '/search', request);
    if (!('response' in result)) return readAttempt('search', result, emptySearch, 'providerRejected');
    try {
      const body = objectJSON(result.response.body);
      const results = arrayOrEmpty(body.results);
      if (results.length > Number(request.page_size)) throw new Error('too many results');
      const nextCursor = validateCursor(text(body.next_cursor));
      const matches: SearchMatch[] = [];
      for (const value of results) {
        const resource = object(value);
        if (resource.object !== 'page' && resource.object !== 'data_source') continue;
        const id = parseID(string(resource.id));
        const title = resource.object === 'page' ? titleOfProperties(decodeProperties(resource.properties)) : joinRichText(resource.title);
        matches.push({ objectType: resource.object, id, title, parent: decodeParent(resource.parent), inTrash: resource.in_trash === true,
          ...(text(resource.url) ? { url: text(resource.url) } : {}), ...(text(resource.last_edited_time) ? { lastEditedTime: text(resource.last_edited_time) } : {}) });
      }
      const output: SearchOutput = { matches, hasMore: body.has_more === true, ...(body.has_more === true && nextCursor ? { nextCursor } : {}) };
      return queryBranch('search', matches.length === 0 && !request.start_cursor ? 'noMatch' : 'searched', output, matches.length === 0 && !request.start_cursor ? failure('search', 'NOT_FOUND', 'no title matched') : undefined, result.receipt);
    } catch { return queryBranch('search', 'invalidResponse', emptySearch, failure('search', 'PROTOCOL', 'Notion returned an invalid search page'), result.receipt); }
  }
}

/** Query one bounded page of database rows. */
export class QueryDatabaseOperation implements Query<QueryDatabaseInput, QueryDatabaseOutput> {
  /** Bind one Notion client. */ constructor(private readonly client: NotionClient) {}
  /** Return the manifest definition. */ definition() { return QueryDatabaseDefinition; }
  /** Resolve the data source and query rows. */
  async invoke(call: Call, input: QueryDatabaseInput): Promise<QueryAttempt<QueryDatabaseOutput>> {
    let selection: { dataSourceId?: string; databaseId?: string }; let body: Record<string, unknown>; let properties: string[];
    try {
      selection = selectDataSource(input.dataSourceId, input.databaseId);
      const pageSize = input.pageSize || 50;
      if (!Number.isInteger(pageSize) || pageSize < 1 || pageSize > 100) throw new Error('invalid pageSize');
      const cursor = validateCursor(input.startCursor ?? '');
      properties = input.properties ?? [];
      if (properties.length > 100 || properties.some(value => !value.trim() || value.trim() !== value || [...value].length > 256)) throw new Error('invalid properties');
      if ((input.sorts?.length ?? 0) > 10) throw new Error('too many sorts');
      body = { ...(input.filter ? { filter: encodeFilter(input.filter) } : {}), ...(input.sorts?.length ? { sorts: input.sorts.map(encodeSort) } : {}), ...(cursor ? { start_cursor: cursor } : {}), page_size: pageSize, result_type: 'page' };
    } catch { return queryBranch('queryDatabase', 'defect', emptyQuery, failure('queryDatabase', 'VALIDATION', 'invalid query input')); }
    const resolved = await resolveDataSource(this.client, call, 'queryDatabase', selection);
    if (typeof resolved !== 'string') return readAttempt('queryDatabase', resolved, emptyQuery, 'providerRejected', 'notFound');
    const output = { ...emptyQuery, dataSourceId: resolved };
    const suffix = properties.length ? `?${properties.map(property => `filter_properties%5B%5D=${queryEscape(property)}`).join('&')}` : '';
    const result = await this.client.exchange(call, 'queryDatabase', 'database query', 'POST', `/data_sources/${pathEscape(resolved)}/query${suffix}`, body, 'read', undefined, undefined, resolved);
    if (!('response' in result)) return readAttempt('queryDatabase', result, output, 'providerRejected', 'notFound');
    try {
      const page = objectJSON(result.response.body);
      const results = arrayOrEmpty(page.results);
      if (results.length > Number(body.page_size)) throw new Error('too many rows');
      const nextCursor = validateCursor(text(page.next_cursor));
      return queryBranch('queryDatabase', 'queried', { dataSourceId: resolved, pages: results.map(decodePage), hasMore: page.has_more === true,
        resultLimitReached: objectOrEmpty(page.request_status).incomplete_reason === 'query_result_limit_reached', ...(page.has_more === true && nextCursor ? { nextCursor } : {}) }, undefined, result.receipt);
    } catch { return queryBranch('queryDatabase', 'invalidResponse', output, failure('queryDatabase', 'PROTOCOL', 'Notion returned an invalid query page'), result.receipt); }
  }
}

/** Read one page with optional bounded top-level content. */
export class GetPageOperation implements Query<GetPageInput, GetPageOutput> {
  /** Bind one Notion client. */ constructor(private readonly client: NotionClient) {}
  /** Return the manifest definition. */ definition() { return GetPageDefinition; }
  /** Read metadata and optionally render block content. */
  async invoke(call: Call, input: GetPageInput): Promise<QueryAttempt<GetPageOutput>> {
    let pageId: string; let maxBlocks: number; let maxTextCharacters: number;
    try {
      pageId = parseID(input.pageId); maxBlocks = input.maxBlocks || 100; maxTextCharacters = input.maxTextCharacters || 20_000;
      if (!Number.isInteger(maxBlocks) || maxBlocks < 1 || maxBlocks > 500 || !Number.isInteger(maxTextCharacters) || maxTextCharacters < 1 || maxTextCharacters > 100_000) throw new Error('invalid content bounds');
    } catch { return queryBranch('getPage', 'defect', emptyGet, failure('getPage', 'VALIDATION', 'invalid page input')); }
    const result = await this.client.exchange(call, 'getPage', 'page read', 'GET', `/pages/${pathEscape(pageId)}`, undefined, 'read', undefined, undefined, pageId);
    if (!('response' in result)) return readAttempt('getPage', result, emptyGet, 'providerRejected', 'notFound');
    let page: Page;
    try { page = decodePage(objectJSON(result.response.body)); }
    catch { return queryBranch('getPage', 'invalidResponse', emptyGet, failure('getPage', 'PROTOCOL', 'Notion returned an unusable page'), result.receipt); }
    if (input.skipContent) return queryBranch('getPage', 'found', { page, content: emptyContent }, undefined, result.receipt);
    const content = { ...emptyContent }; const lines: string[] = []; let cursor = ''; let listNumber = 0;
    for (;;) {
      const query = `page_size=${Math.min(100, maxBlocks - content.blockCount)}${cursor ? `&start_cursor=${queryEscape(cursor)}` : ''}`;
      const blocks = await this.client.exchange(call, 'getPage', 'page content read', 'GET', `/blocks/${pathEscape(page.id)}/children?${query}`, undefined, 'read', undefined, undefined, pageId);
      if (!('response' in blocks)) return readAttempt('getPage', blocks, emptyGet, 'providerRejected', 'notFound');
      let blockList: Record<string, unknown>;
      try { blockList = objectJSON(blocks.response.body); if (array(blockList.results).length > Math.min(100, maxBlocks - content.blockCount)) throw new Error('too many blocks'); }
      catch { return queryBranch('getPage', 'invalidResponse', emptyGet, failure('getPage', 'PROTOCOL', 'Notion returned an invalid block list'), result.receipt); }
      for (const value of array(blockList.results)) {
        const block = object(value); content.blockCount++;
        if (block.has_children === true) content.nestedBlockCount++;
        if (block.type !== 'numbered_list_item') listNumber = 0;
        const rendered = renderBlock(block, listNumber);
        if (block.type === 'numbered_list_item') listNumber++;
        if (rendered === null) { content.skippedBlockCount++; continue; }
        if (content.textTruncated) continue;
        const remaining = maxTextCharacters - [...lines.join('\n')].length - (lines.length ? 1 : 0);
        if ([...rendered].length > remaining) { content.textTruncated = true; if (remaining > 0) lines.push([...rendered].slice(0, remaining).join('')); }
        else lines.push(rendered);
      }
      const next = text(blockList.next_cursor);
      if (blockList.has_more !== true || !next) break;
      if (content.blockCount >= maxBlocks) { content.hasMoreBlocks = true; break; }
      try { cursor = validateCursor(next); }
      catch { return queryBranch('getPage', 'invalidResponse', emptyGet, failure('getPage', 'PROTOCOL', 'invalid block cursor'), result.receipt); }
    }
    content.text = lines.join('\n');
    return queryBranch('getPage', 'found', { page, content }, undefined, result.receipt);
  }
}

/** Create a page through the durable single-dispatch fence. */
export class CreatePageOperation implements Mutation<CreatePageInput, CreatePageOutput> {
  /** Bind one Notion client. */ constructor(private readonly client: NotionClient) {}
  /** Return the manifest definition. */ definition() { return CreatePageDefinition; }
  /** Record the stable call identity without sending it to Notion. */ idempotencyKey(callId: string): string { return callId; }
  /** Never repeat a write that may have reached Notion. */
  async invoke(call: Call, input: CreatePageInput): Promise<MutationAttempt<CreatePageOutput>> {
    let fence: ReturnType<typeof newFence>;
    try { fence = newFence(call, 120_000); }
    catch { return mutationBranch('createPage', 'defect', emptyCreate, failure('createPage', 'LOCAL_DEFECT', 'page dispatch could not be initialized')); }
    const decision = fence.decide();
    if (decision.kind === 'failClosed') return newMutationUncertain(emptyCreate, failure('createPage', 'PROTOCOL', 'page dispatch checkpoint cannot be trusted'), {});
    if (decision.kind === 'readBackRequired') {
      const pageId = decision.marker?.providerId;
      if (!pageId) return newMutationUncertain(emptyCreate, failure('createPage', 'PROTOCOL', 'page may have been sent without a confirmable ID'), {});
      return this.confirmPage(call, pageId, {}, false);
    }
    let selection: { dataSourceId?: string; databaseId?: string }; let properties: Record<string, unknown>; let children: unknown[];
    try { selection = selectDataSource(input.dataSourceId, input.databaseId); properties = encodeProperties(input.properties ?? {}); children = encodeParagraphs(input.bodyText ?? ''); }
    catch { return mutationBranch('createPage', 'defect', emptyCreate, failure('createPage', 'VALIDATION', 'invalid page input')); }
    const credentials = await this.client.resolveToken(call, 'createPage');
    if ('outcome' in credentials) {
      try { await fence.recordNotSent(); }
      catch { return newMutationRetry(failure('createPage', 'AVAILABILITY', 'credential checkpoint unavailable')); }
      return writeFromLookup(credentials.outcome);
    }
    const resolved = await resolveDataSource(this.client, call, 'createPage', selection, credentials.token);
    if (typeof resolved !== 'string') {
      if (resolved.disposition === 'retry') {
        try { await fence.recordNotSent(); }
        catch { return newMutationRetry(failure('createPage', 'AVAILABILITY', 'lookup checkpoint unavailable')); }
      }
      return writeFromLookup(resolved);
    }
    const output: CreatePageOutput = { dataSourceId: resolved };
    const body = { parent: { type: 'data_source_id', data_source_id: resolved }, properties, ...(children.length ? { children } : {}) };
    let result: NotionExchange;
    try { result = await fence.dispatch(signal => this.client.exchange(call, 'createPage', 'page creation', 'POST', '/pages', body, 'unkeyedWrite', signal, credentials.token)); }
    catch { return newMutationRetry(failure('createPage', 'TRANSPORT', 'page dispatch checkpoint unavailable')); }
    if (!('response' in result)) {
      if (result.disposition === 'retry') {
        try { if (result.isProvablyNotSent) await fence.recordCanResend(); else await fence.recordNotSent(); }
        catch { return newMutationRetry(failure('createPage', 'AVAILABILITY', 'retry checkpoint unavailable')); }
        return newMutationRetry(result.failure, result.retryAfterMs);
      }
      if (result.disposition === 'committed' && result.providerId) {
        try { await fence.recordProviderId(result.providerId); } catch { /* Read-back remains safe with the accepted ID. */ }
        return this.confirmPage(call, result.providerId, result.receipt, true, resolved);
      }
      if (result.disposition === 'notFound') return mutationBranch('createPage', 'notFound', output, result.failure, result.receipt);
      if (result.disposition === 'providerRejected') return mutationBranch('createPage', 'providerRejected', output, result.failure, result.receipt);
      if (result.disposition === 'defect') return mutationBranch('createPage', 'defect', output, result.failure, result.receipt);
      return newMutationUncertain(output, result.failure, result.receipt);
    }
    let page: Page;
    try { page = decodePage(objectJSON(result.response.body)); }
    catch {
      try { const body = objectJSON(result.response.body); if (body.object === 'page' && typeof body.id === 'string') { const pageId = parseID(body.id); try { await fence.recordProviderId(pageId); } catch { /* Accepted ID still permits read-back. */ } return this.confirmPage(call, pageId, result.receipt, false, resolved); } }
      catch { /* No accepted ID can be confirmed. */ }
      return newMutationUncertain(output, failure('createPage', 'PROTOCOL', 'Notion accepted the page but returned an unusable response'), result.receipt);
    }
    try { await fence.recordProviderId(page.id); } catch { /* Complete response confirms the page. */ }
    return mutationBranch('createPage', 'created', { dataSourceId: resolved, pageId: page.id, page }, undefined, { ...result.receipt, providerObjectId: page.id });
  }

  private async confirmPage(call: Call, pageId: string, receipt: ReceiptDraft, confirmedAfterTimeout: boolean, dataSourceId = ''): Promise<MutationAttempt<CreatePageOutput>> {
    const output: CreatePageOutput = { ...(dataSourceId ? { dataSourceId } : {}), pageId, ...(confirmedAfterTimeout ? { confirmedAfterTimeout: true } : {}) };
    const result = await this.client.exchange(call, 'createPage', 'page creation', 'GET', `/pages/${pathEscape(pageId)}`);
    if (!('response' in result)) return result.disposition === 'retry' ? newMutationRetry(result.failure, result.retryAfterMs) : newMutationUncertain(output, failure('createPage', 'PROTOCOL', 'accepted page could not be confirmed'), { ...receipt, providerObjectId: pageId });
    try {
      const page = decodePage(objectJSON(result.response.body));
      if (page.id !== pageId) throw new Error('page ID mismatch');
      return mutationBranch('createPage', 'created', { ...output, dataSourceId: page.parent.type === 'data_source_id' ? page.parent.id : dataSourceId, page }, undefined, { ...receipt, providerObjectId: pageId });
    } catch { return newMutationUncertain(output, failure('createPage', 'PROTOCOL', 'accepted page read-back did not match'), { ...receipt, providerObjectId: pageId }); }
  }
}

/** Replace named page properties with absolute values. */
export class UpdatePagePropertiesOperation implements Mutation<UpdatePagePropertiesInput, UpdatePagePropertiesOutput> {
  /** Bind one Notion client. */ constructor(private readonly client: NotionClient) {}
  /** Return the manifest definition. */ definition() { return UpdatePagePropertiesDefinition; }
  /** Repeating absolute values converges on the same page state. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate and send one repeat-safe update. */
  async invoke(call: Call, input: UpdatePagePropertiesInput): Promise<MutationAttempt<UpdatePagePropertiesOutput>> {
    let pageId: string; let properties: Record<string, unknown>;
    try { pageId = parseID(input.pageId); if (!input.properties || Object.keys(input.properties).length === 0) throw new Error('properties required'); properties = encodeProperties(input.properties); }
    catch { return mutationBranch('updatePageProperties', 'defect', emptyUpdate, failure('updatePageProperties', 'VALIDATION', 'invalid page update')); }
    const result = await this.client.exchange(call, 'updatePageProperties', 'page update', 'PATCH', `/pages/${pathEscape(pageId)}`, { properties }, 'repeatableWrite', undefined, undefined, pageId);
    if (!('response' in result)) return writeAttempt('updatePageProperties', result, emptyUpdate, 'providerRejected', 'notFound');
    try {
      const page = decodePage(objectJSON(result.response.body));
      if (page.id !== pageId) throw new Error('page ID mismatch');
      return mutationBranch('updatePageProperties', 'updated', { page }, undefined, result.receipt);
    } catch { return mutationBranch('updatePageProperties', 'invalidResponse', emptyUpdate, failure('updatePageProperties', 'PROTOCOL', 'Notion returned an unusable page after update'), result.receipt); }
  }
}

function queryBranch<T>(operation: string, branch: string, value: T, issue?: Failure, receipt: ReceiptDraft = {}): QueryAttempt<T> { void operation; return newQueryBranch(branch, value, receipt, issue); }
function mutationBranch<T>(operation: string, branch: string, value: T, issue?: Failure, receipt: ReceiptDraft = {}): MutationAttempt<T> { void operation; return newMutationBranch(branch, value, receipt, issue); }
function readAttempt<T>(operation: string, result: Exclude<NotionExchange, { response: unknown }>, zero: T, rejected: string, notFound = rejected): QueryAttempt<T> {
  if (result.disposition === 'retry') return newQueryRetry(result.failure, result.retryAfterMs);
  return queryBranch(operation, result.disposition === 'notFound' ? notFound : result.disposition === 'providerRejected' ? rejected : result.disposition === 'defect' ? 'defect' : 'invalidResponse', zero, result.failure, result.receipt);
}
function writeAttempt<T>(operation: string, result: Exclude<NotionExchange, { response: unknown }>, zero: T, rejected: string, notFound = rejected): MutationAttempt<T> {
  if (result.disposition === 'retry') return newMutationRetry(result.failure, result.retryAfterMs);
  return mutationBranch(operation, result.disposition === 'notFound' ? notFound : result.disposition === 'providerRejected' ? rejected : result.disposition === 'defect' ? 'defect' : 'invalidResponse', zero, result.failure, result.receipt);
}
function writeFromLookup(result: Exclude<NotionExchange, { response: unknown }>): MutationAttempt<CreatePageOutput> { return writeAttempt('createPage', result, emptyCreate, 'providerRejected', 'notFound'); }

function object(value: unknown): Record<string, unknown> { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('expected object'); return value as Record<string, unknown>; }
function objectOrEmpty(value: unknown): Record<string, unknown> { try { return object(value); } catch { return {}; } }
function array(value: unknown): unknown[] { if (!Array.isArray(value)) throw new Error('expected array'); return value; }
function arrayOrEmpty(value: unknown): unknown[] { return value == null ? [] : array(value); }
function text(value: unknown): string { return typeof value === 'string' ? value : ''; }
function string(value: unknown): string { if (typeof value !== 'string') throw new Error('expected string'); return value; }
function objectJSON(bytes: Uint8Array): Record<string, unknown> { return object(JSON.parse(Buffer.from(bytes).toString('utf8')) as unknown); }
function queryEscape(value: string): string { return encodeURIComponent(value).replace(/[!'()*]/g, character => `%${character.charCodeAt(0).toString(16).toUpperCase()}`).replaceAll('%20', '+'); }
function pathEscape(value: string): string { return queryEscape(value).replaceAll('+', '%20').replace(/%(24|26|2B|3D|3A|40)/gi, encoded => String.fromCharCode(Number.parseInt(encoded.slice(1), 16))); }
function parseID(value: string): string {
  const candidate = value.trim();
  if (!candidate || Buffer.byteLength(candidate) > 2048) throw new Error('invalid Notion ID');
  let id = candidate;
  if (/^https:\/\//i.test(candidate)) {
    const url = new URL(candidate);
    if (!/(^|\.)notion\.(so|com|site)$/i.test(url.hostname)) throw new Error('invalid Notion URL');
    const segment = url.pathname.split('/').filter(Boolean).at(-1) ?? '';
    id = /^[0-9a-f-]{36}$/i.test(segment) ? segment : segment.replaceAll('-', '').match(/([0-9a-f]{32})$/i)?.[1] ?? '';
  }
  const compact = id.replaceAll('-', '').toLowerCase();
  if (!/^[0-9a-f]{32}$/.test(compact) || (id.includes('-') && !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(id))) throw new Error('invalid Notion ID');
  return `${compact.slice(0, 8)}-${compact.slice(8, 12)}-${compact.slice(12, 16)}-${compact.slice(16, 20)}-${compact.slice(20)}`;
}
function canonicalOrRaw(value: unknown): string { try { return parseID(string(value)); } catch { return text(value); } }
function validateCursor(value: string): string { const cursor = value.trim(); if (Buffer.byteLength(cursor) > 1024 || (cursor && !/^[!-~]+$/.test(cursor))) throw new Error('invalid cursor'); return cursor; }
function selectDataSource(dataSourceId?: string, databaseId?: string): { dataSourceId?: string; databaseId?: string } {
  if (Boolean(dataSourceId?.trim()) === Boolean(databaseId?.trim())) throw new Error('select one data source');
  return dataSourceId?.trim() ? { dataSourceId: parseID(dataSourceId) } : { databaseId: parseID(databaseId!) };
}
async function resolveDataSource(client: NotionClient, call: Call, operation: string, selection: { dataSourceId?: string; databaseId?: string }, resolvedToken?: string): Promise<string | Exclude<NotionExchange, { response: unknown }>> {
  if (selection.dataSourceId) return selection.dataSourceId;
  const result = await client.exchange(call, operation, 'database lookup', 'GET', `/databases/${pathEscape(selection.databaseId!)}`, undefined, 'read', undefined, resolvedToken, selection.databaseId!);
  if (!('response' in result)) return result;
  try {
    const body = objectJSON(result.response.body); if (body.object !== 'database') throw new Error('invalid database');
    const sources = array(body.data_sources).map(value => parseID(string(object(value).id)));
    if (sources.length > 100) throw new Error('too many data sources');
    if (sources.length !== 1) return { disposition: 'defect', failure: failure(operation, 'VALIDATION', 'database must contain one data source'), receipt: result.receipt };
    return sources[0]!;
  } catch { return { disposition: 'invalidResponse', failure: failure(operation, 'PROTOCOL', 'Notion returned an invalid database'), receipt: result.receipt }; }
}
function decodeParent(value: unknown): Parent {
  const resource = objectOrEmpty(value); const type = text(resource.type);
  const id = type === 'data_source_id' ? canonicalOrRaw(resource.data_source_id) : type === 'database_id' ? canonicalOrRaw(resource.database_id) : type === 'page_id' ? canonicalOrRaw(resource.page_id) : type === 'block_id' ? canonicalOrRaw(resource.block_id) : '';
  return { type, ...(id ? { id } : {}), ...(type === 'data_source_id' && canonicalOrRaw(resource.database_id) ? { databaseId: canonicalOrRaw(resource.database_id) } : {}) };
}
function joinRichText(value: unknown): string { return Array.isArray(value) ? value.map(item => text(objectOrEmpty(item).plain_text)).join('') : ''; }
function decodePage(value: unknown): Page {
  const resource = object(value); if (resource.object !== 'page') throw new Error('not a page');
  const id = parseID(string(resource.id)); const properties = decodeProperties(resource.properties);
  return { id, title: titleOfProperties(properties), parent: decodeParent(resource.parent), inTrash: resource.in_trash === true, archived: resource.is_archived === true, properties,
    ...(text(resource.url) ? { url: text(resource.url) } : {}), ...(text(resource.created_time) ? { createdTime: text(resource.created_time) } : {}), ...(text(resource.last_edited_time) ? { lastEditedTime: text(resource.last_edited_time) } : {}) };
}
function decodeProperties(value: unknown): Record<string, PageProperty> {
  if (value === null || value === undefined) return {};
  const source = object(value); const properties: Record<string, PageProperty> = {};
  for (const [name, raw] of Object.entries(source)) properties[name] = decodeProperty(raw);
  return properties;
}
function titleOfProperties(properties: Record<string, PageProperty> | null): string {
  if (!properties) return '';
  return Object.values(properties).find(property => property.type === 'title')?.text ?? '';
}
function decodeProperty(value: unknown, canNest = true): PageProperty {
  const resource = objectOrEmpty(value); const type = text(resource.type); const property: PageProperty = { id: text(resource.id), type, plainText: '' };
  if (type === 'title' || type === 'rich_text') { const rendered = joinRichText(resource[type]); if (rendered) property.text = rendered; property.plainText = rendered; }
  else if (type === 'number' && typeof resource.number === 'number') { property.number = resource.number; property.plainText = decimalNumber(resource.number); }
  else if (type === 'checkbox' && typeof resource.checkbox === 'boolean') { property.checkbox = resource.checkbox; property.plainText = String(resource.checkbox); }
  else if (type === 'select' || type === 'status') { const name = text(objectOrEmpty(resource[type]).name); if (name) property.option = name; property.plainText = name; }
  else if (type === 'multi_select') { const options = Array.isArray(resource.multi_select) ? resource.multi_select.map(item => text(objectOrEmpty(item).name)) : []; if (options.length) property.options = options; property.plainText = options.join(', '); }
  else if (type === 'date') assignDate(property, resource.date);
  else if (type === 'url' || type === 'email') { const content = text(resource[type]); if (content) property[type] = content; property.plainText = content; }
  else if (type === 'phone_number') { const phone = text(resource.phone_number); if (phone) property.phoneNumber = phone; property.plainText = phone; }
  else if (type === 'people') { const people = arrayOrEmpty(resource.people).map(objectOrEmpty); if (people.length) property.userIds = people.map(item => text(item.id)); property.plainText = people.map(item => text(item.name) || text(item.id)).join(', '); }
  else if (type === 'relation') { const ids = arrayOrEmpty(resource.relation).map(item => text(objectOrEmpty(item).id)); if (ids.length) property.pageIds = ids; property.plainText = ids.join(', '); if (resource.has_more === true) property.truncated = true; }
  else if (type === 'formula') assignComputed(property, resource.formula, false);
  else if (type === 'rollup') assignComputed(property, resource.rollup, canNest);
  else if (type === 'created_time' || type === 'last_edited_time') { const start = text(resource[type]); property.date = { start }; property.plainText = start; }
  else if (type === 'created_by' || type === 'last_edited_by') { const user = objectOrEmpty(resource[type]); if (Object.keys(user).length) { property.userIds = [text(user.id)]; property.plainText = text(user.name) || text(user.id); } }
  else if (type === 'unique_id') { const unique = objectOrEmpty(resource.unique_id); if (typeof unique.number === 'number') { property.number = unique.number; property.text = text(unique.prefix) ? `${text(unique.prefix)}-${decimalNumber(unique.number)}` : decimalNumber(unique.number); property.plainText = property.text; } }
  else if (type === 'verification') property.plainText = text(objectOrEmpty(resource.verification).state);
  else if (type === 'files') property.plainText = Array.isArray(resource.files) ? resource.files.map(item => text(objectOrEmpty(item).name)).join(', ') : '';
  return property;
}
function assignDate(property: PageProperty, raw: unknown): void {
  if (!raw) return;
  const date = objectOrEmpty(raw); const start = text(date.start); if (!start) return;
  const end = text(date.end); const timeZone = text(date.time_zone);
  property.date = { start, ...(end ? { end } : {}), ...(timeZone ? { timeZone } : {}) };
  property.plainText = end ? `${start}/${end}` : start;
}
function assignComputed(property: PageProperty, raw: unknown, isRollup: boolean): void {
  if (!raw) return;
  const computed = objectOrEmpty(raw);
  if (computed.type === 'string') { const value = text(computed.string); if (value) property.text = value; property.plainText = value; }
  else if (computed.type === 'number' && typeof computed.number === 'number') { property.number = computed.number; property.plainText = decimalNumber(computed.number); }
  else if (computed.type === 'boolean' && typeof computed.boolean === 'boolean') { property.checkbox = computed.boolean; property.plainText = String(computed.boolean); }
  else if (computed.type === 'date') assignDate(property, computed.date);
  else if (isRollup && computed.type === 'array') { const entries = arrayOrEmpty(computed.array); if (entries.length > 25) property.truncated = true; property.plainText = entries.slice(0, 25).map(entry => decodeProperty(entry, false)).map(item => item.plainText).filter(Boolean).join(', '); }
}
function renderBlock(block: Record<string, unknown>, number: number): string | null {
  const type = text(block.type); const content = objectOrEmpty(block[type]); const richText = joinRichText(content.rich_text);
  if (['paragraph', 'heading_1', 'heading_2', 'heading_3', 'heading_4', 'quote', 'callout', 'toggle', 'code', 'template'].includes(type)) return richText;
  if (type === 'bulleted_list_item') return `- ${richText}`;
  if (type === 'numbered_list_item') return `${number + 1}. ${richText}`;
  if (type === 'to_do') return `${content.checked === true ? '[x]' : '[ ]'} ${richText}`;
  if (type === 'child_page' || type === 'child_database') return text(content.title) || null;
  if (type === 'equation') return text(content.expression) || null;
  if (['bookmark', 'embed', 'link_preview'].includes(type)) return text(content.url) || null;
  if (type === 'table_row') return Array.isArray(content.cells) && content.cells.length > 0 ? content.cells.map(joinRichText).join(' | ') : null;
  if (['image', 'video', 'file', 'pdf', 'audio'].includes(type)) return joinRichText(content.caption) || null;
  return null;
}

function encodeFilter(filter: QueryFilter, depth = 0, counter = { count: 0 }): Record<string, unknown> {
  const compounds = [filter.and !== undefined, filter.or !== undefined].filter(Boolean).length;
  const leaves = [Boolean(filter.property), Boolean(filter.timestamp)].filter(Boolean).length;
  if (compounds + leaves !== 1) throw new Error('one filter kind required');
  if (compounds) {
    if (depth >= 2 || filter.type || filter.condition || filter.text || filter.number !== undefined || filter.checkbox !== undefined || filter.date || filter.id) throw new Error('invalid compound filter');
    const key = filter.and !== undefined ? 'and' : 'or'; const children = filter.and ?? filter.or ?? [];
    if (!children.length || children.length > 100) throw new Error('invalid filter count');
    return { [key]: children.map(child => encodeFilter(child, depth + 1, counter)) };
  }
  const kind = filter.timestamp ? 'date' : filter.type;
  if (filter.timestamp && (filter.type || !['created_time', 'last_edited_time'].includes(filter.timestamp))) throw new Error('invalid timestamp filter');
  if (filter.property && (!filter.property.trim() || filter.property.trim() !== filter.property || [...filter.property].length > 256)) throw new Error('invalid property');
  const condition = filter.condition ?? ''; if (!condition) throw new Error('condition required');
  counter.count++; if (counter.count > 100) throw new Error('too many conditions');
  const empty = { is_empty: 'none', is_not_empty: 'none' };
  const text = { equals: 'text', does_not_equal: 'text', contains: 'text', does_not_contain: 'text', starts_with: 'text', ends_with: 'text', ...empty };
  const number = { equals: 'number', does_not_equal: 'number', greater_than: 'number', less_than: 'number', greater_than_or_equal_to: 'number', less_than_or_equal_to: 'number', ...empty };
  const uniqueId: Record<string, string> = { ...number };
  delete uniqueId.is_empty; delete uniqueId.is_not_empty;
  const date: Record<string, string> = { equals: 'date', before: 'date', after: 'date', on_or_before: 'date', on_or_after: 'date', past_week: 'relative', past_month: 'relative', past_year: 'relative', next_week: 'relative', next_month: 'relative', next_year: 'relative', this_week: 'relative' };
  const conditions: Record<string, Record<string, string>> = {
    title: text, rich_text: text, url: text, email: text, phone_number: text,
    number, unique_id: uniqueId,
    checkbox: { equals: 'checkbox', does_not_equal: 'checkbox' },
    select: { equals: 'text', does_not_equal: 'text', ...empty }, status: { equals: 'text', does_not_equal: 'text', ...empty },
    multi_select: { contains: 'text', does_not_contain: 'text', ...empty },
    people: { contains: 'id', does_not_contain: 'id', ...empty }, relation: { contains: 'id', does_not_contain: 'id', ...empty },
    created_by: { contains: 'id', does_not_contain: 'id', ...empty }, last_edited_by: { contains: 'id', does_not_contain: 'id', ...empty },
    date: { ...date, ...empty }, created_time: { ...date, ...empty }, last_edited_time: { ...date, ...empty }, files: empty,
  };
  const expected = (filter.timestamp ? date : conditions[kind ?? ''])?.[condition] ?? '';
  if (!expected) throw new Error('unsupported filter condition');
  const supplied = [filter.text ? 'text' : '', filter.number !== undefined ? 'number' : '', filter.checkbox !== undefined ? 'checkbox' : '', filter.date ? 'date' : '', filter.id ? 'id' : ''].filter(Boolean);
  if (expected === 'none' || expected === 'relative') { if (supplied.length) throw new Error('unexpected condition value'); }
  else if (supplied.length !== 1 || supplied[0] !== expected) throw new Error('wrong condition value');
  const value = expected === 'none' ? true : expected === 'relative' ? {} : expected === 'text' ? filter.text : expected === 'number' ? filter.number : expected === 'checkbox' ? filter.checkbox : expected === 'date' ? validDate(filter.date!) : parseID(filter.id!);
  if (expected === 'text' && (!filter.text?.trim() || [...filter.text].length > 2000)) throw new Error('invalid filter text');
  if (expected === 'number' && !Number.isFinite(filter.number)) throw new Error('invalid filter number');
  const field = filter.timestamp || filter.type!;
  return { ...(filter.timestamp ? { timestamp: filter.timestamp } : { property: filter.property }), [field]: { [condition]: value } };
}
function encodeSort(sort: QuerySort): Record<string, string> {
  const direction = sort.direction || 'ascending';
  if (!['ascending', 'descending'].includes(direction) || Boolean(sort.property) === Boolean(sort.timestamp)) throw new Error('invalid sort');
  if (sort.timestamp && !['created_time', 'last_edited_time'].includes(sort.timestamp)) throw new Error('invalid timestamp sort');
  if (sort.property && (!sort.property.trim() || sort.property.trim() !== sort.property || [...sort.property].length > 256)) throw new Error('invalid property sort');
  return sort.timestamp ? { timestamp: sort.timestamp, direction } : { property: sort.property!, direction };
}
function encodeProperties(properties: Record<string, PropertyValue>): Record<string, unknown> {
  if (Object.keys(properties).length > 100) throw new Error('too many properties');
  const encoded: Record<string, unknown> = {};
  for (const key of Object.keys(properties).sort()) {
    if (!key.trim() || key !== key.trim() || [...key].length > 256 || /[\r\n]/.test(key)) throw new Error('invalid property name');
    encoded[key] = encodeProperty(properties[key]!);
  }
  return encoded;
}
function encodeProperty(value: PropertyValue): Record<string, unknown> {
  const type = value.type;
  const permitted: Record<string, string[]> = { title: ['text'], rich_text: ['text'], number: ['number'], checkbox: ['checkbox'], select: ['option'], status: ['option'], multi_select: ['options'], date: ['date'], url: ['url'], email: ['email'], phone_number: ['phoneNumber'], people: ['userIds'], relation: ['pageIds'] };
  const fields = ['text', 'number', 'checkbox', 'option', 'options', 'date', 'url', 'email', 'phoneNumber', 'userIds', 'pageIds'] as const;
  if (!permitted[type] || fields.some(field => value[field] !== undefined && value[field] !== '' && !(Array.isArray(value[field]) && (value[field] as unknown[]).length === 0) && !permitted[type]!.includes(field))) throw new Error('invalid property type or field');
  if (type === 'title' || type === 'rich_text') return { [type]: richText(value.text ?? '', 20_000) };
  if (type === 'number') { if (value.number !== undefined && !Number.isFinite(value.number)) throw new Error('invalid number'); return { number: value.number ?? null }; }
  if (type === 'checkbox') { if (value.checkbox === undefined) throw new Error('checkbox required'); return { checkbox: value.checkbox }; }
  if (type === 'select' || type === 'status') {
    if (!value.option) { if (type === 'status') throw new Error('status required'); return { select: null }; }
    return { [type]: { name: optionName(value.option) } };
  }
  if (type === 'multi_select') {
    const options = value.options ?? []; if (options.length > 100) throw new Error('too many options');
    const names = options.map(optionName); if (new Set(names).size !== names.length) throw new Error('repeated option');
    return { multi_select: names.map(name => ({ name })) };
  }
  if (type === 'date') {
    if (!value.date) return { date: null };
    const timeZone = value.date.timeZone?.trim() ?? '';
    if (timeZone && (timeZone.length > 64 || !/^[A-Za-z][A-Za-z0-9_+-]*(\/[A-Za-z0-9_+-]+){0,3}$/.test(timeZone))) throw new Error('invalid timeZone');
    return { date: { start: validDate(value.date.start), ...(value.date.end?.trim() ? { end: validDate(value.date.end) } : {}), ...(timeZone ? { time_zone: timeZone } : {}) } };
  }
  if (type === 'url') { if (value.url) { const url = new URL(value.url.trim()); if (!url.host || value.url.length > 2000) throw new Error('invalid URL'); } return { url: value.url?.trim() || null }; }
  if (type === 'email') { const email = value.email?.trim() ?? ''; if (email && !isPlainEmailAddress(email)) throw new Error('invalid email'); return { email: email || null }; }
  if (type === 'phone_number') { const phone = value.phoneNumber?.trim() ?? ''; if ([...phone].length > 200 || /[\r\n]/.test(phone)) throw new Error('invalid phone'); return { phone_number: phone || null }; }
  if (type === 'people' || type === 'relation') {
    const ids = (type === 'people' ? value.userIds : value.pageIds) ?? []; if (ids.length > 100) throw new Error('too many IDs');
    const unique = [...new Set(ids.map(parseID))]; return { [type]: unique.map(id => type === 'people' ? { object: 'user', id } : { id }) };
  }
  throw new Error('unsupported property');
}
function richText(value: string, limit: number): unknown[] {
  if ([...value].length > limit) throw new Error('text too long');
  const chunks: string[] = []; let current = '';
  for (const character of value) { if (current.length + character.length > 2000) { chunks.push(current); current = ''; } current += character; }
  if (current) chunks.push(current); if (chunks.length > 100) throw new Error('too many text chunks');
  return chunks.map(content => ({ type: 'text', text: { content } }));
}
function encodeParagraphs(value: string): unknown[] {
  if ([...value].length > 50_000) throw new Error('body too long');
  const normalized = value.replaceAll('\r\n', '\n').trim(); if (!normalized) return [];
  const paragraphs = normalized.split(/\n[ \t]*\n\s*/); if (paragraphs.length > 100) throw new Error('too many paragraphs');
  return paragraphs.map(paragraph => ({ object: 'block', type: 'paragraph', paragraph: { rich_text: richText(paragraph.replace(/[ \t\n]+$/, ''), 50_000) } }));
}
function optionName(value: string): string { const name = value.trim(); if (!name || [...name].length > 100 || /[,\r\n]/.test(name)) throw new Error('invalid option name'); return name; }
function validDate(value: string): string {
  const date = value.trim();
  const match = date.match(/^(\d{4})-(\d{2})-(\d{2})(?:T(\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(Z|[+-]\d{2}:\d{2}))?$/);
  if (!match) throw new Error('invalid date');
  const year = Number(match[1]); const month = Number(match[2]); const day = Number(match[3]);
  const daysInMonth = new Date(Date.UTC(year, month, 0)).getUTCDate();
  if (month < 1 || month > 12 || day < 1 || day > daysInMonth) throw new Error('invalid date');
  if (match[4] !== undefined && (Number(match[4]) > 23 || Number(match[5]) > 59 || Number(match[6]) > 59)) throw new Error('invalid date');
  const zone = match[7];
  if (zone && zone !== 'Z' && (Number(zone.slice(1, 3)) > 23 || Number(zone.slice(4, 6)) > 59)) throw new Error('invalid date');
  return date;
}
function decimalNumber(value: number): string {
  const rendered = String(value);
  if (!/[eE]/.test(rendered)) return rendered;
  const [mantissa, exponentText] = rendered.toLowerCase().split('e');
  const exponent = Number(exponentText);
  const sign = mantissa!.startsWith('-') ? '-' : '';
  const digits = mantissa!.replace('-', '').replace('.', '');
  const integerDigits = mantissa!.replace('-', '').split('.')[0]!.length + exponent;
  if (integerDigits <= 0) return `${sign}0.${'0'.repeat(-integerDigits)}${digits}`;
  if (integerDigits >= digits.length) return `${sign}${digits}${'0'.repeat(integerDigits - digits.length)}`;
  return `${sign}${digits.slice(0, integerDigits)}.${digits.slice(integerDigits)}`;
}
function isPlainEmailAddress(value: string): boolean {
  if (Buffer.byteLength(value) > 200 || /[\r\n]/.test(value)) return false;
  const at = value.indexOf('@');
  if (at < 1 || at !== value.lastIndexOf('@')) return false;
  const local = value.slice(0, at); const domain = value.slice(at + 1);
  const atom = (part: string) => part.length > 0 && !part.startsWith('.') && !part.endsWith('.') && !part.includes('..') && [...part].every(character => {
    const codePoint = character.codePointAt(0)!;
    return (codePoint >= 33 && codePoint <= 126 || codePoint >= 128) && !'()<>[]:;@\\,"'.includes(character);
  });
  if (!atom(local)) return false;
  if (domain.startsWith('[') && domain.endsWith(']')) return isIP(domain.slice(1, -1)) !== 0;
  return atom(domain);
}
