// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, zeroReceipt, type Call, type Mutation, type MutationAttempt, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { createHash } from 'node:crypto';
import { convertWrittenBody } from './markdown.js';
import { documentComparisonText, renderDocumentText } from './document.js';
import { parseStorageDocument } from './storage.js';
import { parseADFDocument } from './adf.js';
import { parseGoRFC3339 } from './time.js';
import type { ConfluenceClient, ConfluenceSession, ConfluenceExchange, ConfluenceClassification } from './client.js';
import { confluenceFailure, decodeObject, pathEscape } from './client.js';
import { SearchPagesDefinition, GetPageDefinition, CreatePageDefinition, UpdatePageDefinition, AddCommentDefinition } from './generated/definitions.js';
import type { SearchPagesInput, SearchPagesOutput, PageSummary, GetPageInput, Page, CreatePageInput, CreatePageOutput, UpdatePageInput, UpdatePageOutput, AddCommentInput, AddCommentOutput, PageSearchFilter } from './generated/models.js';

const emptyTime = '0001-01-01T00:00:00Z';
const idPattern = /^[1-9][0-9]{0,18}$/;
const spaceKeyPattern = /^~?[A-Za-z0-9_-]{1,255}$/;

/** Search a bounded CQL result page. */
export class SearchPagesOperation implements Query<SearchPagesInput, SearchPagesOutput> {
  /** Bind the client. */ constructor(private readonly client: ConfluenceClient) {}
  /** Return the stable definition. */ definition() { return SearchPagesDefinition; }
  /** Execute one authenticated search. */
  async invoke(call: Call, input: SearchPagesInput): Promise<QueryAttempt<SearchPagesOutput>> {
    const operation = 'searchPages';
    let cql: string;
    try { cql = buildCql(input.filter); }
    catch (error) { return newQueryBranch('defect', { cql: '', pages: [] }, zeroReceipt(), confluenceFailure(operation, 'VALIDATION', errorMessage(error))); }
    const pageSize = input.pageSize || 25;
    if (!Number.isInteger(pageSize) || pageSize < 1 || pageSize > 100) return newQueryBranch('defect', { cql: '', pages: [] }, zeroReceipt(), confluenceFailure(operation, 'VALIDATION', 'pageSize must be between 1 and 100'));
    const cursor = (input.cursor || '').trim();
    if (cursor && !isCursor(cursor)) return newQueryBranch('defect', { cql: '', pages: [] }, zeroReceipt(), confluenceFailure(operation, 'VALIDATION', 'cursor must be the printable cursor from a previous search page'));
    const output: SearchPagesOutput = { cql, pages: [] };
    const started = await this.client.startSession(call, operation);
    if ('error' in started) return started.error.isRetryable ? newQueryRetry(started.error.failure, started.error.retryAfterMs) : newQueryBranch('defect', output, zeroReceipt(), started.error.failure);
    const session = started.session;
    const response = await this.client.exchange(session, 'GET', 'search', '/content/search', { cql, limit: String(pageSize), expand: 'space,version', ...(cursor ? { cursor } : {}) });
    const classification = this.client.classifyRead(operation, 'page search', response);
    const receipt = this.client.receipt(session, response);
    if (classification.outcome === 'retry') return newQueryRetry(classification.failure!, classification.retryAfterMs);
    if (classification.outcome !== 'accepted') return newQueryBranch(classification.outcome === 'invalid' ? 'invalidResponse' : classification.outcome === 'defect' ? 'defect' : 'providerRejected', output, receipt, classification.failure);
    let decoded: Record<string, unknown>;
    try { decoded = decodeObject(response); }
    catch { return newQueryBranch('invalidResponse', output, receipt, confluenceFailure(operation, 'PROTOCOL', 'Confluence returned an invalid search page')); }
    const links = objectField(decoded, '_links');
    const nextLink = stringField(links, 'next');
    if (nextLink) {
      try {
        const nextCursor = new URL(nextLink, 'https://confluence.invalid').searchParams.get('cursor') || '';
        if (!isCursor(nextCursor)) throw new TypeError();
        output.nextCursor = nextCursor;
      } catch { return newQueryBranch('invalidResponse', output, receipt, confluenceFailure(operation, 'PROTOCOL', 'Confluence returned an invalid search cursor')); }
    }
    const results = decoded.results;
    if (results !== undefined && !Array.isArray(results)) return newQueryBranch('invalidResponse', { cql, pages: [] }, receipt, confluenceFailure(operation, 'PROTOCOL', 'Confluence returned an invalid search page'));
    for (const resource of (results || []) as unknown[]) {
      try {
        const summary = decodePageSummary(resource, stringField(links, 'base'));
        if (summary && (!input.filter.modifiedSince || summary.lastModifiedAt === emptyTime || parseGoRFC3339(summary.lastModifiedAt)!.epochMs >= parseGoRFC3339(input.filter.modifiedSince)!.epochMs)) (output.pages as PageSummary[]).push(summary);
      } catch (error) { return newQueryBranch('invalidResponse', { cql, pages: [] }, receipt, confluenceFailure(operation, 'PROTOCOL', 'Confluence returned an invalid page in the search results: ' + errorMessage(error))); }
    }
    return newQueryBranch('searched', output, receipt);
  }
}

