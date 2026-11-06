// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newMutationBranch, newMutationRetry, newMutationUncertain, newQueryBranch, newQueryRetry, type Call, type FailureKind, type Mutation, type MutationAttempt, type Query, type QueryAttempt, type ReceiptDraft } from '@superdurable/connectors-sdk';
import { newFence } from '@superdurable/connectors-sdk/singledispatch';
import { randomBytes } from 'node:crypto';
import type { ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { DocsClient, DocsRequestError, docsFailure, goPathEscape, goQuery, googleTokens, isRetryable, isRateLimit, retryAfterMs, statusFailureKind } from './client.js';
import { AppendTextDefinition, CreateDocumentDefinition, GetDocumentTextDefinition, ReplaceDocumentTextDefinition } from './generated/definitions.js';
import { AppendTextInputSchema, CreateDocumentInputSchema, CreatedDocumentSchema, GetDocumentTextInputSchema, ReplaceDocumentTextInputSchema, type AppendTextInput, type AppendTextOutput, type CreateDocumentInput, type CreateDocumentOutput, type CreatedDocument, type Credentials, type GetDocumentTextInput, type GetDocumentTextOutput, type ReplaceDocumentTextInput, type ReplaceDocumentTextOutput } from './generated/models.js';
import { arrayField, decodeDocument, documentBodyText, goHasNonSpace, integerField, isDriveID, objectValue, parseObject, renderDocumentText, stringField, type DocumentResource } from './document.js';

const readFields = 'documentId,title,revisionId,tabs(tabProperties/tabId,childTabs/tabProperties/tabId,documentTab(body,lists))';
const writeFields = 'documentId,revisionId,tabs(tabProperties/tabId,documentTab/body)';
const fileFields = 'id,name,mimeType,parents,webViewLink,createdTime,trashed';
const zeroRead: GetDocumentTextOutput = { documentId: '', title: '', format: '', byteCount: 0 };
const zeroCreate: CreateDocumentOutput = { document: { documentId: '', title: '' } };
const zeroReplace: ReplaceDocumentTextOutput = { documentId: '' };
const zeroAppend: AppendTextOutput = { documentId: '' };

/** Read the first tab as Markdown or plain text. */
export class GetDocumentTextOperation implements Query<GetDocumentTextInput, GetDocumentTextOutput> {
  /** Bind a validated Docs client. */
  constructor(private readonly client: DocsClient) {}
  /** Return the immutable operation definition. */ definition() { return GetDocumentTextDefinition; }
  /** Validate, read, and render one Google Docs revision. */
  async invoke(call: Call, input: GetDocumentTextInput): Promise<QueryAttempt<GetDocumentTextOutput>> {
    if (!GetDocumentTextInputSchema.safeParse(input).success) return newQueryBranch('defect', zeroRead, {}, docsFailure('getDocumentText', 'VALIDATION', 'documentId must be a Google Docs document ID and format must be markdown or plainText'));
    const format = input.format || 'markdown';
    if (!isDriveID(input.documentId) || !['markdown', 'plainText'].includes(format)) return newQueryBranch('defect', zeroRead, {}, docsFailure('getDocumentText', 'VALIDATION', 'documentId must be a Google Docs document ID and format must be markdown or plainText'));
    let credentials: Credentials;
    try { credentials = await this.client.resolveCredentials(call); }
    catch { return newQueryBranch('defect', zeroRead, {}, docsFailure('getDocumentText', 'AUTHENTICATION', 'connection credentials are unavailable')); }
    const url = documentURL(this.client, input.documentId, 'PREVIEW_WITHOUT_SUGGESTIONS', readFields);
    const read = await sendRead(this.client, call, credentials, 'getDocumentText', url, 'tooLarge', 'notFound', zeroRead);
    if ('attempt' in read) return read.attempt;
    const receipt = this.client.receipt(call, read.response, input.documentId);
    let document: DocumentResource;
    try { document = decodeDocument(read.response.body); }
    catch { return newQueryBranch('invalidResponse', zeroRead, receipt, docsFailure('getDocumentText', 'PROTOCOL', 'provider returned an invalid document')); }
    const output: GetDocumentTextOutput = { documentId: document.documentId, title: document.title, ...(document.revisionId ? { revisionId: document.revisionId } : {}), format, byteCount: 0, ...(document.hasOtherTabs ? { hasOtherTabs: true } : {}) };
    let text: string;
    try { text = renderDocumentText(document, format as 'markdown' | 'plainText'); }
    catch { return newQueryBranch('invalidResponse', output, receipt, docsFailure('getDocumentText', 'PROTOCOL', 'document structure could not be rendered')); }
    if (Buffer.byteLength(text, 'utf8') > this.client.maxTextBytes) return newQueryBranch('tooLarge', output, receipt, docsFailure('getDocumentText', 'RESPONSE_TOO_LARGE', 'document text exceeds the configured limit'));
    if (text) output.text = text;
    output.byteCount = Buffer.byteLength(text, 'utf8');
    return newQueryBranch('read', output, receipt);
  }
}

interface ReadFailure<Output> { attempt: QueryAttempt<Output> }

async function sendRead<Output>(client: DocsClient, call: Call, credentials: Credentials, operation: string, url: string, tooLargeBranch: string, notFoundBranch: string, zero: Output, signal?: AbortSignal): Promise<{ response: ProviderResponse } | ReadFailure<Output>> {
  let response: ProviderResponse;
  try { response = await client.request(call, credentials, 'GET', url, undefined, undefined, false, signal); }
  catch (error) {
    if (error instanceof DocsRequestError && error.tooLarge) return { attempt: newQueryBranch(tooLargeBranch, zero, client.receipt(call, { status: error.status, headers: error.headers, body: new Uint8Array() }), docsFailure(operation, 'RESPONSE_TOO_LARGE', 'provider response exceeds the configured limit')) };
    return { attempt: newQueryRetry(docsFailure(operation, 'TRANSPORT', 'provider is unavailable')) };
  }
  if (response.status >= 200 && response.status < 300) return { response };
  const tokens = googleTokens(response);
  if (isRetryable(response.status, tokens)) return { attempt: newQueryRetry(docsFailure(operation, statusFailureKind(response.status, tokens), 'provider temporarily rejected the request'), retryAfterMs(response, client.now())) };
  const receipt = client.receipt(call, response);
  if (response.status === 404) return { attempt: newQueryBranch(notFoundBranch, zero, receipt, docsFailure(operation, 'NOT_FOUND', 'document was not found or is not visible to the connection')) };
  return { attempt: newQueryBranch('providerRejected', zero, receipt, docsFailure(operation, statusFailureKind(response.status, tokens), 'provider rejected the request')) };
}

function documentURL(client: DocsClient, documentId: string, suggestionsViewMode: string, fields: string): string {
  return `${client.documentsBaseURL}/${goPathEscape(documentId)}?${goQuery({ includeTabsContent: 'true', suggestionsViewMode, fields })}`;
}

function filesListURL(client: DocsClient, values: Record<string, string>): string {
  return `${client.driveBaseURL}/files?${goQuery({ ...values, corpora: 'allDrives', supportsAllDrives: 'true', includeItemsFromAllDrives: 'true' })}`;
}

/** Replace the first-tab body or named placeholders under a required revision. */
export class ReplaceDocumentTextOperation implements Mutation<ReplaceDocumentTextInput, ReplaceDocumentTextOutput> {
  /** Bind a validated Docs client. */
  constructor(private readonly client: DocsClient) {}
  /** Return the immutable operation definition. */ definition() { return ReplaceDocumentTextDefinition; }
  /** Use the stable Dex Call ID; the revision guards the write. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate and run one revision-guarded replacement. */
  async invoke(call: Call, input: ReplaceDocumentTextInput): Promise<MutationAttempt<ReplaceDocumentTextOutput>> {
    if (!ReplaceDocumentTextInputSchema.safeParse(input).success) return newMutationBranch('defect', zeroReplace, {}, docsFailure('replaceDocumentText', 'VALIDATION', 'replaceDocumentText input is invalid'));
    try { validateGuardedTarget(input.documentId, input.requiredRevisionId); validateReplaceInput(input, this.client.maxTextBytes); }
    catch (error) { return newMutationBranch('defect', zeroReplace, {}, docsFailure('replaceDocumentText', 'VALIDATION', safeError(error))); }
    return runGuardedWrite(this.client, call, input.documentId, input.requiredRevisionId, 'replaceDocumentText', zeroReplace, 'replaced', (document, bodyText) => buildReplacement(document, bodyText, input),
      bodyText => input.target === 'wholeBody' ? bodyText === `${input.text ?? ''}\n` : (input.placeholders ?? []).every(item => !bodyText.includes(item.placeholder) && bodyText.includes(item.text)),
      (revisionId, replies, wasAlreadyApplied) => ({ documentId: input.documentId, ...(revisionId ? { revisionId } : {}), ...(wasAlreadyApplied ? { wasAlreadyApplied: true } : {}), ...((input.target === 'placeholders' && replies.length) ? { replacements: (input.placeholders ?? []).map((item, index) => ({ placeholder: item.placeholder, occurrencesChanged: isObject(replies[index]?.replaceAllText) ? integerField(replies[index]!.replaceAllText as Record<string, unknown>, 'occurrencesChanged', 0) : 0 })) } : {}) }));
  }
}

/** Append paragraphs under a required revision. */
export class AppendTextOperation implements Mutation<AppendTextInput, AppendTextOutput> {
  /** Bind a validated Docs client. */
  constructor(private readonly client: DocsClient) {}
  /** Return the immutable operation definition. */ definition() { return AppendTextDefinition; }
  /** Use the stable Dex Call ID; the revision guards the write. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate and run one revision-guarded append. */
  async invoke(call: Call, input: AppendTextInput): Promise<MutationAttempt<AppendTextOutput>> {
    if (!AppendTextInputSchema.safeParse(input).success) return newMutationBranch('defect', zeroAppend, {}, docsFailure('appendText', 'VALIDATION', 'appendText input is invalid'));
    try { validateGuardedTarget(input.documentId, input.requiredRevisionId); if (!goHasNonSpace(input.text)) throw new TypeError('text must not be blank'); validateWriteText('text', input.text); if (Buffer.byteLength(input.text) > this.client.maxTextBytes) throw new TypeError('text exceeds the configured text limit'); }
    catch (error) { return newMutationBranch('defect', zeroAppend, {}, docsFailure('appendText', 'VALIDATION', safeError(error))); }
    return runGuardedWrite(this.client, call, input.documentId, input.requiredRevisionId, 'appendText', zeroAppend, 'appended', (document, bodyText) => {
      const separator = bodyText === '\n' ? '' : '\n';
      const insertIndex = document.bodyEndIndex - 1;
      const start = insertIndex + separator.length;
      return { requests: [{ insertText: { text: separator + input.text, location: { index: insertIndex, tabId: document.tabId } } }, ...plainParagraphRequests(document.tabId, start, start + input.text.length)] };
    }, bodyText => bodyText === `${input.text}\n` || bodyText.endsWith(`\n${input.text}\n`), (revisionId, _replies, wasAlreadyApplied) => ({ documentId: input.documentId, ...(revisionId ? { revisionId } : {}), ...(wasAlreadyApplied ? { wasAlreadyApplied: true } : {}) }));
  }
}

interface BuiltChange<Output> { requests: Record<string, unknown>[]; terminal?: MutationAttempt<Output> }

async function runGuardedWrite<Output>(client: DocsClient, call: Call, documentId: string, requiredRevisionId: string, operation: string, zero: Output, appliedBranch: string,
  build: (document: DocumentResource, bodyText: string) => BuiltChange<Output>, held: (bodyText: string) => boolean,
  output: (revisionId: string, replies: Record<string, unknown>[], alreadyApplied: boolean) => Output): Promise<MutationAttempt<Output>> {
  let credentials: Credentials;
  try { credentials = await client.resolveCredentials(call); }
  catch { return newMutationBranch('defect', zero, {}, docsFailure(operation, 'AUTHENTICATION', 'connection credentials are unavailable')); }
  const read = async (): Promise<{ document: DocumentResource; bodyText: string; receipt: ReceiptDraft } | { attempt: MutationAttempt<Output> }> => {
    const result = await sendRead(client, call, credentials, operation, documentURL(client, documentId, 'SUGGESTIONS_INLINE', writeFields), 'invalidResponse', 'notFound', zero);
    if ('attempt' in result) return { attempt: result.attempt as MutationAttempt<Output> };
    const receipt = client.receipt(call, result.response, documentId);
    let document: DocumentResource;
    try { document = decodeDocument(result.response.body); }
    catch { return { attempt: newMutationBranch('invalidResponse', zero, receipt, docsFailure(operation, 'PROTOCOL', 'provider returned an invalid document')) }; }
    if (!document.revisionId) return { attempt: newMutationBranch('providerRejected', zero, receipt, docsFailure(operation, 'AUTHORIZATION', 'connection cannot edit the document')) };
    try { return { document, bodyText: documentBodyText(document), receipt }; }
    catch { return { attempt: newMutationBranch('invalidResponse', zero, receipt, docsFailure(operation, 'PROTOCOL', 'document structure could not be read')) }; }
  };
  const first = await read();
  if ('attempt' in first) return first.attempt;
  const changed = (observed: typeof first): MutationAttempt<Output> => held(observed.bodyText)
    ? newMutationBranch(appliedBranch, output(observed.document.revisionId, [], true), observed.receipt)
    : newMutationBranch('revisionChanged', output(observed.document.revisionId, [], false), observed.receipt, docsFailure(operation, 'CONFLICT', 'document changed after the required revision; nothing was written'));
  if (first.document.revisionId !== requiredRevisionId) return changed(first);
  const prepared = build(first.document, first.bodyText);
  if (prepared.terminal) return prepared.terminal;
  if (prepared.requests.length === 0) return newMutationBranch(appliedBranch, output(first.document.revisionId, [], true), first.receipt);
  let response: ProviderResponse;
  try {
    response = await client.request(call, credentials, 'POST', `${client.documentsBaseURL}/${goPathEscape(documentId)}:batchUpdate`, Buffer.from(JSON.stringify({ requests: prepared.requests, writeControl: { requiredRevisionId } })), 'application/json; charset=UTF-8');
  } catch { return newMutationRetry(docsFailure(operation, 'TRANSPORT', 'provider update outcome is unknown; the next attempt reads the revision')); }
  const receipt = client.receipt(call, response, documentId);
  const tokens = googleTokens(response);
  if (response.status >= 200 && response.status < 300) {
    try {
      const body = parseObject(response.body);
      stringField(body, 'documentId', '');
      const replies = arrayField(body, 'replies', []).map(objectValue);
      for (const reply of replies) {
        if (reply.replaceAllText != null) {
          if (!isObject(reply.replaceAllText)) throw new TypeError('replaceAllText reply is invalid');
          integerField(reply.replaceAllText, 'occurrencesChanged', 0);
        }
      }
      if (body.writeControl != null && !isObject(body.writeControl)) throw new TypeError('writeControl is invalid');
      const revisionId = isObject(body.writeControl) ? stringField(body.writeControl, 'requiredRevisionId', '') : '';
      return newMutationBranch(appliedBranch, output(revisionId, replies, false), receipt);
    } catch { return newMutationRetry(docsFailure(operation, 'PROTOCOL', 'provider returned an invalid update response; the next attempt reads the revision')); }
  }
  if (isRetryable(response.status, tokens)) return newMutationRetry(docsFailure(operation, statusFailureKind(response.status, tokens), 'provider temporarily rejected the update'), retryAfterMs(response, client.now()));
  if (response.status === 400) {
    const reread = await read();
    if ('attempt' in reread) return reread.attempt;
    if (reread.document.revisionId === requiredRevisionId) return newMutationBranch('providerRejected', zero, receipt, docsFailure(operation, 'PROVIDER_REJECTION', 'provider rejected the update at the required revision'));
    return changed(reread);
  }
  if (response.status === 404) return newMutationBranch('notFound', zero, receipt, docsFailure(operation, 'NOT_FOUND', 'document was not found or is not visible to the connection'));
  return newMutationBranch('providerRejected', zero, receipt, docsFailure(operation, statusFailureKind(response.status, tokens), 'provider rejected the update'));
}

function buildReplacement(document: DocumentResource, bodyText: string, input: ReplaceDocumentTextInput): BuiltChange<ReplaceDocumentTextOutput> {
  if (input.target === 'wholeBody') {
    const start = document.bodyStartIndex;
    const end = document.bodyEndIndex - 1;
    const requests: Record<string, unknown>[] = [];
    if (end > start) requests.push({ deleteContentRange: { range: { startIndex: start, endIndex: end, tabId: document.tabId } } });
    if (input.text) {
      requests.push({ insertText: { text: input.text, location: { index: start, tabId: document.tabId } } });
      requests.push(...plainParagraphRequests(document.tabId, start, start + input.text.length));
    }
    return { requests };
  }
  const missing = (input.placeholders ?? []).filter(item => !bodyText.includes(item.placeholder)).map(item => item.placeholder);
  if (missing.length) return { requests: [], terminal: newMutationBranch('placeholderNotFound', { documentId: input.documentId, revisionId: document.revisionId, missingPlaceholders: missing }, {}, docsFailure('replaceDocumentText', 'VALIDATION', 'a placeholder does not occur in the document body; nothing was written')) };
  return { requests: (input.placeholders ?? []).map(item => ({ replaceAllText: { replaceText: item.text, containsText: { text: item.placeholder, matchCase: true }, tabsCriteria: { tabIds: [document.tabId] } } })) };
}

function plainParagraphRequests(tabId: string, startIndex: number, endIndex: number): Record<string, unknown>[] {
  const range = { startIndex, endIndex, tabId };
  return [
    { deleteParagraphBullets: { range } },
    { updateParagraphStyle: { range, paragraphStyle: { namedStyleType: 'NORMAL_TEXT' }, fields: '*' } },
    { updateTextStyle: { range, textStyle: {}, fields: '*' } },
  ];
}

function validateGuardedTarget(documentId: string, revisionId: string): void {
  if (!isDriveID(documentId)) throw new TypeError('documentId must be a Google Docs document ID');
  if (!revisionId || Buffer.byteLength(revisionId) > 1024 || /[\p{C}\p{Z}\u0085\u1680\u2028\u2029]/u.test(revisionId)) throw new TypeError('requiredRevisionId must be the revisionId a getDocumentText Step returned');
}

function validateReplaceInput(input: ReplaceDocumentTextInput, maxTextBytes: number): void {
  if (input.target === 'wholeBody') {
    if (input.placeholders?.length) throw new TypeError('wholeBody takes text, not placeholders');
    if (Buffer.byteLength(input.text ?? '') > maxTextBytes) throw new TypeError('text exceeds the configured text limit');
    validateWriteText('text', input.text ?? '');
    return;
  }
  if (input.target !== 'placeholders') throw new TypeError('target must be wholeBody or placeholders');
  if (input.text) throw new TypeError('placeholders takes placeholders, not text');
  const items = input.placeholders ?? [];
  if (items.length === 0 || items.length > 50) throw new TypeError('placeholders must list 1 to 50 placeholders');
  let totalBytes = 0;
  items.forEach((item, index) => {
    if (!goHasNonSpace(item.placeholder) || Buffer.byteLength(item.placeholder) > 256 || /[\n\v]/.test(item.placeholder)) throw new TypeError(`placeholders[${index}].placeholder must be 1 to 256 bytes on one line`);
    validateWriteText(`placeholders[${index}].placeholder`, item.placeholder);
    validateWriteText(`placeholders[${index}].text`, item.text);
    totalBytes += Buffer.byteLength(item.text);
    if (items.some((other, otherIndex) => otherIndex !== index && other.placeholder.includes(item.placeholder))) throw new TypeError(`placeholders[${index}].placeholder repeats or is part of another placeholder`);
    if (items.some(other => item.text.includes(other.placeholder))) throw new TypeError(`placeholders[${index}].text cannot contain a requested placeholder`);
  });
  if (totalBytes > maxTextBytes) throw new TypeError('placeholder replacements exceed the configured text limit');
}

function validateWriteText(field: string, value: string): void {
  if (value.includes('\r')) throw new TypeError(`${field} must use \\n line breaks without \\r`);
  for (const character of value) {
    const code = character.codePointAt(0)!;
    if (code <= 8 || code >= 12 && code <= 31) throw new TypeError(`${field} cannot contain control characters other than tab and line breaks`);
    if (code >= 0xE000 && code <= 0xF8FF) throw new TypeError(`${field} cannot contain private-use characters, which Google strips`);
  }
}

function safeError(error: unknown): string { return error instanceof Error ? error.message : 'invalid input'; }
function isObject(value: unknown): value is Record<string, unknown> { return !!value && typeof value === 'object' && !Array.isArray(value); }

/** Create one document with a single-dispatch fence and duplicate lookup. */
export class CreateDocumentOperation implements Mutation<CreateDocumentInput, CreateDocumentOutput> {
  /** Bind a validated Docs client. */
  constructor(private readonly client: DocsClient) {}
  /** Return the immutable operation definition. */ definition() { return CreateDocumentDefinition; }
  /** Use the stable Dex Call ID as Drive's private app-property value. */ idempotencyKey(callId: string): string { return callId; }
  /** Validate, reconcile, and create no more than once per Dex Call. */
  async invoke(call: Call, input: CreateDocumentInput): Promise<MutationAttempt<CreateDocumentOutput>> {
    if (!CreateDocumentInputSchema.safeParse(input).success) return newMutationBranch('defect', zeroCreate, {}, docsFailure('createDocument', 'VALIDATION', 'createDocument input is invalid'));
    return createDocument(this.client, call, input);
  }
}

async function createDocument(client: DocsClient, call: Call, input: CreateDocumentInput): Promise<MutationAttempt<CreateDocumentOutput>> {
  let format: 'markdown' | 'plainText';
  try { format = validateCreateInput(input, client.maxTextBytes); }
  catch (error) { return newMutationBranch('defect', zeroCreate, {}, docsFailure('createDocument', 'VALIDATION', safeError(error))); }
  const key = call.idempotencyKey ?? '';
  if (!key || Buffer.byteLength(key) > 100) return newMutationBranch('defect', zeroCreate, {}, docsFailure('createDocument', 'LOCAL_DEFECT', 'idempotency key is missing or too long'));
  let fence: ReturnType<typeof newFence>;
  try { fence = newFence(call, 30_000); }
  catch { return newMutationBranch('defect', zeroCreate, {}, docsFailure('createDocument', 'LOCAL_DEFECT', 'single-dispatch fence could not be constructed')); }
  const decision = fence.decide();
  if (decision.kind === 'failClosed') return newMutationBranch('defect', zeroCreate, {}, docsFailure('createDocument', 'LOCAL_DEFECT', 'single-dispatch checkpoint is invalid'));
  let credentials: Credentials;
  try { credentials = await client.resolveCredentials(call); }
  catch {
    if (decision.kind === 'sendAllowed') {
      try { await fence.recordNotSent(); }
      catch { return newMutationRetry(docsFailure('createDocument', 'AVAILABILITY', 'Dex could not record a pre-send retry')); }
      return newMutationRetry(docsFailure('createDocument', 'AUTHENTICATION', 'connection credentials are unavailable'));
    }
    return newMutationRetry(docsFailure('createDocument', 'AUTHENTICATION', 'connection credentials are unavailable for duplicate lookup'));
  }
  const lookupQuery = { q: `appProperties has { key='dexIdempotencyKey' and value='${key.replace(/\\/g, '\\\\').replace(/'/g, "\\'")}' }`, fields: `incompleteSearch,files(${fileFields})`, pageSize: '10', spaces: 'drive' };
  if (decision.kind === 'readBackRequired' && decision.marker?.providerId && isDriveID(decision.marker.providerId)) {
    const url = `${client.driveBaseURL}/files/${goPathEscape(decision.marker.providerId)}?${goQuery({ fields: fileFields, supportsAllDrives: 'true' })}`;
    const found = await sendRead(client, call, credentials, 'createDocument', url, 'invalidResponse', 'providerRejected', zeroCreate, createLookupSignal(call));
    if ('response' in found) {
      try {
        const document = convertCreatedDocument(parseObject(found.response.body));
        if (document.documentId === decision.marker.providerId) return newMutationBranch('created', { document, isFromEarlierAttempt: true }, client.receipt(call, found.response, document.documentId));
      } catch { /* The app-property lookup below remains the source of truth. */ }
    } else if (found.attempt.kind === 'retry') return newMutationRetry(found.attempt.failure, found.attempt.retryAfterMs);
  }
  const lookupURL = filesListURL(client, lookupQuery);
  const lookup = async (): Promise<{ document?: CreatedDocument; incomplete: boolean; response: ProviderResponse } | { attempt: QueryAttempt<unknown> }> => {
    const result = await sendRead(client, call, credentials, 'createDocument', lookupURL, 'invalidResponse', 'providerRejected', zeroCreate, createLookupSignal(call));
    if ('attempt' in result) return result;
    try { return { ...decodeEarlierDocument(result.response.body), response: result.response }; }
    catch { return { attempt: newQueryBranch('invalidResponse', zeroCreate, client.receipt(call, result.response), docsFailure('createDocument', 'PROTOCOL', 'provider returned an invalid duplicate-lookup response')) }; }
  };
  const firstLookup = await lookup();
  if ('attempt' in firstLookup) {
    const attempt = firstLookup.attempt;
    if (decision.kind === 'sendAllowed' && attempt.kind === 'retry') return retryBeforeSend(fence, 'AVAILABILITY', 'duplicate lookup is unavailable');
    if (decision.kind !== 'sendAllowed' && attempt.kind === 'branch') return newMutationUncertain(zeroCreate, docsFailure('createDocument', attempt.failure?.kind ?? 'PROTOCOL', 'a prior create cannot be confirmed because duplicate lookup was rejected'), attempt.receipt);
    return attempt as MutationAttempt<CreateDocumentOutput>;
  }
  const lookupReceipt = client.receipt(call, firstLookup.response);
  if (firstLookup.document) return newMutationBranch('created', { document: firstLookup.document, isFromEarlierAttempt: true }, client.receipt(call, firstLookup.response, firstLookup.document.documentId));
  if (firstLookup.incomplete) return decision.kind === 'sendAllowed' ? retryBeforeSend(fence, 'AVAILABILITY', 'provider did not complete the duplicate lookup') : newMutationRetry(docsFailure('createDocument', 'AVAILABILITY', 'provider did not complete the duplicate lookup'));
  if (decision.kind !== 'sendAllowed') return newMutationUncertain(zeroCreate, docsFailure('createDocument', 'TRANSPORT', 'an earlier attempt of this Step may have created the document, and the duplicate lookup does not show it yet'), lookupReceipt);
  let request: { url: string; body: Uint8Array; contentType: string };
  try { request = buildCreateRequest(client, input, format, key); }
  catch { return newMutationBranch('defect', zeroCreate, {}, docsFailure('createDocument', 'LOCAL_DEFECT', 'create request could not be encoded')); }
  let response: ProviderResponse | undefined;
  let sendError: unknown;
  try { response = await fence.dispatch(signal => client.request(call, credentials, 'POST', request.url, request.body, request.contentType, true, signal)); }
  catch (error) { sendError = error; }
  if (sendError && !(sendError instanceof DocsRequestError)) return newMutationRetry(docsFailure('createDocument', 'AVAILABILITY', 'Dex could not record a dispatch claim or the attempt has too little time'));
  const receipt = client.receipt(call, response);
  const tokens = response ? googleTokens(response) : [];
  const confirmAfterSend = async (kind: FailureKind, message: string): Promise<MutationAttempt<CreateDocumentOutput>> => {
    const found = await lookup();
    if ('response' in found && !found.incomplete && found.document) {
      try { await fence.recordProviderId(found.document.documentId); }
      catch { /* Confirmation is conclusive even when an extra checkpoint fails. */ }
      return newMutationBranch('created', { document: found.document }, { ...receipt, providerObjectId: found.document.documentId });
    }
    return newMutationUncertain(zeroCreate, docsFailure('createDocument', kind, message), receipt);
  };
  if (response && response.status >= 200 && response.status < 300) {
    try {
      const document = convertCreatedDocument(parseObject(response.body));
      try { await fence.recordProviderId(document.documentId); }
      catch { /* An accepted provider result is conclusive without this extra checkpoint. */ }
      return newMutationBranch('created', { document }, { ...receipt, providerObjectId: document.documentId });
    } catch { return confirmAfterSend('PROTOCOL', 'provider returned an unusable accepted create response'); }
  }
  if (response && (isRateLimit(response.status, tokens) || response.status === 503)) {
    const found = await lookup();
    if ('response' in found && !found.incomplete && found.document) {
      try { await fence.recordProviderId(found.document.documentId); }
      catch { /* Confirmation is conclusive even when an extra checkpoint fails. */ }
      return newMutationBranch('created', { document: found.document }, { ...receipt, providerObjectId: found.document.documentId });
    }
    try { await fence.recordCanResend(); }
    catch { return newMutationRetry(docsFailure('createDocument', 'AVAILABILITY', 'Dex could not record safe resend after provider rejection')); }
    return newMutationRetry(docsFailure('createDocument', statusFailureKind(response.status, tokens), 'provider temporarily rejected the create'), retryAfterMs(response, client.now()));
  }
  if (response?.status === 401) {
    try { await client.refreshCredentialsAfterRejection(call); }
    catch { return newMutationBranch('providerRejected', zeroCreate, receipt, docsFailure('createDocument', 'AUTHENTICATION', 'provider rejected the create credential')); }
    try { await fence.recordCanResend(); }
    catch { return newMutationRetry(docsFailure('createDocument', 'AUTHENTICATION', 'Dex could not record safe resend after credential refresh')); }
    return newMutationRetry(docsFailure('createDocument', 'AUTHENTICATION', 'provider rejected the create credential; refreshed for the next attempt'));
  }
  if (sendError) {
    if (sendError instanceof DocsRequestError && sendError.tooLarge && sendError.status >= 200 && sendError.status < 300) {
      return confirmAfterSend('PROTOCOL', 'provider returned an unusable accepted create response');
    }
    if (sendError instanceof DocsRequestError && sendError.beforeSend) {
      try { await fence.recordCanResend(); }
      catch { return newMutationRetry(docsFailure('createDocument', 'TRANSPORT', 'Dex could not record a proven no-send')); }
      return newMutationRetry(docsFailure('createDocument', 'TRANSPORT', 'provider connection failed before the create was sent'));
    }
    return confirmAfterSend('TRANSPORT', 'provider create outcome is unknown');
  }
  if (response && (response.status >= 500 || response.status === 408 || response.status === 409)) return confirmAfterSend('AVAILABILITY', 'provider create outcome is unknown');
  return newMutationBranch('providerRejected', zeroCreate, receipt, docsFailure('createDocument', statusFailureKind(response?.status ?? 0, tokens), 'provider rejected the create'));
}

async function retryBeforeSend(fence: ReturnType<typeof newFence>, kind: FailureKind, reason: string): Promise<MutationAttempt<CreateDocumentOutput>> {
  try { await fence.recordNotSent(); }
  catch { return newMutationRetry(docsFailure('createDocument', 'AVAILABILITY', 'Dex could not record a pre-send retry')); }
  return newMutationRetry(docsFailure('createDocument', kind, reason));
}

function validateCreateInput(input: CreateDocumentInput, limit: number): 'markdown' | 'plainText' {
  if (!goHasNonSpace(input.title) || Buffer.byteLength(input.title) > 1024 || /[\n\v]/.test(input.title)) throw new TypeError('title must be 1 to 1024 bytes on one line');
  validateWriteText('title', input.title);
  if (input.parentFolderId && !isDriveID(input.parentFolderId)) throw new TypeError('parentFolderId must be a Drive folder ID or root');
  const format = input.initialTextFormat || 'plainText';
  if (format !== 'plainText' && format !== 'markdown') throw new TypeError('initialTextFormat must be plainText or markdown');
  if (Buffer.byteLength(input.initialText ?? '') > limit) throw new TypeError('initialText exceeds the configured text limit');
  validateWriteText('initialText', input.initialText ?? '');
  return format as 'markdown' | 'plainText';
}

function buildCreateRequest(client: DocsClient, input: CreateDocumentInput, format: 'markdown' | 'plainText', key: string): { url: string; body: Uint8Array; contentType: string } {
  const metadata = { appProperties: { dexIdempotencyKey: key }, mimeType: 'application/vnd.google-apps.document', name: input.title, ...(input.parentFolderId ? { parents: [input.parentFolderId] } : {}) };
  const encoded = JSON.stringify(metadata);
  if (!input.initialText) return { url: `${client.driveBaseURL}/files?${goQuery({ fields: fileFields, supportsAllDrives: 'true' })}`, body: Buffer.from(encoded), contentType: 'application/json; charset=UTF-8' };
  const boundary = randomBytes(18).toString('hex');
  const body = Buffer.from(`--${boundary}\r\nContent-Type: application/json; charset=UTF-8\r\n\r\n${encoded}\r\n--${boundary}\r\nContent-Type: ${format === 'markdown' ? 'text/markdown' : 'text/plain'}; charset=UTF-8\r\n\r\n${input.initialText}\r\n--${boundary}--\r\n`);
  return { url: `${client.driveUploadBaseURL}/files?${goQuery({ uploadType: 'multipart', fields: fileFields, supportsAllDrives: 'true' })}`, body, contentType: `multipart/related; boundary=${boundary}` };
}

function decodeEarlierDocument(body: Uint8Array): { document?: CreatedDocument; incomplete: boolean } {
  const value = parseObject(body);
  const files = arrayField(value, 'files', []);
  let document: CreatedDocument | undefined;
  for (const file of files) {
    const current = convertCreatedDocument(objectValue(file));
    if (!document || Date.parse(current.createdTime ?? '0001-01-01T00:00:00Z') < Date.parse(document.createdTime ?? '0001-01-01T00:00:00Z')) document = current;
  }
  if (value.incompleteSearch != null && typeof value.incompleteSearch !== 'boolean') throw new TypeError('incompleteSearch is invalid');
  return { ...(document ? { document } : {}), incomplete: value.incompleteSearch === true };
}

function convertCreatedDocument(value: Record<string, unknown>): CreatedDocument {
  const id = stringField(value, 'id', '');
  if (!isDriveID(id) || stringField(value, 'mimeType', '') !== 'application/vnd.google-apps.document') throw new TypeError('file resource is not a Google Doc with a valid ID');
  const parents = arrayField(value, 'parents', []).map(item => { if (typeof item !== 'string') throw new TypeError('parent ID is invalid'); return item; });
  const createdTime = stringField(value, 'createdTime', '0001-01-01T00:00:00Z');
  if (value.trashed != null && typeof value.trashed !== 'boolean') throw new TypeError('trashed is invalid');
  const document: CreatedDocument = { documentId: id, title: stringField(value, 'name', ''), ...(parents.length ? { parents } : {}), ...(stringField(value, 'webViewLink', '') ? { webViewLink: stringField(value, 'webViewLink') } : {}), ...(createdTime !== '0001-01-01T00:00:00Z' ? { createdTime } : {}), ...(value.trashed === true ? { isTrashed: true } : {}) };
  return CreatedDocumentSchema.parse(document);
}

function createLookupSignal(call: Call): AbortSignal {
  const deadline = call.context.deadline?.getTime();
  const remaining = deadline === undefined ? 25_000 : Math.max(0, deadline - Date.now());
  const margin = deadline === undefined ? 0 : Math.min(1_000, Math.max(1, remaining / 10));
  return AbortSignal.any([call.context.cancellationSignal, AbortSignal.timeout(Math.max(0, remaining - margin))]);
}