/** Read a page body and its current version. */
export class GetPageOperation implements Query<GetPageInput, Page> {
  /** Bind the client. */ constructor(private readonly client: ConfluenceClient) {}
  /** Return the stable definition. */ definition() { return GetPageDefinition; }
  /** Execute one page read. */
  async invoke(call: Call, input: GetPageInput): Promise<QueryAttempt<Page>> {
    const operation = 'getPage';
    let pageId: string;
    try { pageId = validateId(input.pageId, 'pageId'); }
    catch (error) { return newQueryBranch('defect', emptyPage(), zeroReceipt(), confluenceFailure(operation, 'VALIDATION', errorMessage(error))); }
    const representation = input.bodyRepresentation || 'storage';
    if (representation !== 'storage' && representation !== 'atlas_doc_format') return newQueryBranch('defect', emptyPage(), zeroReceipt(), confluenceFailure(operation, 'VALIDATION', 'bodyRepresentation must be storage or atlas_doc_format'));
    const format = input.bodyFormat || 'markdown';
    if (format !== 'markdown' && format !== 'plainText') return newQueryBranch('defect', emptyPage(), zeroReceipt(), confluenceFailure(operation, 'VALIDATION', 'bodyFormat must be markdown or plainText'));
    const maxCharacters = input.maxBodyCharacters || 32768;
    if (!Number.isInteger(maxCharacters) || maxCharacters < 1 || maxCharacters > 131072) return newQueryBranch('defect', emptyPage(), zeroReceipt(), confluenceFailure(operation, 'VALIDATION', 'maxBodyCharacters must be between 1 and 131072'));
    const started = await this.client.startSession(call, operation);
    if ('error' in started) return started.error.isRetryable ? newQueryRetry(started.error.failure, started.error.retryAfterMs) : newQueryBranch('defect', emptyPage(), zeroReceipt(), started.error.failure);
    const session = started.session;
    const response = await this.client.exchange(session, 'GET', 'content', `/pages/${pathEscape(pageId)}`, { 'body-format': representation });
    const classification = this.client.classifyRead(operation, 'page read', response);
    const receipt = this.client.receipt(session, response, pageId);
    if (classification.outcome === 'retry') return newQueryRetry(classification.failure!, classification.retryAfterMs);
    if (classification.outcome !== 'accepted') return newQueryBranch(classification.outcome === 'notFound' ? 'notFound' : classification.outcome === 'invalid' ? 'invalidResponse' : classification.outcome === 'defect' ? 'defect' : 'providerRejected', emptyPage(), receipt, classification.failure);
    let decoded: Record<string, unknown>;
    try { decoded = decodeObject(response); }
    catch { return newQueryBranch('invalidResponse', emptyPage(), receipt, confluenceFailure(operation, 'PROTOCOL', 'Confluence returned an invalid page')); }
    try { return newQueryBranch('found', decodePage(decoded, representation, format, maxCharacters), receipt); }
    catch (error) { return newQueryBranch('invalidResponse', emptyPage(), receipt, confluenceFailure(operation, 'PROTOCOL', 'Confluence returned an unreadable page: ' + errorMessage(error))); }
  }
}

/** Create one page, reconciling a previous send by title. */
export class CreatePageOperation implements Mutation<CreatePageInput, CreatePageOutput> {
  /** Bind the client. */ constructor(private readonly client: ConfluenceClient) {}
  /** Return the stable definition. */ definition() { return CreatePageDefinition; }
  /** Derive the stable Dex idempotency key. */ idempotencyKey(callId: string): string { return callId; }
  /** Execute one create with read-back reconciliation. */
  async invoke(call: Call, input: CreatePageInput): Promise<MutationAttempt<CreatePageOutput>> {
    const operation = 'createPage';
    const requested: CreatePageOutput = { title: (input.title || '').trim() };
    const spaceId = (input.spaceId || '').trim(), spaceKey = (input.spaceKey || '').trim(), parentId = (input.parentPageId || '').trim();
    if (spaceId) requested.spaceId = spaceId;
    if (spaceKey) requested.spaceKey = spaceKey;
    if (parentId) requested.parentPageId = parentId;
    let storage: string, comparisonText: string;
    try {
      if (!!spaceId === !!spaceKey) throw new TypeError('set exactly one of spaceId and spaceKey');
      if (spaceId && !idPattern.test(spaceId)) throw new TypeError('spaceId must be a numeric Confluence space ID');
      if (spaceKey && !spaceKeyPattern.test(spaceKey)) throw new TypeError('spaceKey must be a space key such as OPS');
      if (parentId && !idPattern.test(parentId)) throw new TypeError('parentPageId must be a numeric Confluence page ID');
      requested.title = validateTitle(input.title);
      ({ storage, comparisonText } = convertWrittenBody(input.body, input.bodyFormat, 'body'));
    } catch (error) { return newMutationBranch('defect', requested, zeroReceipt(), confluenceFailure(operation, 'VALIDATION', errorMessage(error))); }
    const started = await this.client.startSession(call, operation);
    if ('error' in started) return started.error.isRetryable ? newMutationRetry(started.error.failure, started.error.retryAfterMs) : newMutationBranch('defect', requested, zeroReceipt(), started.error.failure);
    const session = started.session;
    if (!spaceId) {
      const resolved = await resolveSpaceId(this.client, session, operation, spaceKey);
      if ('classification' in resolved) {
        const state = resolved.classification;
        if (state.outcome === 'retry') return newMutationRetry(state.failure!, state.retryAfterMs);
        const branch = state.outcome === 'notFound' ? 'notFound' : state.outcome === 'defect' ? 'defect' : 'providerRejected';
        return newMutationBranch(branch, requested, this.client.receipt(session, emptyResponse()), state.failure);
      }
      requested.spaceId = resolved.spaceId;
    }
    const body = { spaceId: requested.spaceId!, status: 'current', title: requested.title, ...(parentId ? { parentId } : {}), body: { representation: 'storage', value: storage! } };
    const earliestSendAt = earliestDispatchTime(call, this.client.now());
    const checkpoint = readDispatchCheckpoint(call);
    const firstSendAt = checkpoint ? Math.min(earliestSendAt, checkpoint.dispatchedAt || earliestSendAt) : earliestSendAt;
    if (checkpoint) {
      const lookup = await findPageByTitle(this.client, session, operation, body.spaceId, body.title);
      const decided = this.reconcileByTitle(session, requested, comparisonText!, parentId, firstSendAt, lookup);
      if (decided) return decided;
    }
    try { await call.context.recordHeartbeat({ dispatchedAtUnixMilli: firstSendAt }); }
    catch { return newMutationRetry(confluenceFailure(operation, 'AVAILABILITY', 'the create checkpoint could not be recorded, so nothing was sent to Confluence')); }
    const response = await this.client.exchange(session, 'POST', 'content', '/pages', undefined, body);
    let state = this.client.classifyWrite(operation, 'page', response);
    const receipt = this.client.receipt(session, response);
    if (state.outcome === 'accepted') {
      try {
        const resource = decodeObject(response); validatePageResource(resource);
        return newMutationBranch('created', createdOutput(requested, resource, '', false), this.client.receipt(session, response, stringField(resource, 'id')));
      } catch { state = { outcome: 'ambiguous', failure: confluenceFailure(operation, 'PROTOCOL', 'Confluence accepted the page but returned an unusable page'), retryAfterMs: 0 }; }
    }
    if (state.outcome === 'notApplied') { clearDispatchCheckpoint(call); return newMutationRetry(state.failure!, state.retryAfterMs); }
    if (state.outcome === 'notFound') return newMutationBranch('notFound', requested, receipt, state.failure);
    if (state.outcome === 'defect') { clearDispatchCheckpoint(call); return newMutationBranch('defect', requested, receipt, state.failure); }
    if (state.outcome === 'rejected') {
      if (response.status !== 400 && response.status !== 409) return newMutationBranch('providerRejected', requested, receipt, state.failure);
      const lookup = await findPageByTitle(this.client, session, operation, body.spaceId, body.title);
      const decided = this.reconcileByTitle(session, requested, comparisonText!, parentId, firstSendAt, lookup);
      return decided || newMutationBranch('providerRejected', requested, receipt, state.failure);
    }
    const lookup = await findPageByTitle(this.client, session, operation, body.spaceId, body.title);
    if (lookup.page) {
      const decided = this.reconcileByTitle(session, requested, comparisonText!, parentId, firstSendAt, lookup);
      if (decided) return decided;
    }
    return newMutationRetry(state.failure!, Math.max(state.retryAfterMs, 5000));
  }

  private reconcileByTitle(session: ConfluenceSession, requested: CreatePageOutput, comparisonText: string, parentId: string, earliestSendAt: number, lookup: PageLookup): MutationAttempt<CreatePageOutput> | undefined {
    const operation = 'createPage';
    const state = lookup.classification;
    if (state.outcome === 'notFound') return newMutationBranch('notFound', requested, this.client.receipt(session, lookup.response), state.failure);
    if (state.outcome === 'rejected') return undefined;
    if (state.outcome !== 'accepted') return newMutationRetry({ ...state.failure!, message: 'an earlier create may have reached Confluence and its page could not be looked up: ' + state.failure!.message }, Math.max(state.retryAfterMs, 5000));
    if (!lookup.page) return undefined;
    const page = lookup.page;
    const output = createdOutput(requested, page, lookup.siteBase, false);
    const receipt = this.client.receipt(session, lookup.response, stringField(page, 'id'));
    const createdAt = parseGoRFC3339(stringField(page, 'createdAt'))?.epochMs;
    const body = objectField(objectField(page, 'body'), 'storage');
    const matches = createdAt !== undefined && createdAt >= earliestSendAt - 60_000 && (!parentId || stringField(page, 'parentId') === parentId) && storageComparisonText(stringField(body, 'value')) === comparisonText;
    if (matches) return newMutationBranch('created', { ...output, isConfirmedByTitleLookup: true }, receipt);
    return newMutationBranch('titleConflict', output, receipt, confluenceFailure(operation, 'CONFLICT', 'another page in the space already has this title, so nothing was created'));
  }
}

/** Replace one page at a named version, reconciling its marker on read-back. */
export class UpdatePageOperation implements Mutation<UpdatePageInput, UpdatePageOutput> {
  /** Bind the client. */ constructor(private readonly client: ConfluenceClient) {}
  /** Return the stable definition. */ definition() { return UpdatePageDefinition; }
  /** Derive the stable Dex idempotency key. */ idempotencyKey(callId: string): string { return callId; }
  /** Execute one update with version reconciliation. */
  async invoke(call: Call, input: UpdatePageInput): Promise<MutationAttempt<UpdatePageOutput>> {
    const operation = 'updatePage';
    const requested: UpdatePageOutput = { pageId: (input.pageId || '').trim(), title: (input.title || '').trim() };
    let pageId: string, marker: string, body: Record<string, unknown>;
    try {
      pageId = validateId(input.pageId, 'pageId');
      requested.title = validateTitle(input.title);
      if (!Number.isInteger(input.nextVersionNumber) || input.nextVersionNumber < 2) throw new TypeError("nextVersionNumber must be the page's current version number plus one, at least 2");
      const message = (input.versionMessage || '').trim();
      if ([...message].length > 200) throw new TypeError('versionMessage cannot exceed 200 characters');
      if (hasControlCharacter(message)) throw new TypeError('versionMessage must be one line without control characters');
      const { storage } = convertWrittenBody(input.body, input.bodyFormat, 'body');
      marker = 'dex:' + createHash('sha256').update(call.idempotencyKey || call.id).digest('hex').slice(0, 16);
      body = { id: pageId, status: 'current', title: requested.title, body: { representation: 'storage', value: storage }, version: { number: input.nextVersionNumber, message: (message ? message + ' ' : '') + `[${marker}]` } };
    } catch (error) { return newMutationBranch('defect', requested, zeroReceipt(), confluenceFailure(operation, 'VALIDATION', errorMessage(error))); }
    const started = await this.client.startSession(call, operation);
    if ('error' in started) return started.error.isRetryable ? newMutationRetry(started.error.failure, started.error.retryAfterMs) : newMutationBranch('defect', requested, zeroReceipt(), started.error.failure);
    const session = started.session;
    const response = await this.client.exchange(session, 'PUT', 'content', `/pages/${pathEscape(pageId!)}`, undefined, body!);
    let classification = this.client.classifyWrite(operation, 'page update', response);
    const receipt = this.client.receipt(session, response, pageId!);
    if (classification.outcome === 'accepted') {
      try {
        const page = decodeObject(response); validatePageResource(page);
        if (numberField(objectField(page, 'version'), 'number') !== input.nextVersionNumber) throw new TypeError();
        return newMutationBranch('updated', updatedOutput(requested, page, '', false), receipt);
      } catch { classification = { outcome: 'ambiguous', failure: confluenceFailure(operation, 'PROTOCOL', 'Confluence accepted the update but returned an unusable page'), retryAfterMs: 0 }; }
    }
    if (classification.outcome === 'notApplied') return newMutationRetry(classification.failure!, classification.retryAfterMs);
    if (classification.outcome === 'notFound') return newMutationBranch('notFound', requested, receipt, classification.failure);
    if (classification.outcome === 'defect') return newMutationBranch('defect', requested, receipt, classification.failure);
    if (classification.outcome === 'rejected' && response.status !== 400 && response.status !== 409) return newMutationBranch('providerRejected', requested, receipt, classification.failure);
    const lookup = await readPage(this.client, session, operation, pageId!);
    const readBack = await this.reportReadBack(session, requested, input.nextVersionNumber, marker!, lookup);
    if (readBack) return readBack;
    if (classification.outcome === 'rejected') return newMutationBranch('providerRejected', requested, this.client.receipt(session, lookup.response, pageId!), classification.failure);
    return newMutationRetry(classification.failure!, Math.max(classification.retryAfterMs, 5000));
  }

  private async reportReadBack(session: ConfluenceSession, requested: UpdatePageOutput, nextVersion: number, marker: string, lookup: PageLookup): Promise<MutationAttempt<UpdatePageOutput> | undefined> {
    const operation = 'updatePage';
    const receipt = this.client.receipt(session, lookup.response, requested.pageId);
    const classification = lookup.classification;
    if (classification.outcome === 'notFound') return newMutationBranch('notFound', requested, receipt, classification.failure);
    if (classification.outcome !== 'accepted') return newMutationRetry({ ...classification.failure!, message: 'the update outcome is unknown and the page could not be read back: ' + classification.failure!.message }, Math.max(classification.retryAfterMs, 5000));
    const page = lookup.page!;
    const version = objectField(page, 'version'), current = numberField(version, 'number');
    if (current === nextVersion && stringField(version, 'message').includes(marker)) return newMutationBranch('updated', updatedOutput(requested, page, lookup.siteBase, true), receipt);
    if (current === nextVersion - 1) return undefined;
    if (current > nextVersion) {
      const response = await this.client.exchange(session, 'GET', 'content', `/pages/${pathEscape(requested.pageId)}/versions/${nextVersion}`);
      const state = this.client.classifyRead(operation, 'page version', response);
      if (state.outcome === 'retry') return newMutationRetry(state.failure!, state.retryAfterMs);
      if (state.outcome === 'accepted') {
        try {
          const oldVersion = decodeObject(response);
          if (numberField(oldVersion, 'number') === nextVersion && stringField(oldVersion, 'message').includes(marker)) return newMutationBranch('updated', { ...updatedOutput(requested, page, lookup.siteBase, true), versionNumber: nextVersion }, receipt);
        } catch { /* An unreadable prior version is treated as a conflicting edit. */ }
      }
    }
    return newMutationBranch('versionConflict', updatedOutput(requested, page, lookup.siteBase, false), receipt, confluenceFailure(operation, 'CONFLICT', 'the page is no longer at the version before nextVersionNumber, so nothing was changed'));
  }
}

/** Add one footer comment at most once per Step execution. */
export class AddCommentOperation implements Mutation<AddCommentInput, AddCommentOutput> {
  /** Bind the client. */ constructor(private readonly client: ConfluenceClient) {}
  /** Return the stable definition. */ definition() { return AddCommentDefinition; }
  /** Derive the stable Dex idempotency key. */ idempotencyKey(callId: string): string { return callId; }
  /** Execute one comment send or read-back. */
  async invoke(call: Call, input: AddCommentInput): Promise<MutationAttempt<AddCommentOutput>> {
    const operation = 'addComment';
    const requested: AddCommentOutput = { pageId: (input.pageId || '').trim(), createdAt: emptyTime };
    let pageId: string, storage: string, comparisonText: string;
    try { pageId = validateId(input.pageId, 'pageId'); ({ storage, comparisonText } = convertWrittenBody(input.body, input.bodyFormat, 'body')); }
    catch (error) { return newMutationBranch('defect', requested, zeroReceipt(), confluenceFailure(operation, 'VALIDATION', errorMessage(error))); }
    const started = await this.client.startSession(call, operation);
    if ('error' in started) return started.error.isRetryable ? newMutationRetry(started.error.failure, started.error.retryAfterMs) : newMutationBranch('defect', requested, zeroReceipt(), started.error.failure);
    const session = started.session;
    const checkpoint = readDispatchCheckpoint(call);
    const earliestSendAt = earliestDispatchTime(call, this.client.now());
    if (checkpoint) return this.reconcileEarlierDispatch(session, requested, comparisonText!, Math.min(earliestSendAt, checkpoint.dispatchedAt || earliestSendAt));
    try { await call.context.recordHeartbeat({ dispatchedAtUnixMilli: earliestSendAt }); }
    catch { return newMutationRetry(confluenceFailure(operation, 'AVAILABILITY', 'the comment checkpoint could not be recorded, so nothing was sent to Confluence')); }
    const response = await this.client.exchange(session, 'POST', 'content', '/footer-comments', undefined, { pageId: pageId!, body: { representation: 'storage', value: storage! } });
    let classification = this.client.classifyWrite(operation, 'comment', response);
    const receipt = this.client.receipt(session, response);
    if (classification.outcome === 'accepted') {
      try {
        const comment = decodeObject(response); const id = stringField(comment, 'id');
        if (idPattern.test(id)) return newMutationBranch('added', addedCommentOutput(requested, comment, false), this.client.receipt(session, response, id));
      } catch { /* Unusable success is reconciled after a retry. */ }
      classification = { outcome: 'ambiguous', failure: confluenceFailure(operation, 'PROTOCOL', 'Confluence accepted the comment but returned an unusable comment'), retryAfterMs: 0 };
    }
    if (classification.outcome === 'notApplied') { clearDispatchCheckpoint(call); return newMutationRetry(classification.failure!, classification.retryAfterMs); }
    if (classification.outcome === 'notFound') return newMutationBranch('notFound', requested, receipt, classification.failure);
    if (classification.outcome === 'rejected') return newMutationBranch('providerRejected', requested, receipt, classification.failure);
    if (classification.outcome === 'defect') { clearDispatchCheckpoint(call); return newMutationBranch('defect', requested, receipt, classification.failure); }
    return newMutationRetry(classification.failure!, Math.max(classification.retryAfterMs, 5000));
  }

  private async reconcileEarlierDispatch(session: ConfluenceSession, requested: AddCommentOutput, comparisonText: string, earliestSendAt: number): Promise<MutationAttempt<AddCommentOutput>> {
    const operation = 'addComment';
    const response = await this.client.exchange(session, 'GET', 'content', `/pages/${pathEscape(requested.pageId)}/footer-comments`, { 'body-format': 'storage', sort: '-created-date', limit: '25' });
    const classification = this.client.classifyRead(operation, 'page comments', response);
    const receipt = this.client.receipt(session, response);
    if (classification.outcome === 'retry') return newMutationRetry(classification.failure!, classification.retryAfterMs);
    if (classification.outcome === 'notFound') return newMutationBranch('notFound', requested, receipt, classification.failure);
    if (classification.outcome !== 'accepted') return newMutationUncertain(requested, { ...classification.failure!, message: "an earlier attempt of this Step sent the comment, and the page's comments could not be read back: " + classification.failure!.message }, receipt);
    let results: unknown[];
    try { const decoded = decodeObject(response); if (!Array.isArray(decoded.results)) throw new TypeError(); results = decoded.results; }
    catch { return newMutationUncertain(requested, confluenceFailure(operation, 'PROTOCOL', "an earlier attempt of this Step sent the comment, and the page's comments read back are invalid"), receipt); }
    for (const value of results) {
      if (!value || typeof value !== 'object' || Array.isArray(value)) continue;
      const comment = value as Record<string, unknown>;
      try {
        const id = stringField(comment, 'id'), version = objectField(comment, 'version'), body = objectField(objectField(comment, 'body'), 'storage');
        const createdAt = parseGoRFC3339(stringField(version, 'createdAt'))?.epochMs;
        if (idPattern.test(id) && createdAt !== undefined && createdAt >= earliestSendAt - 60_000 && storageComparisonText(stringField(body, 'value')) === comparisonText) return newMutationBranch('added', addedCommentOutput(requested, comment, true), this.client.receipt(session, response, id));
      } catch { /* Invalid comments cannot prove this Step sent them. */ }
    }
    return newMutationUncertain(requested, confluenceFailure(operation, 'TRANSPORT', 'an earlier attempt of this Step sent the comment without a confirmed outcome, and the page shows no matching comment, so it is not sent again'), receipt);
  }
}

function errorMessage(error: unknown): string { return error instanceof Error ? error.message : 'invalid input'; }
function objectField(object: Record<string, unknown>, key: string): Record<string, unknown> { const value = object[key]; if (value == null) return {}; if (typeof value !== 'object' || Array.isArray(value)) throw new TypeError(`${key} has an invalid type`); return value as Record<string, unknown>; }
function stringField(object: Record<string, unknown>, key: string): string { const value = object[key]; if (value !== undefined && typeof value !== 'string') throw new TypeError(`${key} has an invalid type`); return value || ''; }
function numberField(object: Record<string, unknown>, key: string): number { const value = object[key]; if (value !== undefined && (typeof value !== 'number' || !Number.isInteger(value))) throw new TypeError(`${key} has an invalid type`); return value || 0; }
function isCursor(value: string): boolean { return !!value && Buffer.byteLength(value) <= 4096 && /^[\x21-\x7e]+$/.test(value); }
function validateId(value: string, field: string): string { const trimmed = value.trim(); if (!idPattern.test(trimmed)) throw new TypeError(`${field} must be a numeric Confluence content ID`); return trimmed; }
function emptyPage(): Page { return { id: '', title: '', status: '', spaceId: '', createdAt: emptyTime, version: { number: 0, createdAt: emptyTime }, body: '', bodyFormat: '' }; }
function webURL(base: string, path: string): string { if (!base.startsWith('https://') || !/^\/[\x21-\x7e]*$/.test(path)) return ''; try { const parsed = new URL(base); if (parsed.username || parsed.password || parsed.search || parsed.hash) return ''; } catch { return ''; } const url = base.replace(/\/+$/, '') + path; return Buffer.byteLength(url) <= 2048 ? url : ''; }
function decodePageSummary(value: unknown, base: string): PageSummary | undefined {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('page resource is invalid');
  const resource = value as Record<string, unknown>;
  if (stringField(resource, 'type') !== 'page') return undefined;
  const id = stringField(resource, 'id'); const title = stringField(resource, 'title');
  if (!idPattern.test(id) || !title) throw new TypeError('page ID or title is missing');
  const summary: PageSummary = { id, title, lastModifiedAt: emptyTime };
  const status = stringField(resource, 'status'); if (status) summary.status = status;
  const links = objectField(resource, '_links'); const url = webURL(base, stringField(links, 'webui')); if (url) summary.webUrl = url;
  const space = objectField(resource, 'space');
  if (space.id !== undefined) { const spaceId = typeof space.id === 'number' ? String(space.id) : stringField(space, 'id'); if (idPattern.test(spaceId)) summary.spaceId = spaceId; }
  const spaceKey = stringField(space, 'key'); if (spaceKeyPattern.test(spaceKey)) summary.spaceKey = spaceKey;
  const version = objectField(resource, 'version'); if (version.number !== undefined) summary.versionNumber = numberField(version, 'number');
  const when = parseGoRFC3339(stringField(version, 'when')); if (when) summary.lastModifiedAt = when.canonical;
  return summary;
}
function decodePage(resource: Record<string, unknown>, representation: string, format: string, maxCharacters: number): Page {
  const id = stringField(resource, 'id'), title = stringField(resource, 'title'), status = stringField(resource, 'status'), spaceId = stringField(resource, 'spaceId');
  if (!idPattern.test(id) || !title || !status || !idPattern.test(spaceId)) throw new TypeError('page identity is invalid');
  const versionRecord = objectField(resource, 'version'); const versionNumber = numberField(versionRecord, 'number');
  if (versionNumber < 1) throw new TypeError('page version is invalid');
  const bodyRecord = objectField(resource, 'body'); const body = objectField(bodyRecord, representation); const rawBody = stringField(body, 'value');
  const rendered = renderDocumentText(representation === 'storage' ? parseStorageDocument(rawBody) : parseADFDocument(rawBody), format, maxCharacters);
  const page: Page = { id, title, status, spaceId, createdAt: normalizedTime(stringField(resource, 'createdAt')),
    version: { number: versionNumber, createdAt: normalizedTime(stringField(versionRecord, 'createdAt')) },
    body: rendered.text, bodyFormat: format };
  const parentId = stringField(resource, 'parentId'); if (idPattern.test(parentId)) page.parentPageId = parentId;
  const authorId = stringField(resource, 'authorId'); if (authorId) page.authorAccountId = authorId;
  const versionAuthorId = stringField(versionRecord, 'authorId'); if (versionAuthorId) page.version.authorAccountId = versionAuthorId;
  if (versionRecord.minorEdit === true) page.version.isMinorEdit = true;
  if (rendered.isTruncated) page.isBodyTruncated = true;
  const links = objectField(resource, '_links'); const url = webURL(stringField(links, 'base'), stringField(links, 'webui')); if (url) page.webUrl = url;
  return page;
}
function normalizedTime(value: string): string { return parseGoRFC3339(value)?.canonical || emptyTime; }
function buildCql(filter: PageSearchFilter): string {
  const clauses = ['type = page'];
  for (const [field, values, valid, description] of [
    ['space', filter.spaceKeys || [], spaceKeyPattern, 'a space key such as OPS'],
    ['label', filter.labels || [], /^[^ \t\r\n,]+$/, 'a label without whitespace'],
  ] as const) {
    if (values.length > 50) throw new TypeError(`filter accepts at most 50 ${field} values`);
    if (values.length) { const quoted = values.map(value => { const trimmed = value.trim(); if (!valid.test(trimmed) || field === 'label' && [...trimmed].length > 255) throw new TypeError(`filter ${field} values must each be ${description}`); return quoteCqlString(trimmed); }); clauses.push(`${field} in (${quoted.join(', ')})`); }
  }
  const phrase = (filter.titlePhrase || '').trim(); if (phrase) clauses.push('title ~ ' + quoteCqlString(quoteCqlString(phrase)));
  if (filter.modifiedSince) { const timestamp = parseGoRFC3339(filter.modifiedSince); if (!timestamp || filter.modifiedSince.startsWith('0001-01-01T00:00:00')) throw new TypeError('filter modifiedSince must be a time'); clauses.push('lastmodified >= "' + new Date(timestamp.epochMs - 12 * 60 * 60 * 1000).toISOString().slice(0, 10) + '"'); }
  const additional = (filter.additionalCql || '').trim(); if (additional) { validateAdditionalCql(additional); clauses.push(`(${additional})`); }
  const order = filter.order || 'lastModifiedDescending';
  const suffix = order === 'lastModifiedDescending' ? 'ORDER BY lastmodified DESC' : order === 'createdDescending' ? 'ORDER BY created DESC' : order === 'titleAscending' ? 'ORDER BY title ASC' : '';
  if (!suffix) throw new TypeError('filter order must be lastModifiedDescending, createdDescending, or titleAscending');
  return clauses.join(' AND ') + ' ' + suffix;
}
function quoteCqlString(value: string): string { if ([...value].length > 1024 || hasControlCharacter(value)) throw new TypeError('CQL value cannot contain control characters'); return '"' + value.replace(/\\/g, '\\\\').replace(/"/g, '\\"') + '"'; }
function hasControlCharacter(value: string): boolean { return [...value].some(character => { const code = character.codePointAt(0)!; return code < 32 || code === 127; }); }
function validateAdditionalCql(clause: string): void {
  if ([...clause].length > 4096) throw new TypeError('filter additionalCql cannot exceed 4096 characters');
  let depth = 0, quote = '', escaped = false, unquoted = '';
  for (const character of clause) {
    if (hasControlCharacter(character)) throw new TypeError('filter additionalCql cannot contain control characters');
    if (escaped) escaped = false;
    else if (quote && character === '\\') escaped = true;
    else if (quote && character === quote) quote = '';
    else if (!quote && (character === '"' || character === "'")) quote = character;
    else if (!quote && character === '(') depth++;
    else if (!quote && character === ')') { if (--depth < 0) throw new TypeError('filter additionalCql closes a parenthesis it did not open'); }
    unquoted += quote ? ' ' : character;
  }
  if (quote) throw new TypeError('filter additionalCql has an unterminated string');
  if (depth) throw new TypeError('filter additionalCql has an unclosed parenthesis');
  if (/\border\s+by\b/i.test(unquoted)) throw new TypeError('filter additionalCql cannot contain ORDER BY; use order instead');
}

interface PageLookup { classification: ConfluenceClassification; response: ConfluenceExchange; page?: Record<string, unknown>; siteBase: string; }
function emptyResponse(): ConfluenceExchange { return { status: 0, headers: {}, body: new Uint8Array(), isTooLarge: false, isDispatched: false }; }
function validatePageResource(resource: Record<string, unknown>): void {
  if (!idPattern.test(stringField(resource, 'id'))) throw new TypeError('page ID is not numeric');
  if (!idPattern.test(stringField(resource, 'spaceId'))) throw new TypeError('space ID is not numeric');
  const title = stringField(resource, 'title');
  if (!title || [...title].length > 1020) throw new TypeError('page title is missing or invalid');
  if (numberField(objectField(resource, 'version'), 'number') < 1) throw new TypeError('page version is missing');
}
function validateTitle(value: string): string {
  const title = value.trim();
  if (!title) throw new TypeError('title is required');
  if ([...title].length > 255) throw new TypeError('title cannot exceed 255 characters');
  if (hasControlCharacter(title)) throw new TypeError('title must be one line without control characters');
  return title;
}
function createdOutput(requested: CreatePageOutput, resource: Record<string, unknown>, siteBase: string, isConfirmed: boolean): CreatePageOutput {
  const output: CreatePageOutput = { ...requested, pageId: stringField(resource, 'id'), spaceId: stringField(resource, 'spaceId'), versionNumber: numberField(objectField(resource, 'version'), 'number') };
  delete output.parentPageId;
  const parent = stringField(resource, 'parentId'); if (idPattern.test(parent)) output.parentPageId = parent;
  const links = objectField(resource, '_links'); const url = webURL(stringField(links, 'base') || siteBase, stringField(links, 'webui')); if (url) output.webUrl = url;
  if (isConfirmed) output.isConfirmedByTitleLookup = true;
  return output;
}
function updatedOutput(requested: UpdatePageOutput, resource: Record<string, unknown>, siteBase: string, isConfirmed: boolean): UpdatePageOutput {
  const output: UpdatePageOutput = { ...requested, versionNumber: numberField(objectField(resource, 'version'), 'number') };
  const links = objectField(resource, '_links'); const url = webURL(stringField(links, 'base') || siteBase, stringField(links, 'webui')); if (url) output.webUrl = url;
  if (isConfirmed) output.isConfirmedByReadBack = true;
  return output;
}
function addedCommentOutput(requested: AddCommentOutput, comment: Record<string, unknown>, isConfirmed: boolean): AddCommentOutput {
  const output: AddCommentOutput = { ...requested, commentId: stringField(comment, 'id') };
  const timestamp = stringField(objectField(comment, 'version'), 'createdAt');
  if (parseGoRFC3339(timestamp)) output.createdAt = normalizedTime(timestamp);
  if (isConfirmed) output.isConfirmedByReadBack = true;
  return output;
}
function storageComparisonText(storage: string): string { try { return documentComparisonText(parseStorageDocument(storage)); } catch { return ''; } }
function earliestDispatchTime(call: Call, now: Date): number { const first = call.context.firstAttemptAt.getTime(); return first > 0 ? Math.min(now.getTime(), first) : now.getTime(); }
function readDispatchCheckpoint(call: Call): { dispatchedAt: number } | undefined {
  if (!call.context.hasLastHeartbeatValue()) return undefined;
  try {
    const value: unknown = call.context.getLastHeartbeatValue();
    if (value && typeof value === 'object' && 'dispatchedAtUnixMilli' in value && typeof value.dispatchedAtUnixMilli === 'number' && value.dispatchedAtUnixMilli > 0) return { dispatchedAt: value.dispatchedAtUnixMilli };
  } catch { /* An unreadable heartbeat still means a previous attempt may have sent a write. */ }
  return { dispatchedAt: 0 };
}
function clearDispatchCheckpoint(call: Call): void {
  // A lost clear only causes a read-back before the next attempt; never a duplicate send.
  void call.context.recordHeartbeat(undefined).catch(() => {});
}
async function findPageByTitle(client: ConfluenceClient, session: ConfluenceSession, operation: string, spaceId: string, title: string): Promise<PageLookup> {
  const response = await client.exchange(session, 'GET', 'content', '/pages', { 'space-id': spaceId, title, 'body-format': 'storage', limit: '5' });
  let classification = client.classifyRead(operation, 'page lookup', response);
  const lookup: PageLookup = { classification, response, siteBase: '' };
  if (classification.outcome !== 'accepted') return lookup;
  try {
    const list = decodeObject(response); const results = list.results;
    if (!Array.isArray(results) || results.length > 5) throw new TypeError('invalid page list');
    lookup.siteBase = stringField(objectField(list, '_links'), 'base');
    for (const value of results) {
      if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid page');
      const page = value as Record<string, unknown>; validatePageResource(page);
      if (stringField(page, 'title') === title && stringField(page, 'spaceId') === spaceId) { lookup.page = page; break; }
    }
  } catch {
    classification = { outcome: 'invalid', failure: confluenceFailure(operation, 'PROTOCOL', 'Confluence returned an invalid page list'), retryAfterMs: 0 };
    lookup.classification = classification;
  }
  return lookup;
}
async function readPage(client: ConfluenceClient, session: ConfluenceSession, operation: string, pageId: string): Promise<PageLookup> {
  const response = await client.exchange(session, 'GET', 'content', `/pages/${pathEscape(pageId)}`, { 'body-format': 'storage' });
  const lookup: PageLookup = { classification: client.classifyRead(operation, 'page', response), response, siteBase: '' };
  if (lookup.classification.outcome !== 'accepted') return lookup;
  try { const page = decodeObject(response); validatePageResource(page); lookup.page = page; lookup.siteBase = stringField(objectField(page, '_links'), 'base'); }
  catch { lookup.classification = { outcome: 'invalid', failure: confluenceFailure(operation, 'PROTOCOL', 'Confluence returned an invalid page'), retryAfterMs: 0 }; }
  return lookup;
}
async function resolveSpaceId(client: ConfluenceClient, session: ConfluenceSession, operation: string, spaceKey: string): Promise<{ spaceId: string } | { classification: ConfluenceClassification }> {
  const response = await client.exchange(session, 'GET', 'content', '/spaces', { keys: spaceKey, limit: '1' });
  const classification = client.classifyRead(operation, 'space', response);
  if (classification.outcome !== 'accepted') return { classification };
  try {
    const results = decodeObject(response).results;
    if (!Array.isArray(results)) throw new TypeError();
    for (const value of results) {
      if (!value || typeof value !== 'object' || Array.isArray(value)) continue;
      const resource = value as Record<string, unknown>, id = stringField(resource, 'id');
      if (stringField(resource, 'key').toLowerCase() === spaceKey.toLowerCase() && idPattern.test(id)) return { spaceId: id };
    }
  } catch { return { classification: { outcome: 'invalid', failure: confluenceFailure(operation, 'PROTOCOL', 'Confluence returned an invalid space list'), retryAfterMs: 0 } }; }
  return { classification: { outcome: 'notFound', failure: confluenceFailure(operation, 'NOT_FOUND', `Confluence space ${spaceKey} was not found or is not visible to the connection`), retryAfterMs: 0 } };
}
